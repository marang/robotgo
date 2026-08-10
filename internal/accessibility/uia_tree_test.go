package accessibility

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
)

type fakeUIANode struct {
	structure uiaNodeStructure
	details   uiaNodeDetails
	children  []int
}

type fakeUIAQuery struct {
	nodes          map[int]fakeUIANode
	parent         map[int]int
	detailCalls    map[int]int
	processCalls   map[int]int
	structureCalls map[int]int
	releases       map[int]int
	nextError      map[int]error
}

func newFakeUIAQuery(nodes map[int]fakeUIANode) *fakeUIAQuery {
	query := &fakeUIAQuery{
		nodes: nodes, parent: make(map[int]int), detailCalls: make(map[int]int),
		processCalls: make(map[int]int), structureCalls: make(map[int]int),
		releases: make(map[int]int), nextError: make(map[int]error),
	}
	for parent, node := range nodes {
		for _, child := range node.children {
			query.parent[child] = parent
		}
	}
	return query
}

func TestUIARangeValueActionsRequireUsableSmallChange(t *testing.T) {
	tests := []struct {
		name          string
		readOnly      bool
		available     bool
		stepSupported bool
		step          float64
		want          string
	}{
		{name: "writable slider", available: true, stepSupported: true, step: 1, want: "[set-value increment decrement]"},
		{name: "zero step", available: true, stepSupported: true, want: "[set-value]"},
		{name: "nan step", available: true, stepSupported: true, step: math.NaN(), want: "[set-value]"},
		{name: "infinite step", available: true, stepSupported: true, step: math.Inf(1), want: "[set-value]"},
		{name: "missing step", available: true, want: "[set-value]"},
		{name: "read only", readOnly: true, available: true, stepSupported: true, step: 1, want: "[]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := uiaRangeValueActions(test.readOnly, test.available, test.stepSupported, test.step)
			if fmt.Sprint(got) != test.want {
				t.Fatalf("uiaRangeValueActions() = %v, want %s", got, test.want)
			}
		})
	}
}

func TestUIAToggleStateFailsClosedWithoutReadableValidState(t *testing.T) {
	tests := []struct {
		name      string
		state     int32
		supported bool
		checked   bool
		wantErr   bool
	}{
		{name: "off", state: uiaToggleStateOff, supported: true},
		{name: "on", state: uiaToggleStateOn, supported: true, checked: true},
		{name: "indeterminate", state: uiaToggleStateIndeterminate, supported: true},
		{name: "missing state", state: uiaToggleStateOff, wantErr: true},
		{name: "invalid state", state: 3, supported: true, wantErr: true},
		{name: "negative state", state: -1, supported: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checked, err := uiaToggleChecked(test.state, test.supported)
			if checked != test.checked || (err != nil) != test.wantErr {
				t.Fatalf("uiaToggleChecked(%d, %t) = %t, %v, want %t, error=%t",
					test.state, test.supported, checked, err, test.checked, test.wantErr)
			}
		})
	}
}

func TestUIAPatternActionsKeepToggleAndSelectionSemanticsSeparate(t *testing.T) {
	tests := []struct {
		name                       string
		role                       string
		toggle, selectionAvailable bool
		want                       string
	}{
		{name: "toggle checkbox", role: "checkbox", toggle: true, want: "toggle"},
		{name: "checkbox prefers toggle", role: "checkbox", toggle: true, selectionAvailable: true, want: "toggle"},
		{name: "selection-only checkbox", role: "checkbox", selectionAvailable: true},
		{name: "toggle switch", role: "switch", toggle: true, want: "toggle"},
		{name: "selection-only switch", role: "switch", selectionAvailable: true},
		{name: "selection radio", role: "radio", selectionAvailable: true, want: "press"},
		{name: "radio ignores nonstandard toggle", role: "radio", toggle: true, selectionAvailable: true, want: "press"},
		{name: "nonstandard toggle-only radio", role: "radio", toggle: true},
		{name: "selection tab", role: "tab", selectionAvailable: true, want: "press"},
		{name: "selection list item", role: "list-item", selectionAvailable: true, want: "press"},
		{name: "selection menu item", role: "menu-item", selectionAvailable: true, want: "press"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := uiaPatternAction(test.role, test.toggle, test.selectionAvailable); got != test.want {
				t.Fatalf("uiaPatternAction(%q, %t, %t) = %q, want %q",
					test.role, test.toggle, test.selectionAvailable, got, test.want)
			}
		})
	}
}

func TestUIASelectionItemStateRemainsCanonical(t *testing.T) {
	for _, role := range []string{"radio", "tab", "list-item", "menu-item"} {
		if got := uiaSelectionItemState(role, true); got != "selected" {
			t.Fatalf("selected %s state = %q, want selected", role, got)
		}
		if got := uiaSelectionItemState(role, false); got != "" {
			t.Fatalf("unselected %s state = %q, want empty", role, got)
		}
	}
	for _, role := range []string{"checkbox", "switch", "button"} {
		if got := uiaSelectionItemState(role, true); got != "" {
			t.Fatalf("selection-only %s state = %q, want empty", role, got)
		}
	}
}

func TestUIAInteractiveRolesIncludeEveryActionTarget(t *testing.T) {
	for _, role := range []string{
		"button", "checkbox", "combobox", "radio", "switch", "textbox",
		"link", "list-item", "menu-item", "tab", "slider",
	} {
		if !uiaInteractiveRole(role) {
			t.Fatalf("action-capable role %q is not interactive", role)
		}
	}
	for _, role := range []string{"generic", "group", "label", "window"} {
		if uiaInteractiveRole(role) {
			t.Fatalf("structural role %q is interactive", role)
		}
	}
}

func TestNextBoundedStepValueClampsBeforeDispatch(t *testing.T) {
	tests := []struct {
		name      string
		current   float64
		step      float64
		minimum   float64
		maximum   float64
		decrement bool
		want      float64
		wantErr   bool
	}{
		{name: "increment", current: 4, step: 1, minimum: 0, maximum: 10, want: 5},
		{name: "decrement", current: 4, step: 1, minimum: 0, maximum: 10, decrement: true, want: 3},
		{name: "maximum endpoint", current: 10, step: 1, minimum: 0, maximum: 10, want: 10},
		{name: "minimum endpoint", current: 0, step: 1, minimum: 0, maximum: 10, decrement: true, want: 0},
		{name: "upper clamp", current: 9, step: 5, minimum: 0, maximum: 10, want: 10},
		{name: "lower clamp", current: 1, step: 5, minimum: 0, maximum: 10, decrement: true, want: 0},
		{name: "finite overflow", current: math.MaxFloat64, step: math.MaxFloat64, minimum: 0, maximum: math.MaxFloat64, want: math.MaxFloat64},
		{name: "invalid range", current: 1, step: 1, minimum: 2, maximum: 1, wantErr: true},
		{name: "invalid current", current: 11, step: 1, minimum: 0, maximum: 10, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := nextBoundedStepValue(test.current, test.step, test.minimum, test.maximum, test.decrement)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("nextBoundedStepValue() = %v, %v, want %v, error=%v", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestValidateExplicitRangeValueRejectsInvalidInputBeforeDispatch(t *testing.T) {
	for _, test := range []struct {
		name                    string
		value, minimum, maximum float64
		wantErr                 bool
	}{
		{name: "inside", value: 4, minimum: 0, maximum: 10},
		{name: "minimum", value: 0, minimum: 0, maximum: 10},
		{name: "maximum", value: 10, minimum: 0, maximum: 10},
		{name: "below", value: -1, minimum: 0, maximum: 10, wantErr: true},
		{name: "above", value: 11, minimum: 0, maximum: 10, wantErr: true},
		{name: "invalid range", value: 1, minimum: 2, maximum: 1, wantErr: true},
		{name: "nan value", value: math.NaN(), minimum: 0, maximum: 10, wantErr: true},
		{name: "infinite maximum", value: 1, minimum: 0, maximum: math.Inf(1), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateExplicitRangeValue(test.value, test.minimum, test.maximum)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateExplicitRangeValue() error = %v, want error=%v", err, test.wantErr)
			}
		})
	}
}

func (query *fakeUIAQuery) processID(_ context.Context, reference int) (int32, error) {
	query.processCalls[reference]++
	node, ok := query.nodes[reference]
	if !ok {
		return 0, ErrUnavailable
	}
	if len(node.structure.RuntimeID) == 0 {
		return 0, ErrInvalidTree
	}
	return node.structure.RuntimeID[0], nil
}

func (query *fakeUIAQuery) structure(_ context.Context, reference int) (uiaNodeStructure, error) {
	query.structureCalls[reference]++
	node, ok := query.nodes[reference]
	if !ok {
		return uiaNodeStructure{}, ErrUnavailable
	}
	result := node.structure
	result.RuntimeID = append([]int32(nil), result.RuntimeID...)
	return result, nil
}

func (query *fakeUIAQuery) details(_ context.Context, reference int, _ string, _ Limits) (uiaNodeDetails, error) {
	query.detailCalls[reference]++
	node, ok := query.nodes[reference]
	if !ok {
		return uiaNodeDetails{}, ErrUnavailable
	}
	return node.details, nil
}

func (query *fakeUIAQuery) firstChild(_ context.Context, reference int) (int, error) {
	node, ok := query.nodes[reference]
	if !ok {
		return 0, ErrUnavailable
	}
	if len(node.children) == 0 {
		return 0, nil
	}
	return node.children[0], nil
}

func (query *fakeUIAQuery) nextSibling(_ context.Context, reference int) (int, error) {
	if err := query.nextError[reference]; err != nil {
		return 0, err
	}
	parent := query.parent[reference]
	children := query.nodes[parent].children
	for index, child := range children {
		if child == reference && index+1 < len(children) {
			return children[index+1], nil
		}
	}
	return 0, nil
}

func (query *fakeUIAQuery) release(reference int) { query.releases[reference]++ }

func uiaTestLimits() Limits {
	return Limits{
		MaxElements: 16, MaxDepth: 4, MaxStringBytes: 256,
		MaxReferenceBytes: 128, MaxTotalReferenceBytes: 1024,
		AllowedRoles: map[string]bool{
			"window": true, "button": true, "password": true, "label": true,
		},
		ReadName: true, ReadDescription: true, ReadValue: true,
		ReadStates: true, ReadBounds: true, ReadFocus: true, ReadActions: true,
	}
}

func fakeUIAStructure(id int32, processID int32, controlType int32) uiaNodeStructure {
	return uiaNodeStructure{RuntimeID: []int32{processID, id}, ControlType: controlType}
}

func TestBuildUIATreeMinimizesPrivateAndOutOfScopeReads(t *testing.T) {
	nodes := map[int]fakeUIANode{
		1: {
			structure: fakeUIAStructure(1, 42, uiaControlWindow),
			details: uiaNodeDetails{
				Name: "Fixture", States: []string{"enabled"}, Focused: true,
				Bounds: &Bounds{X: 10, Y: 20, Width: 300, Height: 200},
			},
			children: []int{2, 3, 5, 6, 7},
		},
		2: {
			structure: func() uiaNodeStructure {
				value := fakeUIAStructure(2, 42, uiaControlEdit)
				value.Password = true
				return value
			}(),
			details:  uiaNodeDetails{Name: "Password", Value: "secret"},
			children: []int{8},
		},
		3: {
			structure: func() uiaNodeStructure {
				value := fakeUIAStructure(3, 42, uiaControlText)
				value.Offscreen = true
				return value
			}(),
			details:  uiaNodeDetails{Name: "offscreen-secret"},
			children: []int{4},
		},
		4: {
			structure: fakeUIAStructure(4, 42, uiaControlText),
			details:   uiaNodeDetails{Name: "pruned-secret"},
		},
		5: {
			structure: fakeUIAStructure(5, 42, uiaControlEdit),
			details:   uiaNodeDetails{Name: "disallowed-secret", Value: "private"},
		},
		6: {
			structure: fakeUIAStructure(6, 77, uiaControlButton),
			details:   uiaNodeDetails{Name: "foreign-process-secret"},
		},
		7: {
			structure: fakeUIAStructure(7, 42, uiaControlButton),
			details: uiaNodeDetails{
				Name: "Save", States: []string{"enabled"}, Actions: []string{"press", "focus"},
				Bounds: &Bounds{X: 20, Y: 40, Width: 80, Height: 30},
			},
		},
		8: {
			structure: fakeUIAStructure(8, 42, uiaControlText),
			details:   uiaNodeDetails{Name: "nested-password-secret"},
		},
	}
	query := newFakeUIAQuery(nodes)
	snapshot, err := buildUIATree(t.Context(), query, 1, 42, 77, uiaTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Backend != BackendWindowsAutomation || len(snapshot.Nodes) != 5 || !snapshot.Truncated {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Nodes[0].Name != "Fixture" || snapshot.Nodes[1].Role != "password" ||
		!snapshot.Nodes[1].Sensitive || snapshot.Nodes[1].Name != "" ||
		!snapshot.Nodes[2].Offscreen || snapshot.Nodes[3].Role != "textbox" ||
		snapshot.Nodes[3].Name != "" || snapshot.Nodes[4].Name != "Save" ||
		fmt.Sprint(snapshot.Nodes[4].Actions) != "[press focus]" {
		t.Fatalf("nodes = %+v", snapshot.Nodes)
	}
	for _, reference := range []int{2, 3, 5, 6} {
		if query.detailCalls[reference] != 0 {
			t.Fatalf("private/out-of-scope details read for %d", reference)
		}
	}
	if query.structureCalls[6] != 0 {
		t.Fatalf("foreign-process structure was read: %d", query.structureCalls[6])
	}
	if query.structureCalls[4] != 0 || query.releases[4] != 0 {
		t.Fatalf("offscreen descendant was acquired: structure=%d releases=%d", query.structureCalls[4], query.releases[4])
	}
	if query.structureCalls[8] != 0 || query.releases[8] != 0 {
		t.Fatalf("password descendant was acquired: structure=%d releases=%d", query.structureCalls[8], query.releases[8])
	}
	for _, reference := range []int{1, 2, 3, 5, 6, 7} {
		if query.releases[reference] != 1 {
			t.Fatalf("release[%d] = %d", reference, query.releases[reference])
		}
	}
}

func TestBuildUIATreeReleasesCurrentAndPrefetchedSiblingOnError(t *testing.T) {
	nodes := map[int]fakeUIANode{
		1: {structure: fakeUIAStructure(1, 42, uiaControlWindow), children: []int{2, 3}},
		2: {structure: fakeUIAStructure(2, 42, uiaControlButton)},
		3: {structure: fakeUIAStructure(3, 42, uiaControlButton)},
	}
	query := newFakeUIAQuery(nodes)
	query.nextError[2] = ErrUnavailable
	if _, err := buildUIATree(t.Context(), query, 1, 42, 77, uiaTestLimits()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if query.releases[1] != 1 || query.releases[2] != 1 || query.releases[3] != 0 {
		t.Fatalf("releases = %+v", query.releases)
	}

	query = newFakeUIAQuery(nodes)
	nodes[2] = fakeUIANode{structure: fakeUIAStructure(1, 42, uiaControlButton)}
	if _, err := buildUIATree(t.Context(), query, 1, 42, 77, uiaTestLimits()); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("duplicate error = %v", err)
	}
	if query.releases[1] != 1 || query.releases[2] != 1 || query.releases[3] != 1 {
		t.Fatalf("duplicate releases = %+v", query.releases)
	}
}

func TestBuildUIATreeBoundsNodesAndReferences(t *testing.T) {
	nodes := map[int]fakeUIANode{
		1: {structure: fakeUIAStructure(1, 42, uiaControlWindow), children: []int{2}},
		2: {structure: fakeUIAStructure(2, 42, uiaControlButton)},
	}
	limits := uiaTestLimits()
	limits.MaxElements = 1
	query := newFakeUIAQuery(nodes)
	snapshot, err := buildUIATree(t.Context(), query, 1, 42, 77, limits)
	if err != nil || len(snapshot.Nodes) != 1 || !snapshot.Truncated {
		t.Fatalf("bounded snapshot = %+v, %v", snapshot, err)
	}
	if query.releases[1] != 1 || query.releases[2] != 1 || query.structureCalls[2] != 0 {
		t.Fatalf("bounded lifecycle = releases %+v, calls %+v", query.releases, query.structureCalls)
	}

	limits = uiaTestLimits()
	limits.MaxReferenceBytes = 8
	query = newFakeUIAQuery(nodes)
	if _, err := buildUIATree(t.Context(), query, 1, 42, 77, limits); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("reference limit error = %v", err)
	}
	if query.releases[1] != 1 {
		t.Fatalf("root release after reference error = %+v", query.releases)
	}
}

func TestUIARoleReferenceAndNumericContracts(t *testing.T) {
	if got := mapUIAControlType(uiaControlEdit, true, false); got != "password" {
		t.Fatalf("password role = %q", got)
	}
	for control, want := range map[int32]string{
		uiaControlButton: "button", uiaControlComboBox: "combobox",
		uiaControlDataGrid: "table", uiaControlDataItem: "row",
		uiaControlWindow: "window", 99999: "generic",
	} {
		if got := mapUIAControlType(control, false, false); got != want {
			t.Fatalf("role(%d) = %q, want %q", control, got, want)
		}
	}
	if got := mapUIAControlType(uiaControlButton, false, true); got != "switch" {
		t.Fatalf("toggle-pattern button role = %q, want switch", got)
	}
	if got := mapUIAControlType(uiaControlRadioButton, false, true); got != "radio" {
		t.Fatalf("radio with nonstandard toggle pattern role = %q, want radio", got)
	}
	reference, err := encodeUIAReference(42, 77, []int32{1, -2, 3})
	if err != nil || len(reference) != 27 || reference[0] != uiaReferenceVersion {
		t.Fatalf("reference = %x, %v", reference, err)
	}
	pid, handle, runtimeID, err := decodeUIAReference(reference)
	if err != nil || pid != 42 || handle != 77 || fmt.Sprint(runtimeID) != "[1 -2 3]" {
		t.Fatalf("decoded reference = %d %d %v, %v", pid, handle, runtimeID, err)
	}
	for _, invalid := range [][]byte{nil, reference[:len(reference)-1], append(append([]byte(nil), reference...), 0)} {
		if _, _, _, err := decodeUIAReference(invalid); !errors.Is(err, ErrStaleTarget) {
			t.Fatalf("invalid reference %x error = %v", invalid, err)
		}
	}
	if _, err := encodeUIAReference(0, 77, []int32{1}); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("invalid process reference error = %v", err)
	}
	if _, err := uiaNumericValue(math.NaN()); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("NaN error = %v", err)
	}
}
