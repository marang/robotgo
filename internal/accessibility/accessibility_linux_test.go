//go:build linux

package accessibility

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

type fakeATSPIObject struct {
	children         []atspiReference
	role             uint32
	states           []uint32
	properties       map[string]string
	interfaces       []string
	rect             atspiRect
	text             string
	value            float64
	minimumIncrement float64
	actionCount      int32
	actionNames      []string
	parent           atspiReference
}

type fakeATSPIQuery struct {
	apps            []atspiReference
	pids            map[string]uint32
	pidErrors       map[string]error
	objects         map[string]*fakeATSPIObject
	propertyCalls   map[string]int
	childCalls      map[string]int
	interfaceCalls  map[string]int
	textCalls       map[string]int
	mutationCalls   []string
	setTextValue    string
	setNumericValue float64
	mutationErr     error
}

func (query *fakeATSPIQuery) applications(context.Context) ([]atspiReference, error) {
	return append([]atspiReference(nil), query.apps...), nil
}

func (query *fakeATSPIQuery) processID(_ context.Context, bus string) (uint32, error) {
	if err := query.pidErrors[bus]; err != nil {
		return 0, err
	}
	pid, ok := query.pids[bus]
	if !ok {
		return 0, ErrUnavailable
	}
	return pid, nil
}

func (query *fakeATSPIQuery) childCount(_ context.Context, reference atspiReference) (int32, error) {
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	return int32(len(object.children)), nil
}

func (query *fakeATSPIQuery) child(_ context.Context, reference atspiReference, index int32) (atspiReference, error) {
	object, err := query.object(reference)
	if err != nil || index < 0 || int(index) >= len(object.children) {
		return atspiReference{}, ErrInvalidTree
	}
	query.childCalls[referenceKey(reference)]++
	return object.children[index], nil
}

func (query *fakeATSPIQuery) role(_ context.Context, reference atspiReference) (uint32, error) {
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	return object.role, nil
}

func (query *fakeATSPIQuery) states(_ context.Context, reference atspiReference) ([]uint32, error) {
	object, err := query.object(reference)
	if err != nil {
		return nil, err
	}
	return append([]uint32(nil), object.states...), nil
}

func (query *fakeATSPIQuery) stringProperty(_ context.Context, reference atspiReference, name string) (string, error) {
	object, err := query.object(reference)
	if err != nil {
		return "", err
	}
	query.propertyCalls[referenceKey(reference)+":"+name]++
	return object.properties[name], nil
}

func (query *fakeATSPIQuery) interfaces(_ context.Context, reference atspiReference) ([]string, error) {
	object, err := query.object(reference)
	if err != nil {
		return nil, err
	}
	query.interfaceCalls[referenceKey(reference)]++
	return append([]string(nil), object.interfaces...), nil
}

func (query *fakeATSPIQuery) extents(_ context.Context, reference atspiReference) (atspiRect, error) {
	object, err := query.object(reference)
	if err != nil {
		return atspiRect{}, err
	}
	return object.rect, nil
}

func (query *fakeATSPIQuery) text(_ context.Context, reference atspiReference, _ int32) (string, error) {
	object, err := query.object(reference)
	if err != nil {
		return "", err
	}
	query.textCalls[referenceKey(reference)]++
	return object.text, nil
}

func (query *fakeATSPIQuery) currentValue(_ context.Context, reference atspiReference) (float64, error) {
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	return object.value, nil
}

func (query *fakeATSPIQuery) actionCount(_ context.Context, reference atspiReference) (int32, error) {
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	return object.actionCount, nil
}

func (query *fakeATSPIQuery) actionName(_ context.Context, reference atspiReference, index int32) (string, error) {
	object, err := query.object(reference)
	if err != nil || index < 0 || int(index) >= len(object.actionNames) {
		return "", ErrInvalidTree
	}
	return object.actionNames[index], nil
}

func (query *fakeATSPIQuery) parent(_ context.Context, reference atspiReference) (atspiReference, error) {
	object, err := query.object(reference)
	if err != nil {
		return atspiReference{}, err
	}
	return object.parent, nil
}

func (query *fakeATSPIQuery) doAction(_ context.Context, reference atspiReference, index int32) (bool, error) {
	query.mutationCalls = append(query.mutationCalls, fmt.Sprintf("action:%s:%d", referenceKey(reference), index))
	return query.mutationErr == nil, query.mutationErr
}

func (query *fakeATSPIQuery) grabFocus(_ context.Context, reference atspiReference) (bool, error) {
	query.mutationCalls = append(query.mutationCalls, "focus:"+referenceKey(reference))
	return query.mutationErr == nil, query.mutationErr
}

func (query *fakeATSPIQuery) setTextContents(_ context.Context, reference atspiReference, value string) (bool, error) {
	query.mutationCalls = append(query.mutationCalls, "text:"+referenceKey(reference))
	query.setTextValue = value
	return query.mutationErr == nil, query.mutationErr
}

func (query *fakeATSPIQuery) minimumIncrement(_ context.Context, reference atspiReference) (float64, error) {
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	return object.minimumIncrement, nil
}

func (query *fakeATSPIQuery) setCurrentValue(_ context.Context, reference atspiReference, value float64) error {
	query.mutationCalls = append(query.mutationCalls, "value:"+referenceKey(reference))
	query.setNumericValue = value
	return query.mutationErr
}

func (query *fakeATSPIQuery) object(reference atspiReference) (*fakeATSPIObject, error) {
	object, ok := query.objects[referenceKey(reference)]
	if !ok {
		return nil, ErrUnavailable
	}
	return object, nil
}

func referenceKey(reference atspiReference) string {
	return reference.Bus + "\x00" + string(reference.Path)
}

func atspiTestReference(id string) atspiReference {
	return atspiReference{Bus: ":1.42", Path: dbus.ObjectPath("/fixture/" + id)}
}

func atspiTestStates(states ...uint32) []uint32 {
	result := []uint32{0, 0}
	for _, state := range states {
		result[state/32] |= uint32(1) << (state % 32)
	}
	return result
}

func newFakeATSPIQuery(objects map[atspiReference]*fakeATSPIObject) *fakeATSPIQuery {
	query := &fakeATSPIQuery{
		pids: make(map[string]uint32), pidErrors: make(map[string]error),
		objects:       make(map[string]*fakeATSPIObject),
		propertyCalls: make(map[string]int), childCalls: make(map[string]int),
		interfaceCalls: make(map[string]int), textCalls: make(map[string]int),
	}
	for reference, object := range objects {
		query.objects[referenceKey(reference)] = object
	}
	return query
}

func atspiTestLimits() Limits {
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

func TestFindATSPITargetRequiresOneExactProcessWindow(t *testing.T) {
	application := atspiTestReference("application")
	staleApplication := atspiReference{Bus: ":1.41", Path: dbus.ObjectPath("/fixture/stale")}
	window := atspiTestReference("window")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window:      {role: 23, properties: map[string]string{"Name": "Fixture"}},
	})
	query.apps = []atspiReference{staleApplication, application}
	query.pidErrors[staleApplication.Bus] = dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"}
	query.pids[application.Bus] = 42

	got, err := findATSPITarget(t.Context(), query, Target{ProcessID: 42, ExpectedTitle: "Fixture"})
	if err != nil || got != window {
		t.Fatalf("target = %+v, %v", got, err)
	}
	if _, err := findATSPITarget(t.Context(), query, Target{ProcessID: 7, ExpectedTitle: "Fixture"}); !errors.Is(err, ErrStaleTarget) {
		t.Fatalf("wrong-process target error = %v", err)
	}
	query.objects[referenceKey(application)].children = append(
		query.objects[referenceKey(application)].children, window,
	)
	if _, err := findATSPITarget(t.Context(), query, Target{ProcessID: 42, ExpectedTitle: "Fixture"}); !errors.Is(err, ErrStaleTarget) {
		t.Fatalf("ambiguous target error = %v", err)
	}
}

func TestBuildATSPITreeMinimizesSensitiveAndHiddenReads(t *testing.T) {
	window := atspiTestReference("window")
	password := atspiTestReference("password")
	passwordChild := atspiTestReference("password-child")
	button := atspiTestReference("button")
	hidden := atspiTestReference("hidden")
	disallowed := atspiTestReference("disallowed")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		window: {
			children: []atspiReference{password, button, hidden, disallowed}, role: atspiRoleFrame,
			states:     atspiTestStates(8, 11, 12, 25, 30),
			properties: map[string]string{"Name": "Fixture", "Description": "Window description"},
			interfaces: []string{"Component"}, rect: atspiRect{X: 10, Y: 20, Width: 300, Height: 200},
		},
		password: {
			children: []atspiReference{passwordChild},
			role:     atspiRolePasswordText, states: atspiTestStates(7, 8, 11, 25, 30),
			properties: map[string]string{"Name": "Password", "Description": "private", "Value": "secret"},
			interfaces: []string{"Text", "EditableText"}, text: "correct horse battery staple",
		},
		passwordChild: {
			role: atspiRoleLabel, states: atspiTestStates(25, 30),
			properties: map[string]string{"Name": "nested-password-secret"},
		},
		button: {
			role: atspiRoleButton, states: atspiTestStates(8, 11, 25, 30),
			properties: map[string]string{"Name": "Save", "Description": "Store changes"},
			interfaces: []string{"Action", "Component"}, actionCount: 1, actionNames: []string{"click"},
			rect: atspiRect{X: 20, Y: 40, Width: 80, Height: 30},
		},
		hidden: {
			role: atspiRoleLabel, states: atspiTestStates(8),
			properties: map[string]string{"Name": "hidden-secret", "Description": "private"},
		},
		disallowed: {
			role: atspiRoleText, states: atspiTestStates(8, 25, 30),
			properties: map[string]string{"Name": "out-of-policy", "Description": "private"},
			interfaces: []string{"Text"}, text: "private value",
		},
	})

	snapshot, err := buildATSPITree(t.Context(), query, window, atspiTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Backend != BackendATSPI2 || len(snapshot.Nodes) != 5 || !snapshot.Truncated {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	root, passwordNode, buttonNode, hiddenNode, disallowedNode :=
		snapshot.Nodes[0], snapshot.Nodes[1], snapshot.Nodes[2], snapshot.Nodes[3], snapshot.Nodes[4]
	if root.Role != "window" || root.Name != "Fixture" || !root.Focused || root.Bounds == nil ||
		passwordNode.Role != "password" || !passwordNode.Sensitive || passwordNode.Name != "" || passwordNode.Value != "" ||
		buttonNode.Name != "Save" || buttonNode.Bounds == nil || fmt.Sprint(buttonNode.Actions) != "[press focus]" ||
		!hiddenNode.Hidden || hiddenNode.Name != "" || disallowedNode.Role != "textbox" ||
		disallowedNode.Name != "" || disallowedNode.Value != "" {
		t.Fatalf("projected nodes = %+v", snapshot.Nodes)
	}
	for _, forbidden := range []atspiReference{password, hidden, disallowed} {
		key := referenceKey(forbidden)
		for _, property := range []string{"Name", "Description"} {
			if calls := query.propertyCalls[key+":"+property]; calls != 0 {
				t.Fatalf("sensitive/hidden property %q read %d times", property, calls)
			}
		}
		if query.interfaceCalls[key] != 0 || query.textCalls[key] != 0 {
			t.Fatalf("sensitive/hidden node queried interfaces=%d text=%d",
				query.interfaceCalls[key], query.textCalls[key])
		}
	}
	if query.childCalls[referenceKey(password)] != 0 ||
		query.propertyCalls[referenceKey(passwordChild)+":"+atspiPropertyName] != 0 {
		t.Fatal("password descendant was traversed or read")
	}
}

func TestBuildATSPITreeEnforcesElementDepthAndReferenceLimits(t *testing.T) {
	root := atspiTestReference("root")
	child := atspiTestReference("child")
	grandchild := atspiTestReference("grandchild")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		root: {
			children: []atspiReference{child}, role: 23,
			states: atspiTestStates(8, 25, 30), properties: map[string]string{},
		},
		child: {
			children: []atspiReference{grandchild}, role: 39,
			states: atspiTestStates(25, 30), properties: map[string]string{},
		},
		grandchild: {role: 29, states: atspiTestStates(25, 30), properties: map[string]string{}},
	})
	limits := atspiTestLimits()
	limits.MaxDepth = 1
	snapshot, err := buildATSPITree(t.Context(), query, root, limits)
	if err != nil || len(snapshot.Nodes) != 2 || !snapshot.Truncated || query.childCalls[referenceKey(child)] != 0 {
		t.Fatalf("depth-bounded snapshot = %+v, err=%v, child calls=%v", snapshot, err, query.childCalls)
	}

	limits = atspiTestLimits()
	limits.MaxReferenceBytes = 2
	if _, err := buildATSPITree(t.Context(), query, root, limits); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("reference-bounded error = %v", err)
	}
}

func TestATSPIFixedRoleStateAndActionMappings(t *testing.T) {
	words := atspiTestStates(4, 5, 8, 11, 23, 33, 36)
	if got := mapATSPIRole(79); got != "textbox" {
		t.Fatalf("entry role = %q", got)
	}
	if mapATSPIRole(atspiRoleComboBox) != "combobox" || mapATSPIRole(atspiRoleSwitch) != "switch" {
		t.Fatal("common form-control roles lost their fixed mapping")
	}
	if got := fmt.Sprint(mapATSPIStates("checkbox", words)); got != "[enabled checked collapsed selected required invalid]" {
		t.Fatalf("states = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("checkbox", words, map[string]bool{
		"Action": true, "Component": true,
	}, []string{"toggle"})); got != "[toggle focus expand]" {
		t.Fatalf("actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("checkbox", words, map[string]bool{
		"Action": true,
	}, nil)); got != "[]" {
		t.Fatalf("zero-count actions = %s", got)
	}
}

func TestActATSPIRevalidatesExactObservedElementBeforeDispatch(t *testing.T) {
	application := atspiTestReference("application")
	window := atspiTestReference("window")
	button := atspiTestReference("button")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window: {
			parent: application, children: []atspiReference{button}, role: atspiRoleFrame,
			states:     atspiTestStates(atspiStateEnabled, atspiStateShowing, atspiStateVisible),
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		button: {
			parent: window, role: atspiRoleButton,
			states:      atspiTestStates(atspiStateEnabled, atspiStateFocusable, atspiStateShowing, atspiStateVisible),
			properties:  map[string]string{atspiPropertyName: "Save"},
			interfaces:  []string{atspiShortAction, atspiShortComponent},
			actionCount: 1, actionNames: []string{"click"},
			rect: atspiRect{X: 20, Y: 40, Width: 80, Height: 30},
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(button)), Action: "press",
		Expected: ElementExpectation{
			Role: "button", Name: "Save", States: []string{"enabled"},
			Bounds:  &Bounds{X: 20, Y: 40, Width: 80, Height: 30},
			Actions: []string{"press", "focus"},
		},
	}
	result, err := actATSPI(t.Context(), query, request, button)
	if err != nil || !result.Dispatched || len(query.mutationCalls) != 1 ||
		!strings.HasSuffix(query.mutationCalls[0], ":0") {
		t.Fatalf("semantic press = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.mutationCalls = nil
	query.objects[referenceKey(button)].properties[atspiPropertyName] = "Delete"
	result, err = actATSPI(t.Context(), query, request, button)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("stale semantic press = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
}

func TestActATSPIPreservesPostDispatchBoundaryAndSupportsEmptyText(t *testing.T) {
	application := atspiTestReference("application")
	window := atspiTestReference("window")
	textbox := atspiTestReference("textbox")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window: {
			parent: application, children: []atspiReference{textbox}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		textbox: {
			parent: window, role: atspiRoleEntry,
			states:     atspiTestStates(atspiStateEditable, atspiStateEnabled, atspiStateShowing, atspiStateVisible),
			properties: map[string]string{atspiPropertyName: "Notes"},
			interfaces: []string{atspiShortEditableText},
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(textbox)), Action: "set-value", Value: "",
		Expected: ElementExpectation{
			Role: "textbox", Name: "Notes", States: []string{"enabled"},
			Actions: []string{"set-value"},
		},
	}
	result, err := actATSPI(t.Context(), query, request, textbox)
	if err != nil || !result.Dispatched || query.setTextValue != "" || len(query.mutationCalls) != 1 {
		t.Fatalf("empty set-value = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.mutationErr = errors.New("private native failure")
	result, err = actATSPI(t.Context(), query, request, textbox)
	if !errors.Is(err, ErrUnavailable) || !result.Dispatched {
		t.Fatalf("post-dispatch failure = %+v, %v", result, err)
	}
}

func TestValidATSPIReferenceAcceptsOnlyUniqueNonNullObjects(t *testing.T) {
	valid := atspiTestReference("valid")
	if !validATSPIReference(valid) {
		t.Fatalf("valid reference rejected: %+v", valid)
	}
	for _, invalid := range []atspiReference{
		{Bus: "org.example.Application", Path: valid.Path},
		{Bus: ":1", Path: valid.Path},
		{Bus: ":1..2", Path: valid.Path},
		{Bus: ":1.2", Path: atspiNullPath},
		{Bus: ":1.2", Path: dbus.ObjectPath("not/absolute")},
	} {
		if validATSPIReference(invalid) {
			t.Fatalf("invalid reference accepted: %+v", invalid)
		}
	}
}

func TestNormalizeATSPIErrorUsesFixedErrorClasses(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "denied", err: dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"}, want: ErrPermissionDenied},
		{name: "unsupported", err: dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownMethod"}, want: ErrUnsupported},
		{name: "pointer-denied", err: &dbus.Error{Name: "org.freedesktop.DBus.Error.AuthFailed"}, want: ErrPermissionDenied},
		{name: "unavailable", err: dbus.Error{Name: "org.example.PrivateError", Body: []any{"sensitive details"}}, want: ErrUnavailable},
		{name: "cancelled", err: context.Canceled, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeATSPIError(test.err)
			if !errors.Is(got, test.want) || strings.Contains(got.Error(), "sensitive details") {
				t.Fatalf("normalized error = %v, want class %v", got, test.want)
			}
		})
	}
}
