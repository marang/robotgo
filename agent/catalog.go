package agent

import (
	"strings"

	robotgo "github.com/marang/robotgo"
)

const (
	agentWindowBackendSway     = "sway"
	agentWindowBackendHyprland = "hyprland"
	agentKeyboardPureGoX11     = "pure-go-x11"
)

func buildCatalog(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCatalog {
	return OperationCatalog{
		SchemaVersion: CatalogSchemaVersion,
		Operations: []OperationCapability{
			observationCapability(policy, capabilities),
			findColorCapability(policy, capabilities),
			waitColorCapability(policy, capabilities),
			operationCapability(OperationMove, policy, capabilities.Mouse),
			operationCapability(OperationClick, policy, capabilities.Mouse),
			cooperativeOperationCapability(OperationScroll, policy, capabilities.Mouse),
			elevatedCooperativeOperationCapability(OperationDrag, policy, capabilities.Mouse),
			operationCapability(OperationTypeText, policy, capabilities.Keyboard),
			keyChordCapability(policy, capabilities),
			activationCapability(policy, capabilities),
		},
	}
}

func keyChordCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	feature := keyChordFeature(capabilities)
	if capabilities.Keyboard.Backend == agentKeyboardPureGoX11 {
		capability := elevatedOperationCapability(OperationKeyChord, policy, feature)
		if feature.Available {
			capability.Remediation = "Pure-Go X11 executes the complete chord as one backend-owned press/release transaction because persistent literal-key holds are unsafe"
		}
		return capability
	}
	return elevatedCooperativeOperationCapability(OperationKeyChord, policy, feature)
}

func keyChordFeature(capabilities robotgo.RuntimeCapabilities) robotgo.FeatureCapability {
	if !capabilities.Keyboard.Available {
		return capabilities.Keyboard
	}
	if !capabilities.Window.Available {
		feature := capabilities.Window
		feature.Backend = capabilities.Keyboard.Backend
		feature.Fallback = capabilities.Keyboard.Fallback
		feature.Reason = "keyboard injection is available but active-window identity is unavailable: " + feature.Reason
		feature.Notes = "keyboard.chord requires a trustworthy active process identity before injection"
		return feature
	}
	if capabilities.Runtime.GOOS == goOSLinux &&
		capabilities.Runtime.DisplayServer == robotgo.DisplayServerWayland &&
		capabilities.Window.Backend != agentWindowBackendSway &&
		capabilities.Window.Backend != agentWindowBackendHyprland {
		feature := capabilities.Window
		feature.Available = false
		feature.Backend = capabilities.Keyboard.Backend
		feature.Fallback = capabilities.Keyboard.Fallback
		feature.Reason = robotgo.ErrNotSupported.Error() +
			": selected Wayland window backend cannot provide a trustworthy active process and title"
		feature.Notes = "keyboard.chord requires Sway or Hyprland active-window identity until an accessibility backend provides an equivalent contract"
		return feature
	}
	return capabilities.Keyboard
}

func activationFeature(policy Policy, capabilities robotgo.RuntimeCapabilities) robotgo.FeatureCapability {
	feature := capabilities.Window
	if capabilities.Runtime.GOOS == goOSLinux &&
		capabilities.Runtime.DisplayServer == robotgo.DisplayServerWayland {
		feature.Available = false
		feature.Fallback = false
		feature.Reason = "global foreign-window activation is unavailable on the selected Wayland backend"
		feature.Notes = "use a compositor or accessibility backend that exposes a stable activation contract; RobotGo does not route Wayland activation through X11"
	}
	if capabilities.Runtime.GOOS == "darwin" && capabilities.Runtime.CGOEnabled &&
		onlyNativeHandleWindows(policy) {
		feature.Available = false
		feature.Fallback = false
		feature.Reason = "native macOS CGO window handles are not serializable activation targets"
		feature.Notes = "allow a process target or use the Pure-Go macOS window backend for validated CGWindowID activation"
	}
	return feature
}

func activationCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	feature := activationFeature(policy, capabilities)
	capability := elevatedOperationCapability(OperationActivate, policy, feature)
	if !feature.Available &&
		(capabilities.Runtime.GOOS == goOSLinux &&
			capabilities.Runtime.DisplayServer == robotgo.DisplayServerWayland ||
			capabilities.Runtime.GOOS == "darwin" &&
				capabilities.Runtime.CGOEnabled && onlyNativeHandleWindows(policy)) {
		capability.UnavailableCode = ErrorUnsupported
	}
	return capability
}

func onlyNativeHandleWindows(policy Policy) bool {
	if len(policy.allowWindow) == 0 {
		return false
	}
	for identity := range policy.allowWindow {
		if identity.kind != WindowTargetHandle {
			return false
		}
	}
	return true
}

func observationCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationObserve]
	_, policyAllowed := policy.allowOperation[OperationObserve]
	captureAvailable, captureBackend, captureFallback, remediation := agentCaptureCapability(capabilities)
	capturePolicyAllowed := policyAllowed && policy.MaxCapturePixels > 0 && len(policy.allowDisplay) > 0
	return OperationCapability{
		Operation: OperationObserve, Available: true, PolicyAllowed: policyAllowed,
		Backend: runtimeDiagnosticsBackend, Risk: RiskSensitiveRead,
		ConfirmationRequired: confirmationRequired,
		Cancellation:         CancellationPreflightOnly,
		ProcessGlobalBackend: true, ExclusiveAgentSession: true,
		Reason:      "runtime diagnostics are available without opening desktop consent",
		Remediation: remediation, OptionalCapture: true,
		CaptureAvailable:     captureAvailable,
		CapturePolicyAllowed: capturePolicyAllowed,
		CaptureFallback:      captureFallback,
		CaptureBackend:       captureBackend,
	}
}

func findColorCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationFindColor]
	_, policyAllowed := policy.allowOperation[OperationFindColor]
	captureAvailable, captureBackend, captureFallback, remediation := agentCaptureCapability(capabilities)
	capturePolicyAllowed := policyAllowed && policy.MaxQueries > 0 && policy.MaxCapturePixels > 0 &&
		len(policy.allowDisplay) > 0
	return OperationCapability{
		Operation: OperationFindColor, Available: captureAvailable,
		PolicyAllowed: capturePolicyAllowed,
		Backend:       "in-memory-observation", Risk: RiskSensitiveRead,
		ConfirmationRequired:  confirmationRequired,
		Cancellation:          CancellationCooperative,
		ExclusiveAgentSession: true,
		Reason:                "color search uses only a live capture already owned by this session; creating one requires the reported capture backend",
		Remediation:           remediation,
		CaptureAvailable:      captureAvailable,
		CapturePolicyAllowed:  capturePolicyAllowed,
		CaptureFallback:       captureFallback,
		CaptureBackend:        captureBackend,
	}
}

func waitColorCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationWaitColor]
	_, policyAllowed := policy.allowOperation[OperationWaitColor]
	captureAvailable, captureBackend, captureFallback, remediation := agentCaptureCapability(capabilities)
	capturePolicyAllowed := policyAllowed && policy.MaxQueries > 0 && policy.WaitAttempts > 0 &&
		policy.MaxCapturePixels > 0 && len(policy.allowDisplay) > 0
	return OperationCapability{
		Operation: OperationWaitColor, Available: captureAvailable,
		PolicyAllowed: capturePolicyAllowed,
		Backend:       captureBackend, Fallback: captureFallback, Risk: RiskSensitiveRead,
		ConfirmationRequired: confirmationRequired,
		Cancellation:         CancellationCooperative,
		ProcessGlobalBackend: true, ExclusiveAgentSession: true,
		Reason: capabilities.Capture.Reason, Remediation: remediation,
		CaptureAvailable: captureAvailable, CapturePolicyAllowed: capturePolicyAllowed,
		CaptureFallback: captureFallback, CaptureBackend: captureBackend,
	}
}

func agentCaptureCapability(capabilities robotgo.RuntimeCapabilities) (bool, string, bool, string) {
	feature := capabilities.Capture
	remediation := feature.Notes
	if remediation == "" {
		remediation = feature.Reason
	}
	available := feature.Available
	if capabilities.Runtime.GOOS == goOSLinux &&
		capabilities.Runtime.DisplayServer == robotgo.DisplayServerWayland &&
		feature.Backend != robotgo.FeatureBackendScreenCast &&
		feature.Backend != robotgo.FeatureBackendWaylandScreencopy {
		available = false
		remediation = "agent capture attempts native screencopy first and will not open portal consent implicitly; start ScreenCast explicitly for an authorized fallback"
	}
	return available, feature.Backend, feature.Fallback, remediation
}

func operationCapability(operation Operation, policy Policy, feature robotgo.FeatureCapability) OperationCapability {
	return mutationCapability(
		operation, policy, feature, RiskReversibleMutation,
		CancellationPreflightOnly, false,
	)
}

func cooperativeOperationCapability(operation Operation, policy Policy, feature robotgo.FeatureCapability) OperationCapability {
	return mutationCapability(
		operation, policy, feature, RiskReversibleMutation,
		CancellationCooperative, false,
	)
}

func elevatedOperationCapability(operation Operation, policy Policy, feature robotgo.FeatureCapability) OperationCapability {
	return mutationCapability(
		operation, policy, feature, RiskElevatedMutation,
		CancellationPreflightOnly, true,
	)
}

func elevatedCooperativeOperationCapability(operation Operation, policy Policy, feature robotgo.FeatureCapability) OperationCapability {
	return mutationCapability(
		operation, policy, feature, RiskElevatedMutation,
		CancellationCooperative, true,
	)
}

func mutationCapability(
	operation Operation,
	policy Policy,
	feature robotgo.FeatureCapability,
	risk RiskClass,
	cancellation CancellationSupport,
	mandatoryConfirmation bool,
) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[operation]
	_, policyAllowed := policy.allowOperation[operation]
	remediation := feature.Notes
	if remediation == "" {
		remediation = feature.Reason
	}
	return OperationCapability{
		Operation: operation, Available: feature.Available, PolicyAllowed: policyAllowed, Backend: feature.Backend,
		Fallback: feature.Fallback, Risk: risk,
		ConfirmationRequired: confirmationRequired || mandatoryConfirmation,
		Cancellation:         cancellation,
		ProcessGlobalBackend: true, ExclusiveAgentSession: true,
		Reason: feature.Reason, Remediation: remediation,
		UnavailableCode: featureUnavailableCode(feature),
	}
}

func featureUnavailableCode(feature robotgo.FeatureCapability) ErrorCode {
	if feature.Available {
		return ""
	}
	reason := strings.ToLower(feature.Reason)
	if strings.Contains(reason, strings.ToLower(robotgo.ErrPermissionDenied.Error())) {
		return ErrorPermissionDenied
	}
	if strings.Contains(reason, strings.ToLower(robotgo.ErrNotSupported.Error())) {
		return ErrorUnsupported
	}
	return ErrorUnavailable
}

func cloneCatalog(source OperationCatalog) OperationCatalog {
	return OperationCatalog{
		SchemaVersion: source.SchemaVersion,
		Operations:    append([]OperationCapability(nil), source.Operations...),
	}
}
