// Package accessibility provides bounded, read-only native accessibility
// discovery without exposing platform object references outside the process.
package accessibility

import (
	"context"
	"errors"
)

const (
	BackendATSPI2            = "at-spi2"
	BackendWindowsAutomation = "windows-ui-automation"
)

var (
	ErrUnsupported      = errors.New("accessibility: unsupported")
	ErrUnavailable      = errors.New("accessibility: unavailable")
	ErrPermissionDenied = errors.New("accessibility: permission denied")
	ErrStaleTarget      = errors.New("accessibility: stale target")
	ErrInvalidTree      = errors.New("accessibility: invalid tree")
)

// Capability is a sanitized, non-prompting native-backend probe result.
type Capability struct {
	Available        bool
	Backend          string
	Reason           string
	Notes            string
	PermissionDenied bool
}

// Target identifies one process or native window and one exact top-level
// accessible title. Platform backends accept only target forms they can bind
// to and revalidate without widening the observation scope.
type Target struct {
	ProcessID          int
	NativeWindowHandle int
	ExpectedTitle      string
}

// Limits bounds both native queries and the returned snapshot.
type Limits struct {
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

// Bounds is one global logical accessibility rectangle.
type Bounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Node is a bounded platform-neutral projection. Reference remains private to
// the agent session and must be zeroed when its observation is released.
type Node struct {
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
	Bounds      *Bounds
	Focused     bool
	Actions     []string
}

// Snapshot is one bounded accessibility tree rooted at the exact target.
type Snapshot struct {
	Backend   string
	Nodes     []Node
	Truncated bool
}

// Probe checks native availability without opening an OS consent dialog.
func Probe(ctx context.Context) Capability { return probe(ctx) }

// Inspect returns one bounded native semantic tree.
func Inspect(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	return inspect(ctx, target, limits)
}

func clearSnapshot(snapshot *Snapshot) {
	if snapshot == nil {
		return
	}
	for index := range snapshot.Nodes {
		clear(snapshot.Nodes[index].Reference)
		snapshot.Nodes[index] = Node{}
	}
	clear(snapshot.Nodes)
	snapshot.Nodes = nil
	snapshot.Backend = ""
}
