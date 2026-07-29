package supportmatrix

import (
	"fmt"
	"strings"
)

// MarkdownStart and MarkdownEnd delimit the generated compatibility table.
const (
	MarkdownStart = "<!-- BEGIN GENERATED RUNTIME SUPPORT MATRIX -->"
	MarkdownEnd   = "<!-- END GENERATED RUNTIME SUPPORT MATRIX -->"
)

// RenderMarkdown renders the human-readable table from the checked contract.
func RenderMarkdown(contract Contract) string {
	var body strings.Builder
	body.WriteString(MarkdownStart)
	body.WriteString("\n")
	body.WriteString("| Contract ID | Platform/session | Build mode | Scope | Status | Evidence and limits |\n")
	body.WriteString("|---|---|---|---|---|---|\n")
	for _, row := range contract.Rows {
		fmt.Fprintf(
			&body,
			"| `%s` | %s | %s | %s | %s | %s |\n",
			row.ID,
			row.Platform,
			row.BuildMode,
			row.Scope,
			renderStatus(row.Status),
			renderEvidence(row),
		)
	}
	body.WriteString(MarkdownEnd)
	return body.String()
}

// ReplaceMarkdown replaces exactly one generated support-matrix block.
func ReplaceMarkdown(document string, rendered string) (string, error) {
	start := strings.Index(document, MarkdownStart)
	end := strings.Index(document, MarkdownEnd)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("runtime support matrix markers are missing or out of order")
	}
	if strings.Count(document, MarkdownStart) != 1 ||
		strings.Count(document, MarkdownEnd) != 1 {
		return "", fmt.Errorf("runtime support matrix markers must occur exactly once")
	}
	end += len(MarkdownEnd)
	return document[:start] + rendered + document[end:], nil
}

func renderStatus(status string) string {
	switch status {
	case StatusSupported:
		return "supported"
	case StatusEvidencePending:
		return "implemented / evidence pending"
	case StatusNotClaimed:
		return "not claimed"
	default:
		return status
	}
}

func renderEvidence(row Row) string {
	switch row.Status {
	case StatusSupported:
		return "Blocking checks: " + renderChecks(row.BlockingChecks) + ". " + row.Limits
	case StatusEvidencePending:
		return "Current implementation/preflight checks: " + renderChecks(row.CurrentChecks) +
			". Missing for promotion: " + row.MissingEvidence +
			". Follow-up: [tracking issue](" + row.FollowUp + "). " + row.Limits
	default:
		return row.Limits
	}
}

func renderChecks(checks []string) string {
	quoted := make([]string, 0, len(checks))
	for _, check := range checks {
		quoted = append(quoted, "`"+check+"`")
	}
	return strings.Join(quoted, ", ")
}
