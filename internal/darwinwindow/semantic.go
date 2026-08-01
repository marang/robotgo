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
	Backend   string
	Nodes     []AccessibilityNode
	Truncated bool
}

func InspectAccessibility(
	ctx context.Context,
	target AccessibilityTarget,
	limits AccessibilityLimits,
) (AccessibilitySnapshot, error) {
	return inspectAccessibility(ctx, target, limits)
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
}
