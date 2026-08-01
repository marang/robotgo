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
			details: uiaNodeDetails{Name: "Password", Value: "secret"},
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
	}
	query := newFakeUIAQuery(nodes)
	snapshot, err := buildUIATree(t.Context(), query, 1, 42, uiaTestLimits())
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
	if _, err := buildUIATree(t.Context(), query, 1, 42, uiaTestLimits()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if query.releases[1] != 1 || query.releases[2] != 1 || query.releases[3] != 0 {
		t.Fatalf("releases = %+v", query.releases)
	}

	query = newFakeUIAQuery(nodes)
	nodes[2] = fakeUIANode{structure: fakeUIAStructure(1, 42, uiaControlButton)}
	if _, err := buildUIATree(t.Context(), query, 1, 42, uiaTestLimits()); !errors.Is(err, ErrInvalidTree) {
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
	snapshot, err := buildUIATree(t.Context(), query, 1, 42, limits)
	if err != nil || len(snapshot.Nodes) != 1 || !snapshot.Truncated {
		t.Fatalf("bounded snapshot = %+v, %v", snapshot, err)
	}
	if query.releases[1] != 1 || query.releases[2] != 1 || query.structureCalls[2] != 0 {
		t.Fatalf("bounded lifecycle = releases %+v, calls %+v", query.releases, query.structureCalls)
	}

	limits = uiaTestLimits()
	limits.MaxReferenceBytes = 8
	query = newFakeUIAQuery(nodes)
	if _, err := buildUIATree(t.Context(), query, 1, 42, limits); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("reference limit error = %v", err)
	}
	if query.releases[1] != 1 {
		t.Fatalf("root release after reference error = %+v", query.releases)
	}
}

func TestUIARoleReferenceAndNumericContracts(t *testing.T) {
	if got := mapUIAControlType(uiaControlEdit, true); got != "password" {
		t.Fatalf("password role = %q", got)
	}
	for control, want := range map[int32]string{
		uiaControlButton: "button", uiaControlComboBox: "combobox",
		uiaControlDataGrid: "table", uiaControlDataItem: "row",
		uiaControlWindow: "window", 99999: "generic",
	} {
		if got := mapUIAControlType(control, false); got != want {
			t.Fatalf("role(%d) = %q, want %q", control, got, want)
		}
	}
	reference, err := encodeUIAReference(42, []int32{1, -2, 3})
	if err != nil || len(reference) != 19 || reference[0] != uiaReferenceVersion {
		t.Fatalf("reference = %x, %v", reference, err)
	}
	if _, err := encodeUIAReference(0, []int32{1}); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("invalid process reference error = %v", err)
	}
	if _, err := uiaNumericValue(math.NaN()); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("NaN error = %v", err)
	}
}
