package supportmatrix

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// SchemaVersion is the only support-contract schema understood by this package.
	SchemaVersion = 1

	// StatusSupported identifies a scope backed by release-blocking evidence.
	StatusSupported = "supported"
	// StatusEvidencePending identifies implemented behavior that is not in the
	// supported release scope because required runtime evidence is missing.
	StatusEvidencePending = "implemented_evidence_pending"
	// StatusNotClaimed identifies a scope for which the release makes no support claim.
	StatusNotClaimed = "not_claimed"
)

var (
	rowIDPattern          = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	linearIssueURLPattern = regexp.MustCompile(
		`^/riotbox/issue/LAB-[0-9]+(?:/[a-z0-9]+(?:-[a-z0-9]+)*)?$`,
	)
)

// Contract is the machine-readable source for Runtime Compatibility Matrix v1.
type Contract struct {
	SchemaVersion int      `json:"schemaVersion"`
	Published     string   `json:"published"`
	ReleaseRun    string   `json:"releaseRun"`
	ReleaseCommit string   `json:"releaseCommit"`
	ReleaseChecks []string `json:"releaseChecks"`
	Rows          []Row    `json:"rows"`
}

// Row describes one deliberately bounded platform and build-mode support claim.
type Row struct {
	ID              string   `json:"id"`
	Platform        string   `json:"platform"`
	BuildMode       string   `json:"buildMode"`
	Scope           string   `json:"scope"`
	Status          string   `json:"status"`
	BlockingChecks  []string `json:"blockingChecks,omitempty"`
	CurrentChecks   []string `json:"currentChecks,omitempty"`
	Limits          string   `json:"limits"`
	MissingEvidence string   `json:"missingEvidence,omitempty"`
	FollowUp        string   `json:"followUp,omitempty"`
}

// Load reads and validates one support contract.
func Load(path string) (Contract, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Contract{}, fmt.Errorf("read support contract: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var contract Contract
	if err := decoder.Decode(&contract); err != nil {
		return Contract{}, fmt.Errorf("decode support contract: %w", err)
	}
	var trailing any
	switch err := decoder.Decode(&trailing); {
	case errors.Is(err, io.EOF):
	case err != nil:
		return Contract{}, fmt.Errorf("decode support contract trailing data: %w", err)
	default:
		return Contract{}, errors.New("decode support contract: trailing JSON value")
	}
	if err := contract.Validate(); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

// Validate enforces the support-state and release-evidence contract.
func (contract Contract) Validate() error {
	if contract.SchemaVersion != SchemaVersion {
		return fmt.Errorf(
			"support contract schema = %d, want %d",
			contract.SchemaVersion,
			SchemaVersion,
		)
	}
	if _, err := time.Parse(time.DateOnly, contract.Published); err != nil {
		return fmt.Errorf("invalid published date %q: %w", contract.Published, err)
	}
	if !validReleaseRunURL(contract.ReleaseRun) {
		return fmt.Errorf("releaseRun is not an exact RobotGo Actions run")
	}
	if len(contract.ReleaseCommit) != 40 {
		return fmt.Errorf("releaseCommit must be a full Git SHA")
	}
	if _, err := hex.DecodeString(contract.ReleaseCommit); err != nil {
		return fmt.Errorf("releaseCommit must be a lowercase hexadecimal Git SHA")
	}
	if contract.ReleaseCommit != strings.ToLower(contract.ReleaseCommit) {
		return fmt.Errorf("releaseCommit must be a lowercase hexadecimal Git SHA")
	}
	if len(contract.ReleaseChecks) == 0 {
		return errors.New("releaseChecks is empty")
	}
	if !slices.IsSorted(contract.ReleaseChecks) {
		return errors.New("releaseChecks must be sorted")
	}
	checks := make(map[string]struct{}, len(contract.ReleaseChecks))
	for index, check := range contract.ReleaseChecks {
		if strings.TrimSpace(check) != check || check == "" {
			return fmt.Errorf("releaseChecks[%d] is empty or not trimmed", index)
		}
		if strings.ContainsAny(check, "|`\r\n") {
			return fmt.Errorf("releaseChecks[%d] is not safe to render as Markdown", index)
		}
		if _, exists := checks[check]; exists {
			return fmt.Errorf("duplicate release check %q", check)
		}
		checks[check] = struct{}{}
	}

	if len(contract.Rows) == 0 {
		return errors.New("support rows are empty")
	}
	rowIDs := make(map[string]struct{}, len(contract.Rows))
	for index, row := range contract.Rows {
		if err := validateRow(row, checks); err != nil {
			return fmt.Errorf("row %d (%s): %w", index, row.ID, err)
		}
		if _, exists := rowIDs[row.ID]; exists {
			return fmt.Errorf("duplicate support row ID %q", row.ID)
		}
		rowIDs[row.ID] = struct{}{}
	}
	return nil
}

func validateRow(row Row, releaseChecks map[string]struct{}) error {
	for field, value := range map[string]string{
		"id":        row.ID,
		"platform":  row.Platform,
		"buildMode": row.BuildMode,
		"scope":     row.Scope,
		"limits":    row.Limits,
	} {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%s is empty or not trimmed", field)
		}
		if strings.ContainsAny(value, "|\r\n") {
			return fmt.Errorf("%s contains a Markdown table delimiter or newline", field)
		}
	}
	if !rowIDPattern.MatchString(row.ID) {
		return errors.New("id must use lowercase kebab-case")
	}
	for field, checks := range map[string][]string{
		"blockingChecks": row.BlockingChecks,
		"currentChecks":  row.CurrentChecks,
	} {
		if !slices.IsSorted(checks) {
			return fmt.Errorf("%s must be sorted", field)
		}
		for index, check := range checks {
			if index > 0 && check == checks[index-1] {
				return fmt.Errorf("%s contains duplicate %q", field, check)
			}
			if check == "" || strings.TrimSpace(check) != check {
				return fmt.Errorf("%s contains an empty or untrimmed check", field)
			}
			if _, exists := releaseChecks[check]; !exists {
				return fmt.Errorf("%s references unknown release check %q", field, check)
			}
		}
	}

	switch row.Status {
	case StatusSupported:
		if len(row.BlockingChecks) == 0 {
			return errors.New("supported scope has no blocking check")
		}
		if len(row.CurrentChecks) != 0 ||
			row.MissingEvidence != "" ||
			row.FollowUp != "" {
			return errors.New("supported scope also contains pending-evidence fields")
		}
	case StatusEvidencePending:
		if len(row.BlockingChecks) != 0 {
			return errors.New("evidence-pending scope cannot declare blocking support checks")
		}
		if len(row.CurrentChecks) == 0 {
			return errors.New("evidence-pending scope has no current implementation checks")
		}
		if strings.TrimSpace(row.MissingEvidence) == "" {
			return errors.New("evidence-pending scope does not state missing evidence")
		}
		if strings.TrimSpace(row.MissingEvidence) != row.MissingEvidence ||
			strings.ContainsAny(row.MissingEvidence, "|\r\n") {
			return errors.New("evidence-pending scope has invalid missing evidence text")
		}
		if !validLinearIssueURL(row.FollowUp) {
			return errors.New("evidence-pending scope has no Linear follow-up")
		}
	case StatusNotClaimed:
		if len(row.BlockingChecks) != 0 || len(row.CurrentChecks) != 0 {
			return errors.New("not-claimed scope cannot contain evidence checks")
		}
		if row.MissingEvidence != "" || row.FollowUp != "" {
			return errors.New("not-claimed scope cannot contain pending-evidence fields")
		}
	default:
		return fmt.Errorf("unknown status %q", row.Status)
	}
	return nil
}

func validReleaseRunURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host != "github.com" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return false
	}
	const prefix = "/marang/robotgo/actions/runs/"
	if !strings.HasPrefix(parsed.EscapedPath(), prefix) {
		return false
	}
	runID := strings.TrimPrefix(parsed.EscapedPath(), prefix)
	if runID == "" || strings.Contains(runID, "/") {
		return false
	}
	for _, digit := range runID {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func validLinearIssueURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host != "linear.app" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return false
	}
	return linearIssueURLPattern.MatchString(parsed.EscapedPath())
}
