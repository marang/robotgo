package agent

import (
	"strings"

	robotgo "github.com/marang/robotgo"
)

const (
	agentWindowBackendSway     = "sway"
	agentWindowBackendHyprland = "hyprland"
	agentInputPureGoX11        = "pure-go-x11"
)

func buildCatalog(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCatalog {
	return OperationCatalog{
		SchemaVersion: CatalogSchemaVersion,
		Operations: []OperationCapability{
			observationCapability(policy, capabilities),
			viewCapability(policy, capabilities),
			analysisCapability(OperationOCR, policy),
			analysisCapability(OperationDetectElements, policy),
			inspectUICapability(policy, capabilities),
			resolveUICapability(policy, capabilities),
			elementActCapability(policy, capabilities),
			findColorCapability(policy, capabilities),
			waitColorCapability(policy, capabilities),
			operationCapability(OperationMove, policy, capabilities.Mouse),
			operationCapability(OperationClick, policy, capabilities.Mouse),
			scrollCapability(policy, capabilities),
			elevatedCooperativeOperationCapability(OperationDrag, policy, capabilities.Mouse),
			operationCapability(OperationTypeText, policy, capabilities.Keyboard),
			keyChordCapability(policy, capabilities),
			activationCapability(policy, capabilities),
		},
	}
}

func resolveUICapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationResolveUI]
	_, operationAllowed := policy.allowOperation[OperationResolveUI]
	_, inspectAllowed := policy.allowOperation[OperationInspectUI]
	policyAllowed := operationAllowed && inspectAllowed && len(policy.allowWindow) > 0 &&
		len(policy.allowUIRole) > 0
	for _, property := range []UIProperty{
		UIPropertyRole, UIPropertyName, UIPropertyState, UIPropertyBounds, UIPropertyActions,
	} {
		if _, allowed := policy.allowUIProperty[property]; !allowed {
			policyAllowed = false
		}
	}
	feature := capabilities.Accessibility
	remediation := feature.Notes
	if remediation == "" {
		remediation = feature.Reason
	}
	return OperationCapability{
		Operation: OperationResolveUI, Available: feature.Available,
		PolicyAllowed: policyAllowed, Backend: "retained-observation", Fallback: false,
		Risk: RiskSensitiveRead, ConfirmationRequired: confirmationRequired,
		Cancellation: CancellationCooperative, ProcessGlobalBackend: false,
		ExclusiveAgentSession: true, Reason: feature.Reason, Remediation: remediation,
		UnavailableCode:             featureUnavailableCode(feature),
		TargetSpecVersion:           TargetSpecSchemaVersion,
		TargetResolutionStrategies:  append([]TargetResolutionStrategy(nil), allTargetResolutionStrategies...),
		TargetResolutionModes:       append([]TargetResolutionMode(nil), policy.AllowedTargetModes...),
		CapabilityLeaseVersion:      CapabilityLeaseSchemaVersion,
		CapabilityLeaseRequired:     policy.RequireCapabilityLease,
		MaxCapabilityLeases:         policy.MaxCapabilityLeases,
		MaxCapabilityLeaseMillis:    policy.MaxCapabilityLeaseMillis,
		AdaptiveTargetThreshold:     policy.AdaptiveTargetThreshold,
		MaxTargetAncestors:          min(policy.MaxUITreeDepth, uint32(maxTargetSpecAncestors)),
		TargetEvidenceClauseVersion: TargetEvidenceClauseSchemaVersion,
		TargetEvidenceSources:       append([]TargetEvidenceSource(nil), policy.AllowedTargetEvidenceSources...),
		TargetEvidenceProviders:     append([]TargetEvidenceProvider(nil), policy.AllowedTargetEvidenceProviders...),
		MaxTargetEvidenceClauses:    maxTargetEvidenceClauses,
		MaxTargetEvidenceAgeMillis:  policy.MaxTargetEvidenceAgeMillis,
		MinTargetOCRConfidence:      policy.MinTargetOCRConfidence,
		MinTargetVisualConfidence:   policy.MinTargetVisualConfidence,
	}
}

func elementActCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationElementAct]
	_, operationAllowed := policy.allowOperation[OperationElementAct]
	_, inspectAllowed := policy.allowOperation[OperationInspectUI]
	_, resolveAllowed := policy.allowOperation[OperationResolveUI]
	policyAllowed := operationAllowed && inspectAllowed && len(policy.allowWindow) > 0 &&
		len(policy.allowUIAction) > 0 && policy.MaxActions > 0 &&
		policy.MinActionIntervalMillis > 0 && policy.UIActionTimeoutMillis > 0 &&
		policy.SessionTimeoutMillis > 0 && (!policy.RequireCapabilityLease || resolveAllowed)
	feature := capabilities.Accessibility
	remediation := feature.Notes
	if remediation == "" {
		remediation = feature.Reason
	}
	return OperationCapability{
		Operation: OperationElementAct, Available: feature.Available,
		PolicyAllowed: policyAllowed, Backend: feature.Backend, Fallback: false,
		Risk: RiskElevatedMutation, ConfirmationRequired: confirmationRequired,
		Cancellation: CancellationCooperative, ProcessGlobalBackend: true,
		ExclusiveAgentSession: true, Reason: feature.Reason, Remediation: remediation,
		UnavailableCode:              featureUnavailableCode(feature),
		ActionProofVersion:           ActionProofSchemaVersion,
		UIConditionKinds:             append([]UIElementConditionKind(nil), allUIElementConditionKinds...),
		UIVerificationAttempts:       policy.UIVerificationAttempts,
		UIVerificationIntervalMillis: policy.UIVerificationIntervalMillis,
		UIVerificationTimeoutMillis:  policy.UIVerificationTimeoutMillis,
	}
}

func analysisCapability(operation Operation, policy Policy) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[operation]
	_, operationAllowed := policy.allowOperation[operation]
	available, backend, reason, remediation := true, VisualAnalysisBackend, "", ""
	if operation == OperationOCR {
		available, backend = ocrBackendAvailable, ocrBackendName
		if !available {
			reason = robotgo.ErrNotSupported.Error()
			remediation = "rebuild with the ocr build tag and CGO plus Tesseract/Leptonica development libraries"
		}
	}
	policyAllowed := operationAllowed && policy.MaxAnalysisPixels > 0 && policy.MaxAnalyses > 0 &&
		policy.MaxConcurrentAnalyses == 1 && policy.MinAnalysisIntervalMillis > 0 &&
		policy.AnalysisTimeoutMillis > 0 && policy.SessionTimeoutMillis > 0
	if operation == OperationOCR {
		policyAllowed = policyAllowed && len(policy.allowOCRLanguage) > 0 &&
			policy.MaxOCRBoxes > 0 && policy.MaxOCRTextBytes > 0
	} else {
		policyAllowed = policyAllowed && policy.MaxVisualElements > 0
	}
	code := ErrorCode("")
	if !available {
		code = ErrorUnsupported
	}
	return OperationCapability{
		Operation: operation, Available: available, PolicyAllowed: policyAllowed,
		Backend: backend, Risk: RiskSensitiveRead, ConfirmationRequired: confirmationRequired,
		Cancellation: CancellationCooperative, ProcessGlobalBackend: true,
		ExclusiveAgentSession: true, Reason: reason, Remediation: remediation,
		UnavailableCode: code,
	}
}

func viewCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationView]
	_, operationAllowed := policy.allowOperation[OperationView]
	available, backend, fallback, remediation := agentCaptureCapability(capabilities)
	policyAllowed := operationAllowed && len(policy.allowDisplay) > 0 &&
		(len(policy.AllowedViewRegions) > 0 || policy.AllowFullDisplayView) &&
		policy.MaxViewSourcePixels > 0 && policy.MaxViewEncodedBytes > 0 &&
		policy.MaxViewWidth > 0 && policy.MaxViewHeight > 0 && policy.MaxViews > 0 &&
		policy.MaxObservations > 0 && policy.MaxConcurrentViews == 1 &&
		policy.MinViewIntervalMillis > 0 && policy.ViewTimeoutMillis > 0 &&
		policy.SessionTimeoutMillis > 0
	if capabilities.Runtime.GOOS == goOSLinux &&
		capabilities.Runtime.DisplayServer == robotgo.DisplayServerWayland &&
		backend == robotgo.FeatureBackendScreenCast && !policy.AllowPortalView {
		policyAllowed = false
		remediation = "enable allow_portal_view only after the operator has explicitly established an authorized ScreenCast session"
	}
	unavailableCode := featureUnavailableCode(capabilities.Capture)
	if !available && unavailableCode == "" {
		unavailableCode = ErrorUnsupported
	}
	return OperationCapability{
		Operation: OperationView, Available: available, PolicyAllowed: policyAllowed,
		Backend: backend, Fallback: fallback, Risk: RiskSensitiveRead,
		ConfirmationRequired: confirmationRequired,
		Cancellation:         CancellationCooperative, ProcessGlobalBackend: true,
		ExclusiveAgentSession: true, Reason: capabilities.Capture.Reason,
		Remediation: remediation, UnavailableCode: unavailableCode,
		CaptureAvailable: available, CapturePolicyAllowed: policyAllowed,
		CaptureFallback: fallback, CaptureBackend: backend,
	}
}

func inspectUICapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationInspectUI]
	_, operationAllowed := policy.allowOperation[OperationInspectUI]
	policyAllowed := operationAllowed && len(policy.allowWindow) > 0 &&
		len(policy.allowUIRole) > 0 && len(policy.allowUIProperty) > 0 &&
		policy.MaxQueries > 0 && policy.MaxObservations > 0 &&
		policy.MaxUIElements > 0 && policy.MaxUITreeDepth > 0 && policy.MaxUIStringBytes > 0 &&
		policy.MinUIQueryIntervalMillis > 0 && policy.SessionTimeoutMillis > 0
	if _, roleAllowed := policy.allowUIProperty[UIPropertyRole]; !roleAllowed {
		policyAllowed = false
	}
	feature := capabilities.Accessibility
	remediation := feature.Notes
	if remediation == "" {
		remediation = feature.Reason
	}
	return OperationCapability{
		Operation: OperationInspectUI, Available: feature.Available,
		PolicyAllowed: policyAllowed, Backend: feature.Backend, Fallback: feature.Fallback,
		Risk: RiskSensitiveRead, ConfirmationRequired: confirmationRequired,
		Cancellation: CancellationCooperative, ProcessGlobalBackend: true,
		ExclusiveAgentSession: true, Reason: feature.Reason, Remediation: remediation,
		UnavailableCode: featureUnavailableCode(feature),
	}
}

func keyChordCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	feature := keyChordFeature(capabilities)
	return elevatedCooperativeOperationCapability(OperationKeyChord, policy, feature)
}

func keyChordFeature(capabilities robotgo.RuntimeCapabilities) robotgo.FeatureCapability {
	if !capabilities.Keyboard.Available {
		return capabilities.Keyboard
	}
	if capabilities.Runtime.GOOS == goOSLinux {
		feature := capabilities.Keyboard
		feature.Available = false
		feature.Reason = robotgo.ErrNotSupported.Error() +
			": Linux keyboard input cannot bind a chord to an allowed process"
		feature.Notes = "X11 and Wayland inject into global focus; use a backend with process-targeted keyboard injection"
		return feature
	}
	if capabilities.Runtime.GOOS == "windows" {
		feature := capabilities.Keyboard
		feature.Available = false
		feature.Reason = robotgo.ErrNotSupported.Error() +
			": Windows keyboard input cannot atomically bind validation and dispatch to one window generation"
		feature.Notes = "use a cooperative accessibility or in-process backend that can validate the target generation at dispatch"
		return feature
	}
	if !capabilities.Window.Available {
		feature := capabilities.Window
		feature.Backend = capabilities.Keyboard.Backend
		feature.Fallback = capabilities.Keyboard.Fallback
		feature.Reason = "keyboard injection is available but target-window identity is unavailable: " + feature.Reason
		feature.Notes = "keyboard.chord requires a trustworthy process title before process-targeted injection"
		return feature
	}
	return capabilities.Keyboard
}

func scrollCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	capability := cooperativeOperationCapability(OperationScroll, policy, capabilities.Mouse)
	capability.ScrollAxes = []ScrollAxis{ScrollAxisHorizontal, ScrollAxisVertical}
	if capabilities.Mouse.Backend == agentInputPureGoX11 {
		capability.ScrollAxes = []ScrollAxis{ScrollAxisVertical}
		capability.Remediation = "Pure-Go X11 currently supports vertical scrolling only; use the native X11 backend for horizontal scrolling"
	}
	return capability
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
		hasAllowedWindows(policy) {
		feature.Available = false
		feature.Fallback = false
		feature.Reason = robotgo.ErrNotSupported.Error() +
			": native macOS CGO cannot preserve one exact window reference across validation and activation"
		feature.Notes = "use the Pure-Go macOS window backend for validated CGWindowID activation"
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
				capabilities.Runtime.CGOEnabled && hasAllowedWindows(policy)) {
		capability.UnavailableCode = ErrorUnsupported
	}
	return capability
}

func hasAllowedWindows(policy Policy) bool {
	return len(policy.allowWindow) > 0
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
	cloned := OperationCatalog{
		SchemaVersion: source.SchemaVersion,
		Operations:    append([]OperationCapability(nil), source.Operations...),
	}
	for index := range cloned.Operations {
		cloned.Operations[index].ScrollAxes = append(
			[]ScrollAxis(nil),
			cloned.Operations[index].ScrollAxes...,
		)
		cloned.Operations[index].UIConditionKinds = append(
			[]UIElementConditionKind(nil),
			cloned.Operations[index].UIConditionKinds...,
		)
		cloned.Operations[index].TargetResolutionStrategies = append(
			[]TargetResolutionStrategy(nil),
			cloned.Operations[index].TargetResolutionStrategies...,
		)
		cloned.Operations[index].TargetResolutionModes = append(
			[]TargetResolutionMode(nil), cloned.Operations[index].TargetResolutionModes...,
		)
		cloned.Operations[index].TargetEvidenceSources = append(
			[]TargetEvidenceSource(nil), cloned.Operations[index].TargetEvidenceSources...,
		)
		cloned.Operations[index].TargetEvidenceProviders = append(
			[]TargetEvidenceProvider(nil), cloned.Operations[index].TargetEvidenceProviders...,
		)
	}
	return cloned
}
