//go:build windows

package accessibility

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

const (
	uiaNativeCallTimeoutMillis = uint32(1000)

	uiaMethodElementFromHandle   = 6
	uiaMethodRawViewWalker       = 16
	uiaMethodPutAutoSetFocus     = 59
	uiaMethodPutConnectionLimit  = 61
	uiaMethodPutTransactionLimit = 63

	uiaElementMethodRuntimeID         = 4
	uiaElementMethodSetFocus          = 3
	uiaElementMethodCurrentPattern    = 16
	uiaElementMethodPropertyValue     = 10
	uiaElementMethodProcessID         = 20
	uiaElementMethodControlType       = 21
	uiaElementMethodName              = 23
	uiaElementMethodKeyboardFocus     = 26
	uiaElementMethodKeyboardFocusable = 27
	uiaElementMethodEnabled           = 28
	uiaElementMethodHelpText          = 31
	uiaElementMethodPassword          = 35
	uiaElementMethodOffscreen         = 38
	uiaElementMethodRequired          = 41
	uiaElementMethodBounds            = 43

	uiaWalkerMethodFirstChild  = 4
	uiaWalkerMethodNextSibling = 6

	uiaPatternInvoke         int32 = 10000
	uiaPatternValue          int32 = 10002
	uiaPatternRangeValue     int32 = 10003
	uiaPatternExpandCollapse int32 = 10005
	uiaPatternSelectionItem  int32 = 10010
	uiaPatternToggle         int32 = 10015

	uiaPatternMethodPrimary   = 3
	uiaPatternMethodSecondary = 4
	uiaRangeMethodCurrent     = 4
	uiaRangeMethodMaximum     = 6
	uiaRangeMethodMinimum     = 7
	uiaRangeMethodSmallChange = 9
)

func uiaRoleUsesInvoke(role string) bool {
	switch role {
	case "button", "link", "menu-item", "tab":
		return true
	default:
		return false
	}
}

const (
	uiaPropertyExpandCollapseAvailable int32 = 30028
	uiaPropertyInvokeAvailable         int32 = 30031
	uiaPropertyRangeValueAvailable     int32 = 30033
	uiaPropertySelectionItemAvailable  int32 = 30036
	uiaPropertyToggleAvailable         int32 = 30041
	uiaPropertyValueAvailable          int32 = 30043
	uiaPropertyValueValue              int32 = 30045
	uiaPropertyValueReadOnly           int32 = 30046
	uiaPropertyRangeValueValue         int32 = 30047
	uiaPropertyRangeValueReadOnly      int32 = 30048
	uiaPropertyRangeValueSmallChange   int32 = 30052
	uiaPropertyExpandCollapseState     int32 = 30070
	uiaPropertySelectionItemSelected   int32 = 30079
	uiaPropertyToggleState             int32 = 30086
)

const (
	hResultAccessDenied       uint32 = 0x80070005
	hResultClassNotRegistered uint32 = 0x80040154
	hResultNoInterface        uint32 = 0x80004002
	hResultCallCanceled       uint32 = 0x80010002
	hResultUIAElementNotFound uint32 = 0x80040201
	hResultUIANotSupported    uint32 = 0x80040204
	hResultUIATimeout         uint32 = 0x80131505
)

var (
	clsidCUIAutomation8 = ole.NewGUID("{E22AD333-B25F-460C-83D0-0581107395C9}")
	iidIUIAutomation2   = ole.NewGUID("{34723AFF-0C9D-49D0-9896-7AB52DF8CD8A}")

	ole32DLL             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32DLL.NewProc("CoCreateInstance")
	oleAut32DLL          = windows.NewLazySystemDLL("oleaut32.dll")
	procSafeArrayDestroy = oleAut32DLL.NewProc("SafeArrayDestroy")
	procSafeArrayGetDim  = oleAut32DLL.NewProc("SafeArrayGetDim")
	procSafeArrayGetSize = oleAut32DLL.NewProc("SafeArrayGetElemsize")
	procSafeArrayGetLow  = oleAut32DLL.NewProc("SafeArrayGetLBound")
	procSafeArrayGetHigh = oleAut32DLL.NewProc("SafeArrayGetUBound")
	procSafeArrayGetItem = oleAut32DLL.NewProc("SafeArrayGetElement")
)

type uiaClient struct {
	automation *ole.IUnknown
	query      *uiaCOMQuery
}

type uiaCOMQuery struct {
	walker *ole.IUnknown
}

type uiaCallError uint32

func (err uiaCallError) Error() string { return "Windows UI Automation call failed" }

func initializeUIAThread() error {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oleErr *ole.OleError
		if errors.As(err, &oleErr) && oleErr.Code() == 1 {
			return nil
		}
		return normalizeUIAError(context.Background(), err)
	}
	return nil
}

func uninitializeUIAThread() { ole.CoUninitialize() }

func newUIAClient(ctx context.Context) (*uiaClient, error) {
	automation, err := createUIAutomation2(ctx)
	if err != nil {
		return nil, err
	}
	client := &uiaClient{automation: automation}
	if err := callUIAMethod(ctx, automation, uiaMethodPutAutoSetFocus, 0); err != nil {
		client.close()
		return nil, err
	}
	if err := callUIAMethod(ctx, automation, uiaMethodPutConnectionLimit, uintptr(uiaNativeCallTimeoutMillis)); err != nil {
		client.close()
		return nil, err
	}
	if err := callUIAMethod(ctx, automation, uiaMethodPutTransactionLimit, uintptr(uiaNativeCallTimeoutMillis)); err != nil {
		client.close()
		return nil, err
	}
	var walker *ole.IUnknown
	if err := callUIAMethod(ctx, automation, uiaMethodRawViewWalker, uintptr(unsafe.Pointer(&walker))); err != nil {
		if walker != nil {
			walker.Release()
		}
		client.close()
		return nil, err
	}
	if walker == nil {
		client.close()
		return nil, ErrUnavailable
	}
	client.query = &uiaCOMQuery{walker: walker}
	return client, nil
}

func (client *uiaClient) close() {
	if client == nil {
		return
	}
	if client.query != nil && client.query.walker != nil {
		client.query.walker.Release()
		client.query.walker = nil
	}
	if client.automation != nil {
		client.automation.Release()
		client.automation = nil
	}
}

func (client *uiaClient) elementFromHandle(ctx context.Context, handle uintptr) (*ole.IUnknown, error) {
	if handle == 0 {
		return nil, ErrStaleTarget
	}
	var element *ole.IUnknown
	if err := callUIAMethod(ctx, client.automation, uiaMethodElementFromHandle,
		handle, uintptr(unsafe.Pointer(&element))); err != nil {
		if element != nil {
			element.Release()
		}
		return nil, err
	}
	if element == nil {
		return nil, ErrStaleTarget
	}
	return element, nil
}

func createUIAutomation2(ctx context.Context) (*ole.IUnknown, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var automation *ole.IUnknown
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsidCUIAutomation8)), 0, uintptr(ole.CLSCTX_INPROC_SERVER),
		uintptr(unsafe.Pointer(iidIUIAutomation2)), uintptr(unsafe.Pointer(&automation)),
	)
	if failedHRESULT(hr) {
		if automation != nil {
			automation.Release()
		}
		return nil, normalizeUIAError(ctx, uiaCallError(uint32(hr)))
	}
	if automation == nil {
		return nil, ErrUnavailable
	}
	return automation, nil
}

func (query *uiaCOMQuery) structure(ctx context.Context, element *ole.IUnknown) (uiaNodeStructure, error) {
	runtimeID, err := query.runtimeID(ctx, element)
	if err != nil {
		return uiaNodeStructure{}, fmt.Errorf("runtime ID: %w", err)
	}
	controlType, err := elementInt32(ctx, element, uiaElementMethodControlType)
	if err != nil {
		return uiaNodeStructure{}, fmt.Errorf("control type: %w", err)
	}
	password, err := elementBool(ctx, element, uiaElementMethodPassword)
	if err != nil {
		return uiaNodeStructure{}, fmt.Errorf("password state: %w", err)
	}
	toggleAvailable := false
	if controlType == uiaControlButton && !password {
		// Classify the semantic role from the actual provider capability. This
		// also keeps structural traversal independent of optional property
		// VARIANT decoding and matches the pattern used during dispatch.
		toggleAvailable, err = currentUIAPatternAvailable(ctx, element, uiaPatternToggle)
		if err != nil {
			return uiaNodeStructure{}, fmt.Errorf("toggle pattern: %w", err)
		}
	}
	offscreen, err := elementBool(ctx, element, uiaElementMethodOffscreen)
	if err != nil {
		return uiaNodeStructure{}, fmt.Errorf("offscreen state: %w", err)
	}
	return uiaNodeStructure{
		RuntimeID: runtimeID, ControlType: controlType,
		Password: password, Offscreen: offscreen, ToggleAvailable: toggleAvailable,
	}, nil
}

func (query *uiaCOMQuery) processID(ctx context.Context, element *ole.IUnknown) (int32, error) {
	return elementInt32(ctx, element, uiaElementMethodProcessID)
}

func (query *uiaCOMQuery) details(
	ctx context.Context,
	element *ole.IUnknown,
	role string,
	limits Limits,
) (uiaNodeDetails, error) {
	return query.detailsWithDisabledActions(ctx, element, role, limits, false)
}

func (query *uiaCOMQuery) detailsWithDisabledActions(
	ctx context.Context,
	element *ole.IUnknown,
	role string,
	limits Limits,
	readActionsWhenDisabled bool,
) (uiaNodeDetails, error) {
	result := uiaNodeDetails{}
	var err error
	if limits.ReadName {
		result.Name, result.NameTruncated, err = elementBSTRWithTruncation(ctx, element, uiaElementMethodName, limits.MaxStringBytes)
		if err != nil {
			return result, fmt.Errorf("name: %w", err)
		}
	}
	if limits.ReadDescription {
		result.Description, err = elementBSTR(ctx, element, uiaElementMethodHelpText, limits.MaxStringBytes)
		if err != nil {
			return result, fmt.Errorf("description: %w", err)
		}
	}
	if limits.ReadFocus {
		result.Focused, err = elementBool(ctx, element, uiaElementMethodKeyboardFocus)
		if err != nil {
			return result, fmt.Errorf("focus: %w", err)
		}
		result.FocusObservable = true
	}
	if limits.ReadBounds {
		result.Bounds, err = elementBounds(ctx, element)
		if err != nil {
			return result, fmt.Errorf("bounds: %w", err)
		}
	}
	properties := newUIAPropertyReader(ctx, element, limits.MaxStringBytes)
	if limits.ReadStates {
		result.States, err = query.states(ctx, element, role, properties)
		if err != nil {
			return result, fmt.Errorf("states: %w", err)
		}
		// Strict provider-presence reads belong only to the condition gate.
		// Normal inspection keeps optional platform properties optional.
		if readActionsWhenDisabled {
			result.ObservableStates, err = query.observableStates(ctx, element, role, properties)
			if err != nil {
				return result, fmt.Errorf("observable states: %w", err)
			}
		}
	}
	if limits.ReadActions {
		result.Actions, err = query.actions(ctx, element, role, properties, readActionsWhenDisabled)
		if err != nil {
			return result, fmt.Errorf("actions: %w", err)
		}
	}
	if limits.ReadValue {
		result.Value, result.ValueObservable, err = query.value(role, properties)
		if err != nil {
			return result, fmt.Errorf("value: %w", err)
		}
	}
	return result, nil
}

func (query *uiaCOMQuery) states(
	ctx context.Context,
	element *ole.IUnknown,
	role string,
	properties *uiaPropertyReader,
) ([]string, error) {
	enabled, err := elementBool(ctx, element, uiaElementMethodEnabled)
	if err != nil {
		return nil, err
	}
	states := make([]string, 0, 6)
	if enabled {
		states = append(states, "enabled")
	} else if uiaInteractiveRole(role) {
		states = append(states, "disabled")
	}
	required, err := elementBool(ctx, element, uiaElementMethodRequired)
	if err != nil {
		return nil, err
	}
	if required {
		states = append(states, "required")
	}
	if uiaRoleUsesToggle(role) {
		if available, err := properties.bool(uiaPropertyToggleAvailable); err != nil {
			return nil, err
		} else if available {
			state, supported, err := properties.integer(uiaPropertyToggleState)
			if err != nil {
				return nil, err
			}
			checked, err := uiaToggleChecked(state, supported)
			if err != nil {
				return nil, err
			}
			if checked {
				states = append(states, "checked")
			}
		}
	}
	if uiaRoleUsesSelectionItem(role) {
		if available, err := properties.bool(uiaPropertySelectionItemAvailable); err != nil {
			return nil, err
		} else if available {
			selected, err := properties.bool(uiaPropertySelectionItemSelected)
			if err != nil {
				return nil, err
			}
			if state := uiaSelectionItemState(role, selected); state != "" {
				states = append(states, state)
			}
		}
	}
	if role == "combobox" || role == "list-item" || role == "menu-item" {
		if available, err := properties.bool(uiaPropertyExpandCollapseAvailable); err != nil {
			return nil, err
		} else if available {
			state, supported, err := properties.integer(uiaPropertyExpandCollapseState)
			if err != nil {
				return nil, err
			}
			if supported && state == 0 {
				states = append(states, "collapsed")
			} else if supported && state == 1 {
				states = append(states, "expanded")
			}
		}
	}
	readOnly, err := properties.readOnly(role)
	if err != nil {
		return nil, err
	}
	if readOnly {
		states = append(states, "read-only")
	}
	return uniqueUIAStrings(states), nil
}

func (query *uiaCOMQuery) observableStates(
	ctx context.Context,
	element *ole.IUnknown,
	role string,
	properties *uiaPropertyReader,
) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	states := []string{elementStateEnabled, elementStateDisabled, elementStateRequired}
	if uiaRoleUsesToggle(role) {
		available, err := properties.bool(uiaPropertyToggleAvailable)
		if err != nil {
			return nil, err
		}
		if available {
			state, supported, err := properties.integer(uiaPropertyToggleState)
			if err != nil {
				return nil, err
			}
			if _, err := uiaToggleChecked(state, supported); err != nil {
				return nil, err
			}
			states = append(states, elementStateChecked)
		}
	}
	if uiaRoleUsesSelectionItem(role) {
		available, err := properties.bool(uiaPropertySelectionItemAvailable)
		if err != nil {
			return nil, err
		}
		if available {
			if _, err := properties.requiredBool(uiaPropertySelectionItemSelected); err != nil {
				return nil, err
			}
			states = append(states, elementStateSelected)
		}
	}
	if role == "combobox" || role == "list-item" || role == "menu-item" {
		available, err := properties.bool(uiaPropertyExpandCollapseAvailable)
		if err != nil {
			return nil, err
		}
		if available {
			state, supported, err := properties.integer(uiaPropertyExpandCollapseState)
			if err != nil {
				return nil, err
			}
			if supported && (state == 0 || state == 1) {
				states = append(states, elementStateExpanded, elementStateCollapsed)
			}
		}
	}
	_, readOnlyObservable, err := properties.readOnlyState(role)
	if err != nil {
		return nil, err
	}
	if readOnlyObservable {
		states = append(states, elementStateReadOnly)
	}
	return uniqueUIAStrings(states), nil
}

func (query *uiaCOMQuery) actions(
	ctx context.Context,
	element *ole.IUnknown,
	role string,
	properties *uiaPropertyReader,
	readWhenDisabled bool,
) ([]string, error) {
	enabled, err := elementBool(ctx, element, uiaElementMethodEnabled)
	if err != nil || !enabled && !readWhenDisabled {
		return nil, err
	}
	// Condition reads retain provider capabilities while disabled so action
	// identity remains stable across an enabled -> disabled transition. Normal
	// inspection still publishes only currently offered actions.
	actions := make([]string, 0, 6)
	focusable, err := elementBool(ctx, element, uiaElementMethodKeyboardFocusable)
	if err != nil {
		return nil, err
	}
	if focusable {
		actions = append(actions, "focus")
	}
	if uiaRoleUsesInvoke(role) {
		invoke, err := properties.bool(uiaPropertyInvokeAvailable)
		if err != nil {
			return nil, err
		}
		if invoke {
			actions = append(actions, "press")
		}
	}
	toggle := false
	if uiaRoleUsesToggle(role) {
		toggle, err = properties.bool(uiaPropertyToggleAvailable)
		if err != nil {
			return nil, err
		}
		if toggle {
			state, supported, err := properties.integer(uiaPropertyToggleState)
			if err != nil {
				return nil, err
			}
			if _, err := uiaToggleChecked(state, supported); err != nil {
				return nil, err
			}
		}
	}
	selection := false
	if uiaRoleUsesSelectionItem(role) {
		selection, err = properties.bool(uiaPropertySelectionItemAvailable)
		if err != nil {
			return nil, err
		}
	}
	if action := uiaPatternAction(role, toggle, selection); action != "" {
		actions = append(actions, action)
	}
	if role == "combobox" || role == "list-item" || role == "menu-item" {
		expandable, err := properties.bool(uiaPropertyExpandCollapseAvailable)
		if err != nil {
			return nil, err
		}
		if expandable {
			state, supported, err := properties.integer(uiaPropertyExpandCollapseState)
			if err != nil {
				return nil, err
			}
			if supported && state == 0 {
				actions = append(actions, "expand")
			} else if supported && state == 1 {
				actions = append(actions, "collapse")
			}
		}
	}
	valueAvailable := false
	if role == "textbox" || role == "combobox" {
		valueAvailable, err = properties.bool(uiaPropertyValueAvailable)
		if err != nil {
			return nil, err
		}
	}
	rangeAvailable := false
	if role == "slider" {
		rangeAvailable, err = properties.bool(uiaPropertyRangeValueAvailable)
		if err != nil {
			return nil, err
		}
	}
	readOnly, err := properties.readOnly(role)
	if err != nil {
		return nil, err
	}
	if !readOnly && valueAvailable {
		actions = append(actions, "set-value")
	}
	if !readOnly && rangeAvailable {
		step, supported, err := properties.number(uiaPropertyRangeValueSmallChange)
		if err != nil {
			return nil, err
		}
		actions = append(actions, uiaRangeValueActions(readOnly, rangeAvailable, supported, step)...)
	}
	return uniqueUIAStrings(actions), nil
}

func (query *uiaCOMQuery) value(role string, properties *uiaPropertyReader) (string, bool, error) {
	valueAvailable, err := properties.bool(uiaPropertyValueAvailable)
	if err != nil {
		return "", false, err
	}
	if valueAvailable && (role == "textbox" || role == "combobox") {
		return properties.text(uiaPropertyValueValue)
	}
	rangeAvailable, err := properties.bool(uiaPropertyRangeValueAvailable)
	if err != nil {
		return "", false, err
	}
	if rangeAvailable && (role == "slider" || role == "progress") {
		value, supported, err := properties.number(uiaPropertyRangeValueValue)
		if err != nil || !supported {
			return "", false, err
		}
		formatted, err := uiaNumericValue(value)
		return formatted, err == nil, err
	}
	return "", false, nil
}

func (query *uiaCOMQuery) firstChild(ctx context.Context, element *ole.IUnknown) (*ole.IUnknown, error) {
	return query.walk(ctx, uiaWalkerMethodFirstChild, element)
}

func (query *uiaCOMQuery) nextSibling(ctx context.Context, element *ole.IUnknown) (*ole.IUnknown, error) {
	return query.walk(ctx, uiaWalkerMethodNextSibling, element)
}

func (query *uiaCOMQuery) walk(ctx context.Context, method int, element *ole.IUnknown) (*ole.IUnknown, error) {
	var result *ole.IUnknown
	if err := callUIAMethod(ctx, query.walker, method,
		uintptr(unsafe.Pointer(element)), uintptr(unsafe.Pointer(&result))); err != nil {
		if result != nil {
			result.Release()
		}
		return nil, err
	}
	return result, nil
}

func (query *uiaCOMQuery) release(element *ole.IUnknown) {
	if element != nil {
		element.Release()
	}
}

func (query *uiaCOMQuery) runtimeID(ctx context.Context, element *ole.IUnknown) (result []int32, err error) {
	var array *ole.SafeArray
	if callErr := callUIAMethod(ctx, element, uiaElementMethodRuntimeID,
		uintptr(unsafe.Pointer(&array))); callErr != nil {
		if array != nil {
			_, _, _ = procSafeArrayDestroy.Call(uintptr(unsafe.Pointer(array)))
		}
		return nil, callErr
	}
	if array == nil {
		return nil, ErrInvalidTree
	}
	defer func() {
		hr, _, _ := procSafeArrayDestroy.Call(uintptr(unsafe.Pointer(array)))
		if err == nil && failedHRESULT(hr) {
			result = nil
			err = normalizeUIAError(ctx, uiaCallError(uint32(hr)))
		}
	}()
	return readUIAIntArray(ctx, array)
}

func currentUIAPattern(ctx context.Context, element *ole.IUnknown, patternID int32) (*ole.IUnknown, error) {
	var pattern *ole.IUnknown
	if err := callUIAMethod(ctx, element, uiaElementMethodCurrentPattern,
		uintptr(patternID), uintptr(unsafe.Pointer(&pattern))); err != nil {
		if pattern != nil {
			pattern.Release()
		}
		return nil, err
	}
	if pattern == nil {
		return nil, ErrStaleTarget
	}
	return pattern, nil
}

func currentUIAPatternAvailable(ctx context.Context, element *ole.IUnknown, patternID int32) (bool, error) {
	var pattern *ole.IUnknown
	err := callUIAMethodRaw(ctx, element, uiaElementMethodCurrentPattern,
		uintptr(patternID), uintptr(unsafe.Pointer(&pattern)))
	if err != nil {
		if pattern != nil {
			pattern.Release()
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		var callErr uiaCallError
		if errors.As(err, &callErr) && uint32(callErr) == hResultUIANotSupported {
			return false, nil
		}
		return false, normalizeUIAError(ctx, err)
	}
	if pattern == nil {
		return false, ErrStaleTarget
	}
	pattern.Release()
	return true, nil
}

func setUIAFocus(ctx context.Context, element *ole.IUnknown) error {
	return callUIAMethod(ctx, element, uiaElementMethodSetFocus)
}

func callUIAPattern(ctx context.Context, pattern *ole.IUnknown, method int) error {
	return callUIAMethod(ctx, pattern, method)
}

func setUIAStringValue(ctx context.Context, pattern *ole.IUnknown, value string) error {
	allocated := ole.SysAllocString(value)
	if allocated == nil && value != "" {
		return ErrUnavailable
	}
	defer func() { _ = ole.SysFreeString(allocated) }()
	return callUIAMethod(ctx, pattern, uiaPatternMethodPrimary, uintptr(unsafe.Pointer(allocated)))
}

func setUIARangeValue(ctx context.Context, pattern *ole.IUnknown, value float64) error {
	bits := math.Float64bits(value)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		return callUIAMethod(ctx, pattern, uiaPatternMethodPrimary,
			uintptr(uint32(bits)), uintptr(uint32(bits>>32)))
	}
	return callUIAMethod(ctx, pattern, uiaPatternMethodPrimary, uintptr(bits))
}

func uiaPatternNumber(ctx context.Context, pattern *ole.IUnknown, method int) (float64, error) {
	var value float64
	if err := callUIAMethod(ctx, pattern, method, uintptr(unsafe.Pointer(&value))); err != nil {
		return 0, err
	}
	return value, nil
}

type uiaPropertyReader struct {
	ctx      context.Context
	element  *ole.IUnknown
	maxBytes uint32
	cache    map[int32]uiaPropertyValue
}

func newUIAPropertyReader(ctx context.Context, element *ole.IUnknown, maxBytes uint32) *uiaPropertyReader {
	return &uiaPropertyReader{
		ctx: ctx, element: element, maxBytes: maxBytes,
		cache: make(map[int32]uiaPropertyValue),
	}
}

type uiaPropertyKind uint8

const (
	uiaPropertyMissing uiaPropertyKind = iota
	uiaPropertyBoolean
	uiaPropertyInteger
	uiaPropertyNumber
	uiaPropertyText
)

type uiaPropertyValue struct {
	kind    uiaPropertyKind
	boolean bool
	integer int32
	number  float64
	text    string
}

func (reader *uiaPropertyReader) read(property int32) (uiaPropertyValue, error) {
	if value, ok := reader.cache[property]; ok {
		return value, nil
	}
	var value ole.VARIANT
	err := callUIAMethod(reader.ctx, reader.element, uiaElementMethodPropertyValue,
		uintptr(property), uintptr(unsafe.Pointer(&value)))
	defer func() { _ = value.Clear() }()
	if err != nil {
		return uiaPropertyValue{}, err
	}
	result := uiaPropertyValue{}
	switch value.VT {
	case ole.VT_EMPTY, ole.VT_UNKNOWN:
		result.kind = uiaPropertyMissing
	case ole.VT_BOOL:
		result.kind = uiaPropertyBoolean
		result.boolean = value.Val&0xffff != 0
	case ole.VT_I4:
		result.kind = uiaPropertyInteger
		result.integer = int32(value.Val)
	case ole.VT_R8:
		result.kind = uiaPropertyNumber
		result.number = math.Float64frombits(uint64(value.Val))
	case ole.VT_BSTR:
		result.kind = uiaPropertyText
		result.text = boundedBSTR(*(**uint16)(unsafe.Pointer(&value.Val)), reader.maxBytes)
	default:
		return uiaPropertyValue{}, fmt.Errorf(
			"uia property %d has unsupported VARIANT type %d: %w", property, value.VT, ErrInvalidTree,
		)
	}
	reader.cache[property] = result
	return result, nil
}

func (reader *uiaPropertyReader) bool(property int32) (bool, error) {
	value, err := reader.read(property)
	if err != nil {
		return false, err
	}
	switch value.kind {
	case uiaPropertyMissing:
		return false, nil
	case uiaPropertyBoolean:
		return value.boolean, nil
	default:
		return false, invalidUIAPropertyKind(property, "boolean", value.kind)
	}
}

func (reader *uiaPropertyReader) requiredBool(property int32) (bool, error) {
	value, err := reader.read(property)
	if err != nil {
		return false, err
	}
	if value.kind != uiaPropertyBoolean {
		return false, invalidUIAPropertyKind(property, "boolean", value.kind)
	}
	return value.boolean, nil
}

func (reader *uiaPropertyReader) integer(property int32) (int32, bool, error) {
	value, err := reader.read(property)
	if err != nil {
		return 0, false, err
	}
	switch value.kind {
	case uiaPropertyMissing:
		return 0, false, nil
	case uiaPropertyInteger:
		return value.integer, true, nil
	default:
		return 0, false, invalidUIAPropertyKind(property, "integer", value.kind)
	}
}

func (reader *uiaPropertyReader) number(property int32) (float64, bool, error) {
	value, err := reader.read(property)
	if err != nil {
		return 0, false, err
	}
	switch value.kind {
	case uiaPropertyMissing:
		return 0, false, nil
	case uiaPropertyNumber:
		return value.number, true, nil
	default:
		return 0, false, invalidUIAPropertyKind(property, "number", value.kind)
	}
}

func (reader *uiaPropertyReader) text(property int32) (string, bool, error) {
	value, err := reader.read(property)
	if err != nil {
		return "", false, err
	}
	switch value.kind {
	case uiaPropertyMissing:
		return "", false, nil
	case uiaPropertyText:
		return value.text, true, nil
	default:
		return "", false, invalidUIAPropertyKind(property, "text", value.kind)
	}
}

func invalidUIAPropertyKind(property int32, expected string, actual uiaPropertyKind) error {
	return fmt.Errorf(
		"uia property %d expected %s kind, got %d: %w", property, expected, actual, ErrInvalidTree,
	)
}

func (reader *uiaPropertyReader) readOnly(role string) (bool, error) {
	if role != "textbox" && role != "combobox" && role != "slider" {
		return false, nil
	}
	if role != "slider" {
		valueAvailable, err := reader.bool(uiaPropertyValueAvailable)
		if err != nil {
			return false, err
		}
		if valueAvailable {
			return reader.bool(uiaPropertyValueReadOnly)
		}
	}
	rangeAvailable, err := reader.bool(uiaPropertyRangeValueAvailable)
	if err != nil || !rangeAvailable {
		return false, err
	}
	return reader.bool(uiaPropertyRangeValueReadOnly)
}

func (reader *uiaPropertyReader) readOnlyState(role string) (bool, bool, error) {
	if role != "textbox" && role != "combobox" && role != "slider" {
		return false, false, nil
	}
	if role != "slider" {
		valueAvailable, err := reader.bool(uiaPropertyValueAvailable)
		if err != nil {
			return false, false, err
		}
		if valueAvailable {
			value, err := reader.requiredBool(uiaPropertyValueReadOnly)
			return value, true, err
		}
	}
	rangeAvailable, err := reader.bool(uiaPropertyRangeValueAvailable)
	if err != nil || !rangeAvailable {
		return false, false, err
	}
	value, err := reader.requiredBool(uiaPropertyRangeValueReadOnly)
	return value, true, err
}

func elementInt32(ctx context.Context, element *ole.IUnknown, method int) (int32, error) {
	var value int32
	if err := callUIAMethod(ctx, element, method, uintptr(unsafe.Pointer(&value))); err != nil {
		return 0, err
	}
	return value, nil
}

func elementBool(ctx context.Context, element *ole.IUnknown, method int) (bool, error) {
	value, err := elementInt32(ctx, element, method)
	return value != 0, err
}

func elementBSTR(ctx context.Context, element *ole.IUnknown, method int, maxBytes uint32) (string, error) {
	value, _, err := elementBSTRWithTruncation(ctx, element, method, maxBytes)
	return value, err
}

func elementBSTRWithTruncation(ctx context.Context, element *ole.IUnknown, method int, maxBytes uint32) (string, bool, error) {
	var value *uint16
	if err := callUIAMethod(ctx, element, method, uintptr(unsafe.Pointer(&value))); err != nil {
		if value != nil {
			_ = ole.SysFreeString((*int16)(unsafe.Pointer(value)))
		}
		return "", false, err
	}
	if value == nil {
		return "", false, nil
	}
	defer func() { _ = ole.SysFreeString((*int16)(unsafe.Pointer(value))) }()
	result := boundedBSTR(value, maxBytes)
	units := int(ole.SysStringLen((*int16)(unsafe.Pointer(value))))
	truncated := units > int(maxBytes)
	if !truncated {
		truncated = len(string(utf16.Decode(unsafe.Slice(value, units)))) > int(maxBytes)
	}
	return result, truncated, nil
}

func boundedBSTR(value *uint16, maxBytes uint32) string {
	if value == nil || maxBytes == 0 {
		return ""
	}
	units := int(ole.SysStringLen((*int16)(unsafe.Pointer(value))))
	if units > int(maxBytes) {
		units = int(maxBytes)
	}
	decoded := string(utf16.Decode(unsafe.Slice(value, units)))
	if len(decoded) <= int(maxBytes) {
		return decoded
	}
	decoded = decoded[:maxBytes]
	for len(decoded) > 0 && !utf8.ValidString(decoded) {
		decoded = decoded[:len(decoded)-1]
	}
	return decoded
}

func elementBounds(ctx context.Context, element *ole.IUnknown) (*Bounds, error) {
	var value windows.Rect
	if err := callUIAMethod(ctx, element, uiaElementMethodBounds, uintptr(unsafe.Pointer(&value))); err != nil {
		return nil, err
	}
	return boundsFromWindowsRect(value)
}

func boundsFromWindowsRect(value windows.Rect) (*Bounds, error) {
	left, top := int64(value.Left), int64(value.Top)
	width, height := int64(value.Right)-left, int64(value.Bottom)-top
	if width <= 0 || height <= 0 {
		return nil, nil
	}
	leftInt, topInt, widthInt, heightInt := int(left), int(top), int(width), int(height)
	if int64(leftInt) != left || int64(topInt) != top || int64(widthInt) != width || int64(heightInt) != height {
		return nil, ErrInvalidTree
	}
	return &Bounds{X: leftInt, Y: topInt, Width: widthInt, Height: heightInt}, nil
}

func readUIAIntArray(ctx context.Context, array *ole.SafeArray) ([]int32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dimensions, _, _ := procSafeArrayGetDim.Call(uintptr(unsafe.Pointer(array)))
	elementSize, _, _ := procSafeArrayGetSize.Call(uintptr(unsafe.Pointer(array)))
	if dimensions != 1 || elementSize != 4 {
		return nil, ErrInvalidTree
	}
	var lower, upper int32
	if hr, _, _ := procSafeArrayGetLow.Call(uintptr(unsafe.Pointer(array)), 1, uintptr(unsafe.Pointer(&lower))); failedHRESULT(hr) {
		return nil, normalizeUIAError(ctx, uiaCallError(uint32(hr)))
	}
	if hr, _, _ := procSafeArrayGetHigh.Call(uintptr(unsafe.Pointer(array)), 1, uintptr(unsafe.Pointer(&upper))); failedHRESULT(hr) {
		return nil, normalizeUIAError(ctx, uiaCallError(uint32(hr)))
	}
	count := int64(upper) - int64(lower) + 1
	if count <= 0 || count > maxUIARuntimeIDInts {
		return nil, ErrInvalidTree
	}
	result := make([]int32, int(count))
	for offset := range result {
		index := lower + int32(offset)
		hr, _, _ := procSafeArrayGetItem.Call(
			uintptr(unsafe.Pointer(array)), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&result[offset])),
		)
		if failedHRESULT(hr) {
			return nil, normalizeUIAError(ctx, uiaCallError(uint32(hr)))
		}
	}
	return result, nil
}

func callUIAMethod(ctx context.Context, object *ole.IUnknown, index int, arguments ...uintptr) error {
	err := callUIAMethodRaw(ctx, object, index, arguments...)
	if err == nil {
		return nil
	}
	return normalizeUIAError(ctx, err)
}

func callUIAMethodRaw(ctx context.Context, object *ole.IUnknown, index int, arguments ...uintptr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	method := comMethod(object, index)
	if method == 0 {
		return ErrUnavailable
	}
	args := make([]uintptr, 1, len(arguments)+1)
	args[0] = uintptr(unsafe.Pointer(object))
	args = append(args, arguments...)
	hr, _, _ := syscall.SyscallN(method, args...)
	runtime.KeepAlive(object)
	if failedHRESULT(hr) {
		return uiaCallError(uint32(hr))
	}
	return nil
}

func comMethod(object *ole.IUnknown, index int) uintptr {
	if object == nil || index < 0 {
		return 0
	}
	vtable := *(*unsafe.Pointer)(unsafe.Pointer(object))
	if vtable == nil {
		return 0
	}
	return *(*uintptr)(unsafe.Add(vtable, uintptr(index)*unsafe.Sizeof(uintptr(0))))
}

func failedHRESULT(value uintptr) bool { return int32(uint32(value)) < 0 }

func normalizeUIAError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var callErr uiaCallError
	if errors.As(err, &callErr) {
		switch uint32(callErr) {
		case hResultAccessDenied:
			return ErrPermissionDenied
		case hResultClassNotRegistered, hResultNoInterface, hResultUIANotSupported:
			return ErrUnsupported
		case hResultUIAElementNotFound:
			return ErrStaleTarget
		case hResultCallCanceled, hResultUIATimeout:
			return ErrUnavailable
		default:
			return ErrUnavailable
		}
	}
	var oleErr *ole.OleError
	if errors.As(err, &oleErr) {
		return normalizeUIAError(ctx, uiaCallError(uint32(oleErr.Code())))
	}
	return ErrUnavailable
}

func uniqueUIAStrings(values []string) []string {
	result := values[:0]
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
