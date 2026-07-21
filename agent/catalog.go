package agent

import robotgo "github.com/marang/robotgo"

func buildCatalog(policy Policy, capabilities robotgo.RuntimeCapabilities) OperationCatalog {
	return OperationCatalog{
		SchemaVersion: CatalogSchemaVersion,
		Operations: []OperationCapability{
			operationCapability(OperationMove, policy, capabilities.Mouse),
			operationCapability(OperationClick, policy, capabilities.Mouse),
			operationCapability(OperationTypeText, policy, capabilities.Keyboard),
		},
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
