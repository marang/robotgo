// Package accessibility provides bounded, read-only native accessibility
// discovery without exposing platform object references outside the process.
package accessibility

import (
	"context"
	"errors"
	"math"
)

const (
	BackendATSPI2             = "at-spi2"
	BackendMacOSAccessibility = "macos-accessibility"
	BackendWindowsAutomation  = "windows-ui-automation"
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
	Backend           string
	Nodes             []Node
	Truncated         bool
	IdentityTruncated bool
}

// ElementExpectation contains only the semantic facts authorized at the agent
// boundary and re-read immediately before native dispatch.
type ElementExpectation struct {
	Role      string
	Name      string
	Sensitive bool
	States    []string
	Bounds    *Bounds
	Actions   []string
}

// ActionRequest binds one retained opaque reference to its exact top-level
// target and expected live semantic identity.
type ActionRequest struct {
	Target          Target
	Reference       []byte
	Expected        ElementExpectation
	Action          string
	Value           []byte
	Postcondition   *ElementCondition
	BeforeFinalGate func(context.Context) error
}

type finalGateCallbackError struct{ cause error }

func (err *finalGateCallbackError) Error() string {
	return "accessibility: final gate accounting failed"
}
func (err *finalGateCallbackError) Unwrap() error { return err.cause }

func runFinalGateCallback(ctx context.Context, callback func(context.Context) error) error {
	if callback == nil {
		return nil
	}
	if err := callback(ctx); err != nil {
		return &finalGateCallbackError{cause: err}
	}
	return nil
}

func finalGateCallbackCause(err error) (error, bool) {
	var callbackErr *finalGateCallbackError
	if !errors.As(err, &callbackErr) {
		return nil, false
	}
	return callbackErr.cause, true
}

// ActionResult identifies the irreversible native dispatch boundary and
// whether an idempotent final gate proved that dispatch was unnecessary.
type ActionResult struct {
	Dispatched       bool
	AlreadySatisfied bool
	CleanupComplete  bool
}

// ConditionResult is one read-only check of an observation-bound element.
type ConditionResult struct {
	Satisfied       bool
	CleanupComplete bool
}

// Probe checks native availability without opening an OS consent dialog.
func Probe(ctx context.Context) Capability { return probe(ctx) }

// Inspect returns one bounded native semantic tree.
func Inspect(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	return inspect(ctx, target, limits)
}

// Act re-resolves and revalidates one retained element before performing one
// native semantic action. It never uses pointer or keyboard fallback.
func Act(ctx context.Context, request ActionRequest) (ActionResult, error) {
	request.Value = append([]byte(nil), request.Value...)
	defer clear(request.Value)
	return act(ctx, request)
}

// Check re-resolves one retained element and evaluates its postcondition
// without performing an action or using a fallback backend.
func Check(ctx context.Context, request ActionRequest) (ConditionResult, error) {
	request.Value = append([]byte(nil), request.Value...)
	defer clear(request.Value)
	return check(ctx, request)
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
	snapshot.Truncated = false
	snapshot.IdentityTruncated = false
}

func equalAccessibilityBounds(left, right *Bounds) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func validateExplicitRangeValue(value, minimum, maximum float64) error {
	if minimum > maximum || value < minimum || value > maximum ||
		math.IsNaN(value) || math.IsNaN(minimum) || math.IsNaN(maximum) ||
		math.IsInf(value, 0) || math.IsInf(minimum, 0) || math.IsInf(maximum, 0) {
		return ErrInvalidTree
	}
	return nil
}

func nextBoundedStepValue(current, step, minimum, maximum float64, decrement bool) (float64, error) {
	if step <= 0 || minimum > maximum || current < minimum || current > maximum ||
		math.IsNaN(current) || math.IsNaN(step) || math.IsNaN(minimum) || math.IsNaN(maximum) ||
		math.IsInf(current, 0) || math.IsInf(step, 0) || math.IsInf(minimum, 0) || math.IsInf(maximum, 0) {
		return 0, ErrInvalidTree
	}
	if decrement {
		step = -step
	}
	next := current + step
	if next < minimum {
		return minimum, nil
	}
	if next > maximum {
		return maximum, nil
	}
	return next, nil
}
