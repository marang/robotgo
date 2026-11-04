package compositorevidence

import (
	"errors"
	"strings"
)

// Provider identifies the evidence execution service. The empty configuration
// value preserves the historical GitHub Actions contract.
type Provider string

const (
	ProviderGitHubActions Provider = "github-actions"
	ProviderDagu          Provider = "dagu"
	daguSwayWorkflow               = "robotgo-ovh-ci"
)

// DaguIdentity binds a cell to the real scheduler attempt, checked-out tree,
// and immutable worker image. Values are supplied explicitly by the workflow.
type DaguIdentity struct {
	AttemptID  string `json:"attempt_id"`
	Step       string `json:"step"`
	SourceTree string `json:"source_tree"`
	Image      string `json:"image"`
}

// executionIdentity is a value so identity comparisons cannot accidentally
// compare metadata pointers or treat missing provenance as an empty document.
type executionIdentity struct {
	provider   Provider
	workflow   string
	runID      string
	runAttempt int
	dagu       DaguIdentity
	hasDagu    bool
}

func normalizedProvider(provider Provider) Provider {
	if provider == "" {
		return ProviderGitHubActions
	}
	return provider
}

func execution(provider Provider, workflow, runID string, runAttempt int, dagu *DaguIdentity) executionIdentity {
	identity := executionIdentity{provider: normalizedProvider(provider), workflow: workflow, runID: runID, runAttempt: runAttempt}
	if dagu != nil {
		identity.dagu = *dagu
		identity.hasDagu = true
	}
	return identity
}

func (config PreflightConfig) execution() executionIdentity {
	return execution(config.Provider, config.Workflow, config.RunID, config.RunAttempt, config.Dagu)
}

func (report PreflightReport) execution() executionIdentity {
	return execution(report.Provider, report.Workflow, report.RunID, report.RunAttempt, report.Dagu)
}

func (config FinalizeConfig) execution() executionIdentity {
	return execution(config.Provider, config.Workflow, config.RunID, config.RunAttempt, config.Dagu)
}

func (expected VerifyExpectations) execution() executionIdentity {
	return execution(expected.Provider, expected.Workflow, expected.RunID, expected.RunAttempt, expected.Dagu)
}

func (ci CI) execution() executionIdentity {
	return execution(ci.Provider, ci.Workflow, ci.RunID, ci.RunAttempt, ci.Dagu)
}

func validateExecution(cell Cell, identity executionIdentity) error {
	switch identity.provider {
	case ProviderGitHubActions:
		if identity.hasDagu {
			return errors.New("GitHub Actions evidence must not contain Dagu provenance")
		}
		if err := validateWorkflow(cell, identity.workflow); err != nil {
			return err
		}
		if !runIDPattern.MatchString(identity.runID) || identity.runAttempt <= 0 {
			return errors.New("GitHub Actions run identity requires a positive decimal run ID and attempt")
		}
	case ProviderDagu:
		if !identity.hasDagu {
			return errors.New("Dagu evidence requires provenance")
		}
		if identity.workflow != daguSwayWorkflow {
			return errors.New("Dagu workflow does not match protected Sway DAG")
		}
		if identity.runAttempt != 0 {
			return errors.New("Dagu evidence must use the scheduler attempt ID instead of a numeric run attempt")
		}
		if err := validateLabel("Dagu run ID", identity.runID); err != nil {
			return err
		}
		if err := validateLabel("Dagu attempt ID", identity.dagu.AttemptID); err != nil {
			return err
		}
		step := daguStep(cell)
		if step == "" || identity.dagu.Step != step {
			return errors.New("Dagu step does not match protected Sway cell")
		}
		if len(identity.dagu.SourceTree) != 40 || !gitObjectPattern.MatchString(identity.dagu.SourceTree) {
			return errors.New("Dagu source tree must be a lowercase 40-character Git tree ID")
		}
		if !strings.HasPrefix(identity.dagu.Image, "sha256:") || !digestPattern.MatchString(strings.TrimPrefix(identity.dagu.Image, "sha256:")) {
			return errors.New("Dagu image must be an immutable sha256 digest")
		}
	default:
		return errors.New("unsupported compositor evidence provider")
	}
	return nil
}

func daguStep(cell Cell) string {
	switch cell {
	case CellNativeInput:
		return "native_input"
	case CellNativeCapture:
		return "native_capture"
	case CellNativeWindow:
		return "native_window"
	case CellNativeOutput:
		return "native_output"
	case CellNativeOutputMulti:
		return "native_output_multi"
	case CellPortalAvailability:
		return "portal_availability"
	default:
		return ""
	}
}
