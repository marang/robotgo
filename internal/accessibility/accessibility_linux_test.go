//go:build linux

package accessibility

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
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
	minimumValue     float64
	maximumValue     float64
	actionCount      int32
	actionNames      []string
	parent           atspiReference
}

type fakeATSPIQuery struct {
	apps             []atspiReference
	pids             map[string]uint32
	pidErrors        map[string]error
	objects          map[string]*fakeATSPIObject
	propertyCalls    map[string]int
	childCalls       map[string]int
	interfaceCalls   map[string]int
	textCalls        map[string]int
	textLimits       map[string][]int32
	mutationCalls    []string
	setTextValue     string
	setNumericValue  float64
	mutationErr      error
	minimumStepErr   error
	actionNameErr    error
	statesErr        error
	statesHook       func(atspiReference)
	actionNameHook   func()
	minimumStepHook  func()
	maximumValueHook func()
	propertyHook     func(atspiReference, string)
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
	if query.statesErr != nil {
		return nil, query.statesErr
	}
	object, err := query.object(reference)
	if err != nil {
		return nil, err
	}
	if query.statesHook != nil {
		query.statesHook(reference)
	}
	return append([]uint32(nil), object.states...), nil
}

func (query *fakeATSPIQuery) stringProperty(_ context.Context, reference atspiReference, name string) (string, error) {
	object, err := query.object(reference)
	if err != nil {
		return "", err
	}
	query.propertyCalls[referenceKey(reference)+":"+name]++
	if query.propertyHook != nil {
		query.propertyHook(reference, name)
	}
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

func (query *fakeATSPIQuery) text(_ context.Context, reference atspiReference, limit int32) (string, error) {
	object, err := query.object(reference)
	if err != nil {
		return "", err
	}
	query.textCalls[referenceKey(reference)]++
	query.textLimits[referenceKey(reference)] = append(query.textLimits[referenceKey(reference)], limit)
	characters := []rune(object.text)
	if limit >= 0 && len(characters) > int(limit) {
		characters = characters[:limit]
	}
	return string(characters), nil
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
	if query.actionNameErr != nil {
		return "", query.actionNameErr
	}
	object, err := query.object(reference)
	if err != nil || index < 0 || int(index) >= len(object.actionNames) {
		return "", ErrInvalidTree
	}
	name := object.actionNames[index]
	if query.actionNameHook != nil {
		query.actionNameHook()
	}
	return name, nil
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
	if query.minimumStepErr != nil {
		return 0, query.minimumStepErr
	}
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	if query.minimumStepHook != nil {
		query.minimumStepHook()
	}
	return object.minimumIncrement, nil
}

func (query *fakeATSPIQuery) minimumValue(_ context.Context, reference atspiReference) (float64, error) {
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	return object.minimumValue, nil
}

func (query *fakeATSPIQuery) maximumValue(_ context.Context, reference atspiReference) (float64, error) {
	object, err := query.object(reference)
	if err != nil {
		return 0, err
	}
	if query.maximumValueHook != nil {
		query.maximumValueHook()
	}
	return object.maximumValue, nil
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
		textLimits: make(map[string][]int32),
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

func TestBuildATSPITreeScopesMixedValidationAndProjectsReadOnly(t *testing.T) {
	root := atspiTestReference("mixed_root")
	control := atspiTestReference("mixed_control")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		root: {
			children: []atspiReference{control}, role: atspiRoleFrame,
			states:     atspiTestStates(atspiStateShowing, atspiStateVisible),
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		control: {
			role: atspiRoleSwitch,
			states: atspiTestStates(
				atspiStateEnabled, atspiStateFocusable, atspiStateIndeterminate,
				atspiStateShowing, atspiStateVisible,
			),
			properties:  map[string]string{atspiPropertyName: "Mode"},
			interfaces:  []string{atspiShortAction, atspiShortComponent},
			actionCount: 1, actionNames: []string{"toggle"},
		},
	})
	limits := atspiTestLimits()

	// A disallowed control remains structural and must not trigger private
	// property or interface reads merely because its native state is mixed.
	if snapshot, err := buildATSPITree(t.Context(), query, root, limits); err != nil || len(snapshot.Nodes) != 2 {
		t.Fatalf("disallowed mixed switch snapshot = %+v, %v", snapshot, err)
	}
	controlKey := referenceKey(control)
	if query.propertyCalls[controlKey+":"+atspiPropertyName] != 0 || query.interfaceCalls[controlKey] != 0 {
		t.Fatalf("disallowed mixed switch reads = properties %v, interfaces %v",
			query.propertyCalls, query.interfaceCalls)
	}

	limits.AllowedRoles["switch"] = true
	if _, err := buildATSPITree(t.Context(), query, root, limits); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("allowed mixed switch error = %v", err)
	}

	query.objects[controlKey].role = atspiRoleSlider
	query.objects[controlKey].states = atspiTestStates(
		atspiStateEnabled, atspiStateFocusable, atspiStateReadOnly,
		atspiStateShowing, atspiStateVisible,
	)
	query.objects[controlKey].interfaces = []string{atspiShortComponent, atspiShortValue}
	query.objects[controlKey].actionCount = 0
	query.objects[controlKey].actionNames = nil
	query.minimumStepErr = ErrUnavailable
	delete(limits.AllowedRoles, "switch")
	limits.AllowedRoles["slider"] = true
	snapshot, err := buildATSPITree(t.Context(), query, root, limits)
	if err != nil || len(snapshot.Nodes) != 2 ||
		fmt.Sprint(snapshot.Nodes[1].States) != "[enabled read-only]" ||
		fmt.Sprint(snapshot.Nodes[1].Actions) != "[focus]" {
		t.Fatalf("read-only slider snapshot = %+v, %v", snapshot, err)
	}
	request := ActionRequest{
		Reference: []byte(referenceKey(control)), Action: "focus",
		Expected: ElementExpectation{
			Role: "slider", Name: "Mode", States: []string{elementStateEnabled, elementStateReadOnly},
			Actions: []string{"focus"},
		},
		Postcondition: &ElementCondition{
			Kind: ElementConditionStateAbsent, State: elementStateReadOnly,
		},
	}
	if satisfied, err := checkATSPIElementCondition(t.Context(), query, control, request); err != nil || satisfied {
		t.Fatalf("read-only slider condition = %t, %v", satisfied, err)
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
	if got := fmt.Sprint(mapATSPIStates("radio", words)); got != "[enabled collapsed selected required invalid]" {
		t.Fatalf("radio states = %s", got)
	}
	mixed := atspiTestStates(atspiStateEnabled, atspiStateIndeterminate)
	for _, role := range []string{"checkbox", "radio"} {
		if err := validateATSPINativeStates(role, mixed); err != nil {
			t.Fatalf("mixed %s state = %v", role, err)
		}
		if got := fmt.Sprint(mapATSPIStates(role, mixed)); got != "[enabled]" {
			t.Fatalf("mixed %s states = %s", role, got)
		}
	}
	for _, test := range []struct {
		name string
		role string
		bits []uint32
	}{
		{name: "mixed switch", role: "switch", bits: []uint32{atspiStateIndeterminate}},
		{name: "mixed checked checkbox", role: "checkbox", bits: []uint32{atspiStateIndeterminate, atspiStateChecked}},
		{name: "mixed selected radio", role: "radio", bits: []uint32{atspiStateIndeterminate, atspiStateSelected}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateATSPINativeStates(test.role, atspiTestStates(test.bits...)); !errors.Is(err, ErrInvalidTree) {
				t.Fatalf("invalid mixed state = %v", err)
			}
		})
	}
	mixedSelectedCheckbox := atspiTestStates(
		atspiStateEnabled, atspiStateIndeterminate, atspiStateSelected,
	)
	if err := validateATSPINativeStates("checkbox", mixedSelectedCheckbox); err != nil {
		t.Fatalf("mixed selected checkbox state = %v", err)
	}
	if got := fmt.Sprint(mapATSPIStates("checkbox", mixedSelectedCheckbox)); got != "[enabled selected]" {
		t.Fatalf("mixed selected checkbox states = %s", got)
	}
	readOnly := atspiTestStates(atspiStateEnabled, atspiStateFocusable, atspiStateReadOnly)
	if got := fmt.Sprint(mapATSPIStates("slider", readOnly)); got != "[enabled read-only]" {
		t.Fatalf("read-only slider states = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("checkbox", words, map[string]bool{
		"Action": true, "Component": true,
	}, []string{"toggle"}, false)); got != "[toggle focus expand]" {
		t.Fatalf("actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("radio", words, map[string]bool{
		"Action": true, "Component": true,
	}, []string{"press"}, false)); got != "[press focus]" {
		t.Fatalf("radio actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("checkbox", words, map[string]bool{
		"Action": true,
	}, nil, false)); got != "[]" {
		t.Fatalf("zero-count actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("slider", words, map[string]bool{
		atspiShortValue: true,
	}, nil, false)); got != "[set-value]" {
		t.Fatalf("slider without step = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("slider", words, map[string]bool{
		atspiShortValue: true,
	}, nil, true)); got != "[set-value increment decrement]" {
		t.Fatalf("slider with step = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("textbox", atspiTestStates(atspiStateEnabled), map[string]bool{
		atspiShortEditableText: true,
	}, nil, false)); got != "[]" {
		t.Fatalf("read-only textbox actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("textbox", atspiTestStates(atspiStateEditable, atspiStateEnabled), map[string]bool{
		atspiShortEditableText: true,
	}, nil, false)); got != "[set-value]" {
		t.Fatalf("editable textbox actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("switch", readOnly, map[string]bool{
		atspiShortAction: true, atspiShortComponent: true,
	}, []string{"toggle"}, false)); got != "[focus]" {
		t.Fatalf("read-only switch actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("textbox", atspiTestStates(
		atspiStateEditable, atspiStateEnabled, atspiStateReadOnly,
	), map[string]bool{atspiShortEditableText: true}, nil, false)); got != "[]" {
		t.Fatalf("explicitly read-only textbox actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("slider", readOnly, map[string]bool{
		atspiShortValue: true, atspiShortComponent: true,
	}, nil, true)); got != "[focus]" {
		t.Fatalf("read-only slider actions = %s", got)
	}
	if got := fmt.Sprint(inferATSPIActions("combobox", atspiTestStates(
		atspiStateCollapsed, atspiStateEnabled, atspiStateFocusable, atspiStateReadOnly,
	), map[string]bool{atspiShortAction: true, atspiShortComponent: true}, []string{"expand"}, false)); got != "[focus expand]" {
		t.Fatalf("read-only combobox disclosure actions = %s", got)
	}
}

func TestUsableATSPIStepActionsRequirePositiveFiniteIncrement(t *testing.T) {
	slider := atspiTestReference("slider-step")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		slider: {minimumIncrement: 1},
	})
	interfaces := map[string]bool{atspiShortValue: true}

	for _, test := range []struct {
		name string
		step float64
		want bool
	}{
		{name: "positive", step: 1, want: true},
		{name: "zero"},
		{name: "nan", step: math.NaN()},
		{name: "infinite", step: math.Inf(1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			query.objects[referenceKey(slider)].minimumIncrement = test.step
			got, err := usableATSPIStepActions(t.Context(), query, slider, "slider", interfaces)
			if err != nil || got != test.want {
				t.Fatalf("usableATSPIStepActions() = %v, %v, want %v", got, err, test.want)
			}
		})
	}

	query.minimumStepErr = dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty"}
	if got, err := usableATSPIStepActions(t.Context(), query, slider, "slider", interfaces); err != nil || got {
		t.Fatalf("unsupported increment = %v, %v", got, err)
	}
	query.minimumStepErr = ErrUnavailable
	if _, err := usableATSPIStepActions(t.Context(), query, slider, "slider", interfaces); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable increment error = %v", err)
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
	dispatchBoundaries := 0
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(button)), Action: "press",
		Expected: ElementExpectation{
			Role: "button", Name: "Save", States: []string{"enabled"},
			Bounds:  &Bounds{X: 20, Y: 40, Width: 80, Height: 30},
			Actions: []string{"press", "focus"},
		},
		BeforeDispatch: func(context.Context) error { dispatchBoundaries++; return nil },
	}
	result, err := actATSPI(t.Context(), query, request, button)
	if err != nil || !result.Dispatched || len(query.mutationCalls) != 1 ||
		!strings.HasSuffix(query.mutationCalls[0], ":0") || dispatchBoundaries != 1 {
		t.Fatalf("semantic press = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.mutationCalls = nil
	query.objects[referenceKey(button)].properties[atspiPropertyName] = "Delete"
	result, err = actATSPI(t.Context(), query, request, button)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 || dispatchBoundaries != 1 {
		t.Fatalf("stale semantic press = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.objects[referenceKey(button)].properties[atspiPropertyName] = "Save"
	query.objects[referenceKey(button)].actionCount = 2
	query.objects[referenceKey(button)].actionNames = []string{"click", "delete"}
	windowTitleCalls := 0
	query.propertyHook = func(reference atspiReference, name string) {
		if reference == window && name == atspiPropertyName {
			windowTitleCalls++
		}
		if windowTitleCalls == 3 {
			query.objects[referenceKey(button)].actionNames = []string{"delete", "click"}
			query.propertyHook = nil
		}
	}
	result, err = actATSPI(t.Context(), query, request, button)
	if err != nil || !result.Dispatched || len(query.mutationCalls) != 1 ||
		!strings.HasSuffix(query.mutationCalls[0], ":1") || dispatchBoundaries != 2 {
		t.Fatalf("reordered semantic press = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.mutationCalls = nil
	query.objects[referenceKey(button)].actionNames = []string{"click", "delete"}
	windowTitleCalls = 0
	query.propertyHook = func(reference atspiReference, name string) {
		if reference == window && name == atspiPropertyName {
			windowTitleCalls++
		}
		if windowTitleCalls == 3 {
			query.actionNameHook = func() {
				query.objects[referenceKey(button)].actionNames = []string{"delete", "click"}
				query.actionNameHook = nil
			}
			query.propertyHook = nil
		}
	}
	result, err = actATSPI(t.Context(), query, request, button)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("hybrid action-name scan = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.objects[referenceKey(button)].actionNames = []string{"click", "delete"}
	windowTitleCalls = 0
	query.propertyHook = func(reference atspiReference, name string) {
		if reference == window && name == atspiPropertyName {
			windowTitleCalls++
		}
		if windowTitleCalls == 4 {
			query.actionNameErr = context.DeadlineExceeded
			query.propertyHook = nil
		}
	}
	result, err = actATSPI(t.Context(), query, request, button)
	if !errors.Is(err, context.DeadlineExceeded) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("final action-name timeout = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
	query.actionNameErr = nil

	query.actionNameHook = func() {
		query.objects[referenceKey(window)].properties[atspiPropertyName] = "Replacement"
		query.actionNameHook = nil
	}
	result, err = actATSPI(t.Context(), query, request, button)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("late stale window title = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
}

func TestATSPIConditionGateIsIdempotentAndFailsClosed(t *testing.T) {
	application := atspiTestReference("application")
	window := atspiTestReference("window")
	checkbox := atspiTestReference("checkbox")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window: {
			parent: application, children: []atspiReference{checkbox}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		checkbox: {
			parent: window, role: atspiRoleCheckBox,
			states: atspiTestStates(
				atspiStateChecked, atspiStateEnabled, atspiStateFocusable,
				atspiStateShowing, atspiStateVisible,
			),
			properties:  map[string]string{atspiPropertyName: "Remember"},
			interfaces:  []string{atspiShortAction, atspiShortComponent},
			actionCount: 1, actionNames: []string{"click"},
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(checkbox)), Action: "toggle",
		Expected: ElementExpectation{
			Role: "checkbox", Name: "Remember", States: []string{elementStateEnabled},
			Actions: []string{"toggle", "focus"},
		},
		Postcondition: &ElementCondition{
			Kind: ElementConditionStatePresent, State: elementStateChecked,
		},
	}

	result, err := actATSPI(t.Context(), query, request, checkbox)
	if err != nil || !result.AlreadySatisfied || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("already-satisfied toggle = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
	checked, err := checkATSPI(t.Context(), query, request, checkbox)
	if err != nil || !checked.Satisfied || len(query.mutationCalls) != 0 {
		t.Fatalf("read-only checked probe = %+v, %v, calls=%v", checked, err, query.mutationCalls)
	}

	query.objects[referenceKey(checkbox)].states = atspiTestStates(
		atspiStateEnabled, atspiStateFocusable, atspiStateShowing, atspiStateVisible,
	)
	result, err = actATSPI(t.Context(), query, request, checkbox)
	if err != nil || !result.Dispatched || result.AlreadySatisfied || len(query.mutationCalls) != 1 {
		t.Fatalf("unsatisfied toggle = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.mutationCalls = nil
	query.objects[referenceKey(checkbox)].states = atspiTestStates(
		atspiStateChecked, atspiStateEnabled, atspiStateFocusable,
		atspiStateSelected, atspiStateShowing, atspiStateVisible,
	)
	result, err = actATSPI(t.Context(), query, request, checkbox)
	if !errors.Is(err, ErrStaleTarget) || result.AlreadySatisfied || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("unrelated state drift = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.statesErr = context.DeadlineExceeded
	result, err = actATSPI(t.Context(), query, request, checkbox)
	if !errors.Is(err, context.DeadlineExceeded) || result.AlreadySatisfied || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("failed condition read = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
}

func TestATSPIConditionRejectsMixedSwitchWithoutDispatch(t *testing.T) {
	application := atspiTestReference("mixed_application")
	window := atspiTestReference("mixed_window")
	control := atspiTestReference("mixed_switch")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window: {
			parent: application, children: []atspiReference{control}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		control: {
			parent: window, role: atspiRoleSwitch,
			states: atspiTestStates(
				atspiStateEnabled, atspiStateFocusable, atspiStateIndeterminate,
				atspiStateShowing, atspiStateVisible,
			),
			properties:  map[string]string{atspiPropertyName: "Remember"},
			interfaces:  []string{atspiShortAction, atspiShortComponent},
			actionCount: 1, actionNames: []string{"toggle"},
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(control)), Action: "toggle",
		Expected: ElementExpectation{
			Role: "switch", Name: "Remember", States: []string{elementStateEnabled},
			Actions: []string{"toggle", "focus"},
		},
		Postcondition: &ElementCondition{
			Kind: ElementConditionStateAbsent, State: elementStateChecked,
		},
	}

	result, err := actATSPI(t.Context(), query, request, control)
	if !errors.Is(err, ErrInvalidTree) || result.AlreadySatisfied || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("mixed switch action = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	// Mixed checkboxes remain intentionally observable without claiming the
	// canonical checked state, so v1 state-absent may be satisfied read-only.
	query.objects[referenceKey(control)].role = atspiRoleCheckBox
	request.Expected.Role = "checkbox"
	result, err = actATSPI(t.Context(), query, request, control)
	if err != nil || !result.AlreadySatisfied || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("mixed checkbox action = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
}

func TestATSPIReadOnlyDriftSuppressesValueMutation(t *testing.T) {
	application := atspiTestReference("readonly_application")
	window := atspiTestReference("readonly_window")
	slider := atspiTestReference("readonly_slider")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window: {
			parent: application, children: []atspiReference{slider}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		slider: {
			parent: window, role: atspiRoleSlider,
			states: atspiTestStates(
				atspiStateEnabled, atspiStateFocusable, atspiStateShowing, atspiStateVisible,
			),
			properties: map[string]string{atspiPropertyName: "Volume"},
			interfaces: []string{atspiShortComponent, atspiShortValue},
			value:      0, minimumIncrement: 1,
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(slider)), Action: "set-value", Value: []byte("1"),
		Expected: ElementExpectation{
			Role: "slider", Name: "Volume", States: []string{elementStateEnabled},
			Actions: []string{"focus", "set-value", "increment", "decrement"},
		},
		Postcondition: &ElementCondition{Kind: ElementConditionValueEqualsActionValue},
	}

	windowTitleCalls := 0
	query.propertyHook = func(reference atspiReference, name string) {
		if reference != window || name != atspiPropertyName {
			return
		}
		windowTitleCalls++
		if windowTitleCalls == 2 {
			query.objects[referenceKey(slider)].states = atspiTestStates(
				atspiStateEnabled, atspiStateFocusable, atspiStateReadOnly,
				atspiStateShowing, atspiStateVisible,
			)
			query.propertyHook = nil
		}
	}

	result, err := actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrStaleTarget) || result.AlreadySatisfied || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("read-only slider drift = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
}

func TestATSPIConditionAcceptsExpansionActionInversion(t *testing.T) {
	combobox := atspiTestReference("combobox")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		combobox: {
			role: atspiRoleComboBox,
			states: atspiTestStates(
				atspiStateEnabled, atspiStateExpanded, atspiStateShowing, atspiStateVisible,
			),
			properties:  map[string]string{atspiPropertyName: "Options"},
			interfaces:  []string{atspiShortAction},
			actionCount: 1,
			actionNames: []string{"collapse"},
		},
	})
	request := ActionRequest{
		Reference: []byte(referenceKey(combobox)),
		Action:    "expand",
		Expected: ElementExpectation{
			Role: "combobox", Name: "Options",
			States:  []string{elementStateEnabled, elementStateCollapsed},
			Actions: []string{"expand"},
		},
		Postcondition: &ElementCondition{
			Kind: ElementConditionStatePresent, State: elementStateExpanded,
		},
	}

	satisfied, err := checkATSPIElementCondition(t.Context(), query, combobox, request)
	if err != nil || !satisfied || len(query.mutationCalls) != 0 {
		t.Fatalf("expanded condition = %t, %v, calls=%v", satisfied, err, query.mutationCalls)
	}
}

func TestAccessibilityEarlyErrorsReportCompleteCleanup(t *testing.T) {
	t.Parallel()
	action, err := Act(t.Context(), ActionRequest{})
	if !errors.Is(err, ErrStaleTarget) || action.Dispatched || action.AlreadySatisfied || !action.CleanupComplete {
		t.Fatalf("early action error = %+v, %v", action, err)
	}
	condition, err := Check(t.Context(), ActionRequest{})
	if !errors.Is(err, ErrStaleTarget) || condition.Satisfied || !condition.CleanupComplete {
		t.Fatalf("early condition error = %+v, %v", condition, err)
	}
}

func TestCheckATSPIValueConditionDoesNotMutateOrConsumeActionValue(t *testing.T) {
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
			states: atspiTestStates(
				atspiStateEditable, atspiStateEnabled, atspiStateShowing, atspiStateVisible,
			),
			properties: map[string]string{atspiPropertyName: "Notes"},
			interfaces: []string{atspiShortEditableText, atspiShortText},
			text:       "private value",
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	value := []byte("private value")
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(textbox)), Action: "set-value", Value: value,
		Expected: ElementExpectation{
			Role: "textbox", Name: "Notes", States: []string{elementStateEnabled},
			Actions: []string{"set-value"},
		},
		Postcondition: &ElementCondition{Kind: ElementConditionValueEqualsActionValue},
	}

	result, err := checkATSPI(t.Context(), query, request, textbox)
	if err != nil || !result.Satisfied || len(query.mutationCalls) != 0 ||
		!slices.Equal(value, []byte("private value")) {
		t.Fatalf("value condition = %+v, %v, calls=%v, value=%q", result, err, query.mutationCalls, value)
	}

	boundary := strings.Repeat("x", maxElementConditionValueBytes)
	request.Value = []byte(boundary)
	query.objects[referenceKey(textbox)].text = boundary
	result, err = checkATSPI(t.Context(), query, request, textbox)
	if err != nil || !result.Satisfied {
		t.Fatalf("exact boundary value condition = %+v, %v", result, err)
	}
	query.objects[referenceKey(textbox)].text = boundary + "x"
	result, err = checkATSPI(t.Context(), query, request, textbox)
	if err != nil || result.Satisfied {
		t.Fatalf("truncated-prefix value condition = %+v, %v", result, err)
	}
	limits := query.textLimits[referenceKey(textbox)]
	if len(limits) == 0 || limits[len(limits)-1] != maxElementConditionValueBytes+1 {
		t.Fatalf("value condition text limits = %v, want final limit %d", limits, maxElementConditionValueBytes+1)
	}

	request.Value = []byte(strings.Repeat("x", maxElementConditionValueBytes+1))
	textCalls := query.textCalls[referenceKey(textbox)]
	result, err = checkATSPI(t.Context(), query, request, textbox)
	if !errors.Is(err, ErrInvalidTree) || result.Satisfied ||
		query.textCalls[referenceKey(textbox)] != textCalls {
		t.Fatalf("oversized condition = %+v, %v, text calls=%d, want %d",
			result, err, query.textCalls[referenceKey(textbox)], textCalls)
	}
}

func TestFakeATSPITextLimitUsesCharacterOffsets(t *testing.T) {
	textbox := atspiTestReference("textbox")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		textbox: {text: "éx"},
	})
	got, err := query.text(t.Context(), textbox, 1)
	if err != nil || got != "é" {
		t.Fatalf("one-character AT-SPI text = %q, %v; want é", got, err)
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
		Reference: []byte(referenceKey(textbox)), Action: "set-value",
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

func TestActATSPIRevalidatesWindowAfterSliderPreparation(t *testing.T) {
	application := atspiTestReference("application")
	window := atspiTestReference("window")
	slider := atspiTestReference("slider")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window: {
			parent: application, children: []atspiReference{slider}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		slider: {
			parent: window, role: atspiRoleSlider,
			states:           atspiTestStates(atspiStateEnabled, atspiStateShowing, atspiStateVisible),
			properties:       map[string]string{atspiPropertyName: "Volume"},
			interfaces:       []string{atspiShortValue},
			value:            4,
			minimumIncrement: 1,
			minimumValue:     0,
			maximumValue:     10,
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	query.minimumStepHook = func() {
		query.objects[referenceKey(window)].properties[atspiPropertyName] = "Replacement"
		query.minimumStepHook = nil
	}
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(slider)), Action: "increment",
		Expected: ElementExpectation{
			Role: "slider", Name: "Volume", States: []string{"enabled"},
			Actions: []string{"set-value", "increment", "decrement"},
		},
	}

	result, err := actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("late stale slider window = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
}

func TestActATSPIRevalidatesMembershipAfterSliderPreparation(t *testing.T) {
	application := atspiTestReference("application")
	window := atspiTestReference("window")
	replacement := atspiTestReference("replacement")
	slider := atspiTestReference("slider")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window, replacement}},
		window: {
			parent: application, children: []atspiReference{slider}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		replacement: {
			parent: application, children: []atspiReference{slider}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Other"},
		},
		slider: {
			parent: window, role: atspiRoleSlider,
			states:           atspiTestStates(atspiStateEnabled, atspiStateShowing, atspiStateVisible),
			properties:       map[string]string{atspiPropertyName: "Volume"},
			interfaces:       []string{atspiShortValue},
			value:            4,
			minimumIncrement: 1,
			minimumValue:     0,
			maximumValue:     10,
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	query.minimumStepHook = func() {
		query.objects[referenceKey(slider)].parent = replacement
		query.minimumStepHook = nil
	}
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(slider)), Action: "increment",
		Expected: ElementExpectation{
			Role: "slider", Name: "Volume", States: []string{"enabled"},
			Actions: []string{"set-value", "increment", "decrement"},
		},
	}

	result, err := actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("late reparented slider = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}
}

func TestActATSPIRevalidatesSliderSemanticsAfterRangePreparation(t *testing.T) {
	application := atspiTestReference("application")
	window := atspiTestReference("window")
	slider := atspiTestReference("slider")
	query := newFakeATSPIQuery(map[atspiReference]*fakeATSPIObject{
		application: {children: []atspiReference{window}},
		window: {
			parent: application, children: []atspiReference{slider}, role: atspiRoleFrame,
			properties: map[string]string{atspiPropertyName: "Fixture"},
		},
		slider: {
			parent: window, role: atspiRoleSlider,
			states:           atspiTestStates(atspiStateEnabled, atspiStateShowing, atspiStateVisible),
			properties:       map[string]string{atspiPropertyName: "Volume"},
			interfaces:       []string{atspiShortValue},
			value:            4,
			minimumIncrement: 1,
			minimumValue:     0,
			maximumValue:     10,
		},
	})
	query.apps = []atspiReference{application}
	query.pids[application.Bus] = 42
	query.maximumValueHook = func() {
		query.objects[referenceKey(slider)].properties[atspiPropertyName] = "Balance"
		query.maximumValueHook = nil
	}
	request := ActionRequest{
		Target:    Target{ProcessID: 42, ExpectedTitle: "Fixture"},
		Reference: []byte(referenceKey(slider)), Action: "increment",
		Expected: ElementExpectation{
			Role: "slider", Name: "Volume", States: []string{"enabled"},
			Actions: []string{"set-value", "increment", "decrement"},
		},
	}

	result, err := actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("late stale slider semantics = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.objects[referenceKey(slider)].properties[atspiPropertyName] = "Volume"
	request.Action = "set-value"
	request.Value = []byte("11")
	result, err = actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrInvalidTree) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("out-of-range slider value = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	request.Value = []byte("8")
	minimumStepCalls := 0
	query.minimumStepHook = func() {
		minimumStepCalls++
		if minimumStepCalls == 2 {
			query.objects[referenceKey(slider)].maximumValue = 5
			query.minimumStepHook = nil
		}
	}
	result, err = actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrInvalidTree) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("late out-of-range slider value = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.objects[referenceKey(slider)].maximumValue = 10
	maximumValueCalls := 0
	query.maximumValueHook = func() {
		maximumValueCalls++
		if maximumValueCalls == 2 {
			query.objects[referenceKey(window)].properties[atspiPropertyName] = "Replacement"
			query.maximumValueHook = nil
		}
	}
	result, err = actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("late stale slider window after range read = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.objects[referenceKey(window)].properties[atspiPropertyName] = "Fixture"
	maximumValueCalls = 0
	query.maximumValueHook = func() {
		maximumValueCalls++
		if maximumValueCalls == 2 {
			query.objects[referenceKey(slider)].properties[atspiPropertyName] = "Balance"
			query.maximumValueHook = nil
		}
	}
	result, err = actATSPI(t.Context(), query, request, slider)
	if !errors.Is(err, ErrStaleTarget) || result.Dispatched || len(query.mutationCalls) != 0 {
		t.Fatalf("late stale slider semantics after range read = %+v, %v, calls=%v", result, err, query.mutationCalls)
	}

	query.objects[referenceKey(slider)].properties[atspiPropertyName] = "Volume"
	request.Action = "increment"
	request.Value = nil
	minimumStepCalls = 0
	query.minimumStepHook = func() {
		minimumStepCalls++
		if minimumStepCalls == 3 {
			query.objects[referenceKey(slider)].value = 9
			query.minimumStepHook = nil
		}
	}
	result, err = actATSPI(t.Context(), query, request, slider)
	if err != nil || !result.Dispatched || query.setNumericValue != 10 || len(query.mutationCalls) != 1 {
		t.Fatalf("recomputed slider step = %+v, %v, value=%v, calls=%v", result, err, query.setNumericValue, query.mutationCalls)
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
		{name: "unknown-property", err: dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty"}, want: ErrUnsupported},
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
