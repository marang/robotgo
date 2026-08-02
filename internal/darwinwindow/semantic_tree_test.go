package darwinwindow

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type fakeAXSemanticNode struct {
	pid       int32
	structure axSemanticStructure
	details   axSemanticDetails
	children  []int
}

type fakeAXSemanticQuery struct {
	nodes         map[int]fakeAXSemanticNode
	releases      map[int]int
	detailCalls   map[int]int
	childrenCalls map[int]int
	err           map[int]error
}

var _ axSemanticQuery[int] = (*fakeAXSemanticQuery)(nil)

func newFakeAXSemanticQuery(nodes map[int]fakeAXSemanticNode) *fakeAXSemanticQuery {
	return &fakeAXSemanticQuery{
		nodes: nodes, releases: make(map[int]int), detailCalls: make(map[int]int),
		childrenCalls: make(map[int]int), err: make(map[int]error),
	}
}

func (query *fakeAXSemanticQuery) processID(ctx context.Context, element int) (int32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := query.err[element]; err != nil {
		return 0, err
	}
	return query.nodes[element].pid, nil
}

func (query *fakeAXSemanticQuery) structure(_ context.Context, element int) (axSemanticStructure, error) {
	return query.nodes[element].structure, nil
}

func (query *fakeAXSemanticQuery) details(
	_ context.Context,
	element int,
	_ string,
	_ AccessibilityLimits,
) (axSemanticDetails, error) {
	query.detailCalls[element]++
	return query.nodes[element].details, nil
}

func (query *fakeAXSemanticQuery) children(_ context.Context, element int) ([]int, bool, error) {
	query.childrenCalls[element]++
	return append([]int(nil), query.nodes[element].children...), false, nil
}

func (query *fakeAXSemanticQuery) release(element int) { query.releases[element]++ }

func fullAccessibilityLimits() AccessibilityLimits {
	return AccessibilityLimits{
		MaxElements: 32, MaxDepth: 8, MaxStringBytes: 1024,
		MaxReferenceBytes: 128, MaxTotalReferenceBytes: 4096,
		AllowedRoles: map[string]bool{
			"window": true, "button": true, "password": true, "group": true,
		},
		ReadName: true, ReadDescription: true, ReadValue: true,
		ReadStates: true, ReadBounds: true, ReadFocus: true, ReadActions: true,
	}
}

func TestBuildAXSemanticTreePreservesPrivacyAndOwnership(t *testing.T) {
	t.Parallel()
	nodes := map[int]fakeAXSemanticNode{
		1: {
			pid: 42, structure: axSemanticStructure{Role: "window"},
			details: axSemanticDetails{Name: "Fixture"}, children: []int{2, 3, 4, 6},
		},
		2: {
			pid: 42, structure: axSemanticStructure{Role: "password", Sensitive: true},
			details: axSemanticDetails{Name: "Password", Value: "secret"}, children: []int{7},
		},
		3: {
			pid: 42, structure: axSemanticStructure{Role: "button"},
			details: axSemanticDetails{
				Name: "Save", States: []string{"enabled"}, Focused: true,
				Actions: []string{"press"}, Bounds: &AccessibilityBounds{X: 1, Y: 2, Width: 3, Height: 4},
			},
		},
		4: {
			pid: 42, structure: axSemanticStructure{Role: "group", Hidden: true},
			details: axSemanticDetails{Name: "hidden-secret"}, children: []int{5},
		},
		5: {pid: 42, structure: axSemanticStructure{Role: "button"}, details: axSemanticDetails{Name: "never-read"}},
		6: {pid: 99, structure: axSemanticStructure{Role: "button"}, details: axSemanticDetails{Name: "foreign"}},
		7: {pid: 42, structure: axSemanticStructure{Role: "label"}, details: axSemanticDetails{Name: "nested-secret"}},
	}
	query := newFakeAXSemanticQuery(nodes)
	snapshot, err := buildAXSemanticTree(t.Context(), query, 1, 42, 77, fullAccessibilityLimits())
	if err != nil {
		t.Fatalf("build tree: %v", err)
	}
	if snapshot.Backend != AccessibilityBackend || len(snapshot.Nodes) != 4 || !snapshot.Truncated {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Nodes[0].Name != "Fixture" || snapshot.Nodes[1].Role != "password" ||
		!snapshot.Nodes[1].Sensitive || snapshot.Nodes[1].Name != "" || snapshot.Nodes[1].Value != "" ||
		snapshot.Nodes[2].Name != "Save" || snapshot.Nodes[3].Name != "" || !snapshot.Nodes[3].Hidden {
		t.Fatalf("nodes = %+v", snapshot.Nodes)
	}
	if query.detailCalls[2] != 0 || query.detailCalls[4] != 0 || query.detailCalls[5] != 0 ||
		query.detailCalls[6] != 0 || query.childrenCalls[4] != 0 || query.childrenCalls[5] != 0 ||
		query.childrenCalls[2] != 0 || query.childrenCalls[6] != 0 || query.childrenCalls[7] != 0 {
		t.Fatalf("sensitive/hidden/foreign query boundary violated: details=%v children=%v", query.detailCalls, query.childrenCalls)
	}
	for _, element := range []int{1, 2, 3, 4, 6} {
		if query.releases[element] != 1 {
			t.Fatalf("element %d releases = %d", element, query.releases[element])
		}
	}
	if query.releases[5] != 0 {
		t.Fatalf("hidden descendant was acquired and released: %d", query.releases[5])
	}
	if query.releases[7] != 0 {
		t.Fatalf("sensitive descendant was acquired and released: %d", query.releases[7])
	}
	if len(snapshot.Nodes[0].Reference) != 11 || len(snapshot.Nodes[1].Reference) != 15 {
		t.Fatalf("observation references have unexpected lengths: %d, %d", len(snapshot.Nodes[0].Reference), len(snapshot.Nodes[1].Reference))
	}
}

func TestBuildAXSemanticTreeRejectsDuplicateAndReleasesPendingSiblings(t *testing.T) {
	t.Parallel()
	query := newFakeAXSemanticQuery(map[int]fakeAXSemanticNode{
		1: {pid: 42, structure: axSemanticStructure{Role: "window"}, children: []int{2, 2, 3}},
		2: {pid: 42, structure: axSemanticStructure{Role: "button"}},
		3: {pid: 42, structure: axSemanticStructure{Role: "button"}},
	})
	_, err := buildAXSemanticTree(t.Context(), query, 1, 42, 77, fullAccessibilityLimits())
	if !errors.Is(err, ErrAccessibilityInvalidTree) {
		t.Fatalf("error = %v", err)
	}
	if query.releases[1] != 1 || query.releases[2] != 2 || query.releases[3] != 1 {
		t.Fatalf("releases = %v", query.releases)
	}
}

func TestBuildAXSemanticTreeClearsPartialSnapshotOnError(t *testing.T) {
	t.Parallel()
	query := newFakeAXSemanticQuery(map[int]fakeAXSemanticNode{
		1: {pid: 42, structure: axSemanticStructure{Role: "window"}, details: axSemanticDetails{Name: "private"}, children: []int{2}},
		2: {pid: 42, structure: axSemanticStructure{Role: "button"}},
	})
	query.err[2] = errors.New("native private failure")
	snapshot, err := buildAXSemanticTree(t.Context(), query, 1, 42, 77, fullAccessibilityLimits())
	if err == nil || snapshot.Nodes != nil || snapshot.Backend != "" {
		t.Fatalf("snapshot=%+v error=%v", snapshot, err)
	}
	if query.releases[1] != 1 || query.releases[2] != 1 {
		t.Fatalf("releases = %v", query.releases)
	}
}

func TestEncodeAccessibilityReferenceRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		pid      int32
		windowID uint32
	}{
		{pid: 0, windowID: 1},
		{pid: 1, windowID: 0},
	} {
		if _, err := encodeAccessibilityReference(test.pid, test.windowID, nil); !errors.Is(err, ErrAccessibilityInvalidTree) {
			t.Fatalf("encode(%d,%d) error = %v", test.pid, test.windowID, err)
		}
	}
	reference, err := encodeAccessibilityReference(42, 77, []uint32{1, 4, 9})
	if err != nil {
		t.Fatal(err)
	}
	pid, windowID, path, err := decodeAccessibilityReference(reference)
	if err != nil || pid != 42 || windowID != 77 || fmt.Sprint(path) != "[1 4 9]" {
		t.Fatalf("decoded reference = %d %d %v, %v", pid, windowID, path, err)
	}
	for _, invalid := range [][]byte{nil, reference[:len(reference)-1], append(append([]byte(nil), reference...), 0)} {
		if _, _, _, err := decodeAccessibilityReference(invalid); !errors.Is(err, ErrAccessibilityStaleTarget) {
			t.Fatalf("invalid reference %x error = %v", invalid, err)
		}
	}
}

func TestMapAXRoleUsesOnlyFixedVocabulary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role      string
		subrole   string
		sensitive bool
		want      string
	}{
		{role: "AXWindow", want: "window"},
		{role: "AXWindow", subrole: "AXDialog", want: "dialog"},
		{role: "AXTextField", want: "textbox"},
		{role: "AXTextField", subrole: "AXSecureTextField", sensitive: true, want: "password"},
		{role: "AXUnknownPrivateRole", want: "generic"},
	}
	for _, test := range tests {
		if got := mapAXRole(test.role, test.subrole, test.sensitive); got != test.want {
			t.Fatalf("mapAXRole(%q,%q,%t) = %q, want %q", test.role, test.subrole, test.sensitive, got, test.want)
		}
	}
	if !structuralAXRole("window") || structuralAXRole("button") {
		t.Fatal("structural AX role classification is inconsistent")
	}
}

func TestExpansionActionMatchesExecutableAXPaths(t *testing.T) {
	tests := []struct {
		name             string
		expanded         bool
		hasExpanded      bool
		expandedSettable bool
		showMenu         bool
		want             string
	}{
		{name: "settable expanded", expanded: true, hasExpanded: true, expandedSettable: true, want: "collapse"},
		{name: "read-only expanded", expanded: true, hasExpanded: true, showMenu: true, want: ""},
		{name: "settable collapsed", hasExpanded: true, expandedSettable: true, want: "expand"},
		{name: "menu collapsed", hasExpanded: true, showMenu: true, want: "expand"},
		{name: "menu without state", showMenu: true, want: "expand"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := expansionAction(test.expanded, test.hasExpanded, test.expandedSettable, test.showMenu); got != test.want {
				t.Fatalf("expansionAction() = %q, want %q", got, test.want)
			}
		})
	}
}
