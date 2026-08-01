//go:build linux

package accessibility

import (
	"context"
	"crypto/sha256"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	atspiRoleAlert          uint32 = 2
	atspiRoleCheckBox       uint32 = 7
	atspiRoleComboBox       uint32 = 11
	atspiRoleDialog         uint32 = 16
	atspiRoleFrame          uint32 = 23
	atspiRoleIcon           uint32 = 26
	atspiRoleImage          uint32 = 27
	atspiRoleLabel          uint32 = 29
	atspiRoleList           uint32 = 31
	atspiRoleListItem       uint32 = 32
	atspiRoleMenu           uint32 = 33
	atspiRoleMenuBar        uint32 = 34
	atspiRoleMenuItem       uint32 = 35
	atspiRolePageTab        uint32 = 37
	atspiRolePageTabList    uint32 = 38
	atspiRolePanel          uint32 = 39
	atspiRolePasswordText   uint32 = 40
	atspiRoleProgressBar    uint32 = 42
	atspiRoleButton         uint32 = 43
	atspiRoleRadioButton    uint32 = 44
	atspiRoleRadioMenuItem  uint32 = 45
	atspiRoleRootPane       uint32 = 46
	atspiRoleScrollBar      uint32 = 48
	atspiRoleScrollPane     uint32 = 49
	atspiRoleSlider         uint32 = 51
	atspiRoleTable          uint32 = 55
	atspiRoleTableCell      uint32 = 56
	atspiRoleTableColumn    uint32 = 57
	atspiRoleTableRowHeader uint32 = 58
	atspiRoleTearoffMenu    uint32 = 59
	atspiRoleText           uint32 = 61
	atspiRoleToggleButton   uint32 = 62
	atspiRoleTree           uint32 = 65
	atspiRoleTreeTable      uint32 = 66
	atspiRoleWindow         uint32 = 69
	atspiRoleApplication    uint32 = 75
	atspiRoleEntry          uint32 = 79
	atspiRoleCaption        uint32 = 81
	atspiRoleHeading        uint32 = 83
	atspiRoleLink           uint32 = 88
	atspiRoleTableRow       uint32 = 90
	atspiRoleTreeItem       uint32 = 91
	atspiRoleListBox        uint32 = 98
	atspiRoleGrouping       uint32 = 99
	atspiRoleImageMap       uint32 = 100
	atspiRoleNotification   uint32 = 101
	atspiRoleInfoBar        uint32 = 102
	atspiRoleLevelBar       uint32 = 103
	atspiRoleStatic         uint32 = 116
	atspiRolePushButtonMenu uint32 = 129
	atspiRoleSwitch         uint32 = 130
)

const (
	atspiStateChecked   uint32 = 4
	atspiStateCollapsed uint32 = 5
	atspiStateEditable  uint32 = 7
	atspiStateEnabled   uint32 = 8
	atspiStateExpanded  uint32 = 10
	atspiStateFocusable uint32 = 11
	atspiStateFocused   uint32 = 12
	atspiStateSelected  uint32 = 23
	atspiStateShowing   uint32 = 25
	atspiStateVisible   uint32 = 30
	atspiStateRequired  uint32 = 33
	atspiStateInvalid   uint32 = 36
)

type atspiStringBudget struct {
	remaining int
	truncated bool
}

func (budget *atspiStringBudget) take(value string) string {
	if value == "" {
		return ""
	}
	if budget.remaining <= 0 {
		budget.truncated = true
		return ""
	}
	if len(value) <= budget.remaining {
		budget.remaining -= len(value)
		return value
	}
	value = value[:budget.remaining]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	budget.remaining -= len(value)
	budget.truncated = true
	return value
}

func buildATSPITree(ctx context.Context, query atspiQuery, root atspiReference, limits Limits) (Snapshot, error) {
	snapshot := Snapshot{Backend: BackendATSPI2, Nodes: make([]Node, 0, limits.MaxElements)}
	budget := &atspiStringBudget{remaining: int(limits.MaxStringBytes)}
	seen := make(map[[sha256.Size]byte]struct{}, limits.MaxElements)
	referenceBytes := 0
	var visit func(atspiReference, int, uint32) error
	visit = func(reference atspiReference, parent int, depth uint32) error {
		if len(snapshot.Nodes) >= int(limits.MaxElements) {
			snapshot.Truncated = true
			return nil
		}
		if !validATSPIReference(reference) || depth > limits.MaxDepth {
			return ErrInvalidTree
		}
		referenceData := make([]byte, 0, len(reference.Bus)+1+len(reference.Path))
		referenceData = append(referenceData, reference.Bus...)
		referenceData = append(referenceData, 0)
		referenceData = append(referenceData, reference.Path...)
		referenceDigest := sha256.Sum256(referenceData)
		if _, duplicate := seen[referenceDigest]; duplicate {
			return ErrInvalidTree
		}
		seen[referenceDigest] = struct{}{}
		if len(referenceData) > int(limits.MaxReferenceBytes) ||
			len(referenceData) > int(limits.MaxTotalReferenceBytes)-referenceBytes {
			return ErrInvalidTree
		}
		referenceBytes += len(referenceData)

		roleID, err := query.role(ctx, reference)
		if err != nil {
			return normalizeATSPIError(err)
		}
		stateWords, err := query.states(ctx, reference)
		if err != nil {
			return normalizeATSPIError(err)
		}
		if len(stateWords) != 2 {
			return ErrInvalidTree
		}
		role := mapATSPIRole(roleID)
		hidden := !atspiStateSet(stateWords, atspiStateVisible)
		offscreen := !atspiStateSet(stateWords, atspiStateShowing)
		sensitive := roleID == atspiRolePasswordText
		if sensitive || hidden || offscreen {
			snapshot.Truncated = true
		}
		node := Node{
			Reference: referenceData,
			Parent:    parent, Depth: depth, Role: role,
			Sensitive: sensitive, Hidden: hidden, Offscreen: offscreen,
		}
		roleAllowed := limits.AllowedRoles[role]
		if roleAllowed && !hidden && !offscreen && !sensitive {
			if limits.ReadName {
				value, err := query.stringProperty(ctx, reference, atspiPropertyName)
				if err != nil {
					return normalizeATSPIError(err)
				}
				node.Name = budget.take(value)
			}
			if limits.ReadDescription {
				value, err := query.stringProperty(ctx, reference, atspiPropertyDescription)
				if err != nil {
					return normalizeATSPIError(err)
				}
				node.Description = budget.take(value)
			}
		}
		if limits.ReadStates {
			node.States = mapATSPIStates(role, stateWords)
		}
		if limits.ReadFocus {
			node.Focused = atspiStateSet(stateWords, atspiStateFocused)
		}

		needsInterfaces := roleAllowed && !hidden && !offscreen && !sensitive &&
			(limits.ReadActions || limits.ReadValue || limits.ReadBounds)
		interfaces := map[string]bool{}
		if needsInterfaces {
			values, err := query.interfaces(ctx, reference)
			if err != nil {
				return normalizeATSPIError(err)
			}
			if len(values) > maxATSPIInterfaces {
				return ErrInvalidTree
			}
			for _, value := range values {
				if value == "" || len(value) > maxATSPIInterfaceBytes || strings.IndexByte(value, 0) >= 0 {
					return ErrInvalidTree
				}
				interfaces[value] = true
			}
		}
		if limits.ReadBounds && interfaces[atspiShortComponent] {
			rect, err := query.extents(ctx, reference)
			if err != nil {
				return normalizeATSPIError(err)
			}
			node.Bounds = &Bounds{X: int(rect.X), Y: int(rect.Y), Width: int(rect.Width), Height: int(rect.Height)}
		}
		if limits.ReadActions {
			hasDefaultAction := false
			if interfaces[atspiShortAction] && defaultATSPIAction(role) != "" {
				count, err := query.actionCount(ctx, reference)
				if err != nil {
					return normalizeATSPIError(err)
				}
				if count < 0 || count > maxATSPIActions {
					return ErrInvalidTree
				}
				hasDefaultAction = count > 0
			}
			node.Actions = inferATSPIActions(role, stateWords, interfaces, hasDefaultAction)
		}
		if limits.ReadValue {
			value, err := readATSPIValue(ctx, query, reference, role, interfaces, limits.MaxStringBytes)
			if err != nil {
				return normalizeATSPIError(err)
			}
			node.Value = budget.take(value)
		}

		nodeIndex := len(snapshot.Nodes)
		snapshot.Nodes = append(snapshot.Nodes, node)
		if sensitive || hidden || offscreen {
			return nil
		}
		count, err := query.childCount(ctx, reference)
		if err != nil {
			return normalizeATSPIError(err)
		}
		if count < 0 {
			return ErrInvalidTree
		}
		if count == 0 {
			return nil
		}
		if depth == limits.MaxDepth {
			snapshot.Truncated = true
			return nil
		}
		for index := int32(0); index < count; index++ {
			if len(snapshot.Nodes) >= int(limits.MaxElements) {
				snapshot.Truncated = true
				break
			}
			child, err := query.child(ctx, reference, index)
			if err != nil {
				return normalizeATSPIError(err)
			}
			if err := visit(child, nodeIndex, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, -1, 0); err != nil {
		clearSnapshot(&snapshot)
		return Snapshot{}, err
	}
	snapshot.Truncated = snapshot.Truncated || budget.truncated
	return snapshot, nil
}

func readATSPIValue(
	ctx context.Context,
	query atspiQuery,
	reference atspiReference,
	role string,
	interfaces map[string]bool,
	maxBytes uint32,
) (string, error) {
	if role == "textbox" && interfaces[atspiShortText] {
		limit := maxBytes
		if limit > math.MaxInt32 {
			limit = math.MaxInt32
		}
		return query.text(ctx, reference, int32(limit))
	}
	if interfaces[atspiShortValue] {
		value, err := query.currentValue(ctx, reference)
		if err != nil {
			return "", err
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", ErrInvalidTree
		}
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	}
	return "", nil
}

func atspiTopLevelRole(role uint32) bool {
	return role == atspiRoleDialog || role == atspiRoleFrame || role == atspiRoleWindow
}

func mapATSPIRole(role uint32) string {
	switch role {
	case atspiRoleAlert, atspiRoleDialog, atspiRoleNotification:
		return "dialog"
	case atspiRoleCheckBox, atspiRoleToggleButton:
		return "checkbox"
	case atspiRoleComboBox:
		return "combobox"
	case atspiRoleSwitch:
		return "switch"
	case atspiRoleFrame, atspiRoleWindow:
		return "window"
	case atspiRoleIcon, atspiRoleImage, atspiRoleImageMap:
		return "image"
	case atspiRoleLabel, atspiRoleCaption, atspiRoleHeading, atspiRoleStatic:
		return "label"
	case atspiRoleList, atspiRoleTree, atspiRoleListBox:
		return "list"
	case atspiRoleListItem, atspiRoleTreeItem:
		return "list-item"
	case atspiRoleMenu, atspiRoleMenuBar:
		return "menu"
	case atspiRoleMenuItem, atspiRoleRadioMenuItem, atspiRoleTearoffMenu:
		return "menu-item"
	case atspiRolePageTab:
		return "tab"
	case atspiRolePageTabList:
		return "tab-panel"
	case atspiRolePanel, atspiRoleRootPane, atspiRoleScrollPane, atspiRoleGrouping, atspiRoleInfoBar:
		return "group"
	case atspiRolePasswordText:
		return "password"
	case atspiRoleProgressBar, atspiRoleLevelBar:
		return "progress"
	case atspiRoleButton, atspiRolePushButtonMenu:
		return "button"
	case atspiRoleRadioButton:
		return "radio"
	case atspiRoleScrollBar, atspiRoleSlider:
		return "slider"
	case atspiRoleTable, atspiRoleTreeTable:
		return "table"
	case atspiRoleTableCell, atspiRoleTableColumn, atspiRoleTableRowHeader:
		return "cell"
	case atspiRoleText, atspiRoleEntry:
		return "textbox"
	case atspiRoleApplication:
		return "application"
	case atspiRoleLink:
		return "link"
	case atspiRoleTableRow:
		return "row"
	default:
		return "generic"
	}
}

func mapATSPIStates(role string, words []uint32) []string {
	states := make([]string, 0, 5)
	if atspiStateSet(words, atspiStateEnabled) {
		states = append(states, "enabled")
	} else if interactiveRole(role) {
		states = append(states, "disabled")
	}
	for _, candidate := range []struct {
		bit   uint32
		state string
	}{
		{atspiStateChecked, "checked"}, {atspiStateCollapsed, "collapsed"}, {atspiStateExpanded, "expanded"},
		{atspiStateSelected, "selected"}, {atspiStateRequired, "required"}, {atspiStateInvalid, "invalid"},
	} {
		if atspiStateSet(words, candidate.bit) {
			states = append(states, candidate.state)
		}
	}
	if role == "textbox" && !atspiStateSet(words, atspiStateEditable) {
		states = append(states, "read-only")
	}
	return states
}

func inferATSPIActions(
	role string,
	words []uint32,
	interfaces map[string]bool,
	hasDefaultAction bool,
) []string {
	actions := make([]string, 0, 4)
	if action := defaultATSPIAction(role); hasDefaultAction && action != "" {
		actions = append(actions, action)
	}
	if interfaces[atspiShortComponent] && atspiStateSet(words, atspiStateFocusable) {
		actions = append(actions, "focus")
	}
	if role == "textbox" && interfaces[atspiShortEditableText] {
		actions = append(actions, "set-value")
	}
	if atspiStateSet(words, atspiStateCollapsed) {
		actions = append(actions, "expand")
	}
	if atspiStateSet(words, atspiStateExpanded) {
		actions = append(actions, "collapse")
	}
	if interfaces[atspiShortValue] && role == "slider" {
		actions = append(actions, "set-value", "increment", "decrement")
	}
	return uniqueStrings(actions)
}

func defaultATSPIAction(role string) string {
	switch role {
	case "button", "link", "menu-item", "tab":
		return "press"
	case "checkbox", "radio", "switch":
		return "toggle"
	default:
		return ""
	}
}

func uniqueStrings(values []string) []string {
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

func interactiveRole(role string) bool {
	switch role {
	case "button", "checkbox", "combobox", "radio", "switch", "textbox", "link", "menu-item", "tab", "slider":
		return true
	default:
		return false
	}
}

func atspiStateSet(words []uint32, state uint32) bool {
	word := state / 32
	return int(word) < len(words) && words[word]&(uint32(1)<<(state%32)) != 0
}
