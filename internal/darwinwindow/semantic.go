package darwinwindow

import (
	"context"
	"errors"
)

const AccessibilityBackend = "macos-accessibility"

var (
	ErrAccessibilityUnavailable = errors.New("macOS Accessibility inspection is unavailable")
	ErrAccessibilityStaleTarget = errors.New("macOS Accessibility target is stale")
	ErrAccessibilityInvalidTree = errors.New("macOS Accessibility returned an invalid tree")
)

type AccessibilityTarget struct {
	ProcessID     int
	CGWindowID    int
	ExpectedTitle string
}

type AccessibilityLimits struct {
	MaxElements            uint32
	MaxDepth               uint32
	MaxStringBytes         uint32
	MaxReferenceBytes      uint32
	MaxTotalReferenceBytes uint32
	AllowedRoles           map[string]bool
	ReadName               bool
	ReadDescription        bool
	ReadValue              bool
	ReadStates             bool
	ReadBounds             bool
	ReadFocus              bool
	ReadActions            bool
}

type AccessibilityBounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

type AccessibilityNode struct {
	Reference   []byte
	Parent      int
	Depth       uint32
	Role        string
	Name        string
	Description string
	Value       string
	Sensitive   bool
	Hidden      bool
	Offscreen   bool
	States      []string
	Bounds      *AccessibilityBounds
	Focused     bool
	Actions     []string
}

type AccessibilitySnapshot struct {
	Backend           string
	Nodes             []AccessibilityNode
	Truncated         bool
	IdentityTruncated bool
}

type AccessibilityElementExpectation struct {
	Role      string
	Name      string
	Sensitive bool
	States    []string
	Bounds    *AccessibilityBounds
	Actions   []string
}

type AccessibilityActionRequest struct {
	Target          AccessibilityTarget
	Reference       []byte
	Expected        AccessibilityElementExpectation
	Action          string
	Value           []byte
	Postcondition   *AccessibilityElementCondition
	BeforeFinalGate func(context.Context) error
}

type AccessibilityActionResult struct {
	Dispatched       bool
	AlreadySatisfied bool
	CleanupComplete  bool
}

type AccessibilityConditionResult struct {
	Satisfied       bool
	CleanupComplete bool
}

func InspectAccessibility(
	ctx context.Context,
	target AccessibilityTarget,
	limits AccessibilityLimits,
) (AccessibilitySnapshot, error) {
	return inspectAccessibility(ctx, target, limits)
}

func ActAccessibility(ctx context.Context, request AccessibilityActionRequest) (AccessibilityActionResult, error) {
	request.Value = append([]byte(nil), request.Value...)
	defer clear(request.Value)
	return actAccessibility(ctx, request)
}

func CheckAccessibility(ctx context.Context, request AccessibilityActionRequest) (AccessibilityConditionResult, error) {
	request.Value = append([]byte(nil), request.Value...)
	defer clear(request.Value)
	return checkAccessibility(ctx, request)
}

func clearAccessibilitySnapshot(snapshot *AccessibilitySnapshot) {
	if snapshot == nil {
		return
	}
	for index := range snapshot.Nodes {
		clear(snapshot.Nodes[index].Reference)
		snapshot.Nodes[index] = AccessibilityNode{}
	}
	clear(snapshot.Nodes)
	snapshot.Nodes = nil
	snapshot.Backend = ""
	snapshot.Truncated = false
	snapshot.IdentityTruncated = false
}

func expansionAction(expanded, hasExpanded, expandedSettable, showMenu bool) string {
	if hasExpanded {
		if expanded {
			if expandedSettable {
				return "collapse"
			}
			return ""
		}
		if expandedSettable || showMenu {
			return "expand"
		}
		return ""
	}
	if showMenu {
		return "expand"
	}
	return ""
}
