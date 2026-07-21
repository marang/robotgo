package agent

import robotgo "github.com/marang/robotgo"

func buildCatalog(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCatalog {
	return OperationCatalog{
		SchemaVersion: CatalogSchemaVersion,
		Operations: []OperationCapability{
			observationCapability(policy, capabilities),
			findColorCapability(policy),
			waitColorCapability(policy, capabilities),
			operationCapability(OperationMove, policy, capabilities.Mouse),
			operationCapability(OperationClick, policy, capabilities.Mouse),
			operationCapability(OperationTypeText, policy, capabilities.Keyboard),
		},
	}
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

func findColorCapability(policy Policy) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationFindColor]
	_, policyAllowed := policy.allowOperation[OperationFindColor]
	return OperationCapability{
		Operation: OperationFindColor, Available: true,
		PolicyAllowed: policyAllowed && policy.MaxQueries > 0,
		Backend:       "in-memory-observation", Risk: RiskSensitiveRead,
		ConfirmationRequired:  confirmationRequired,
		Cancellation:          CancellationCooperative,
		ExclusiveAgentSession: true,
		Reason:                "color search uses only a live capture already owned by this session",
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
	_, confirmationRequired := policy.requireConfirmation[operation]
	_, policyAllowed := policy.allowOperation[operation]
	remediation := feature.Notes
	if remediation == "" {
		remediation = feature.Reason
	}
	return OperationCapability{
		Operation: operation, Available: feature.Available, PolicyAllowed: policyAllowed, Backend: feature.Backend,
		Fallback: feature.Fallback, Risk: RiskReversibleMutation,
		ConfirmationRequired: confirmationRequired,
		Cancellation:         CancellationPreflightOnly,
		ProcessGlobalBackend: true, ExclusiveAgentSession: true,
		Reason: feature.Reason, Remediation: remediation,
	}
}

func cloneCatalog(source OperationCatalog) OperationCatalog {
	return OperationCatalog{
		SchemaVersion: source.SchemaVersion,
		Operations:    append([]OperationCapability(nil), source.Operations...),
	}
}
