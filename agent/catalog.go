package agent

import robotgo "github.com/marang/robotgo"

func buildCatalog(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCatalog {
	return OperationCatalog{
		SchemaVersion: CatalogSchemaVersion,
		Operations: []OperationCapability{
			observationCapability(policy, capabilities),
			operationCapability(OperationMove, policy, capabilities.Mouse),
			operationCapability(OperationClick, policy, capabilities.Mouse),
			operationCapability(OperationTypeText, policy, capabilities.Keyboard),
		},
	}
}

func observationCapability(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCapability {
	_, confirmationRequired := policy.requireConfirmation[OperationObserve]
	_, policyAllowed := policy.allowOperation[OperationObserve]
	remediation := capabilities.Capture.Notes
	if remediation == "" {
		remediation = capabilities.Capture.Reason
	}
	captureAvailable := capabilities.Capture.Available
	captureBackend := capabilities.Capture.Backend
	capturePolicyAllowed := policyAllowed && policy.MaxCapturePixels > 0 && len(policy.allowDisplay) > 0
	if capabilities.Runtime.GOOS == goOSLinux &&
		capabilities.Runtime.DisplayServer == robotgo.DisplayServerWayland &&
		captureBackend != robotgo.FeatureBackendScreenCast &&
		captureBackend != robotgo.FeatureBackendWaylandScreencopy {
		captureAvailable = false
		remediation = "agent capture attempts native screencopy first and will not open portal consent implicitly; start ScreenCast explicitly for an authorized fallback"
	}
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
		CaptureFallback:      capabilities.Capture.Fallback,
		CaptureBackend:       captureBackend,
	}
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
