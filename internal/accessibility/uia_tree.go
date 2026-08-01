package accessibility

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strconv"
	"unicode/utf8"
)

const (
	uiaReferenceVersion = byte(1)
	maxUIARuntimeIDInts = 256
)

// UI Automation control type identifiers are stable Windows API constants.
const (
	uiaControlButton      int32 = 50000
	uiaControlCheckBox    int32 = 50002
	uiaControlComboBox    int32 = 50003
	uiaControlEdit        int32 = 50004
	uiaControlHyperlink   int32 = 50005
	uiaControlImage       int32 = 50006
	uiaControlListItem    int32 = 50007
	uiaControlList        int32 = 50008
	uiaControlMenu        int32 = 50009
	uiaControlMenuBar     int32 = 50010
	uiaControlMenuItem    int32 = 50011
	uiaControlProgressBar int32 = 50012
	uiaControlRadioButton int32 = 50013
	uiaControlScrollBar   int32 = 50014
	uiaControlSlider      int32 = 50015
	uiaControlSpinner     int32 = 50016
	uiaControlTab         int32 = 50018
	uiaControlTabItem     int32 = 50019
	uiaControlText        int32 = 50020
	uiaControlTree        int32 = 50023
	uiaControlTreeItem    int32 = 50024
	uiaControlGroup       int32 = 50026
	uiaControlDataGrid    int32 = 50028
	uiaControlDataItem    int32 = 50029
	uiaControlDocument    int32 = 50030
	uiaControlSplitButton int32 = 50031
	uiaControlWindow      int32 = 50032
	uiaControlPane        int32 = 50033
	uiaControlHeader      int32 = 50034
	uiaControlHeaderItem  int32 = 50035
	uiaControlTable       int32 = 50036
)

type uiaNodeStructure struct {
	RuntimeID   []int32
	ControlType int32
	Password    bool
	Offscreen   bool
}

type uiaNodeDetails struct {
	Name        string
	Description string
	Value       string
	States      []string
	Bounds      *Bounds
	Focused     bool
	Actions     []string
}

// uiaTreeQuery keeps native object ownership behind a small interface so the
// privacy and lifecycle rules can be tested without a Windows desktop.
type uiaTreeQuery[T comparable] interface {
	processID(context.Context, T) (int32, error)
	structure(context.Context, T) (uiaNodeStructure, error)
	details(context.Context, T, string, Limits) (uiaNodeDetails, error)
	firstChild(context.Context, T) (T, error)
	nextSibling(context.Context, T) (T, error)
	release(T)
}

// buildUIATree takes ownership of root and every reference returned by query.
func buildUIATree[T comparable](
	ctx context.Context,
	query uiaTreeQuery[T],
	root T,
	expectedProcessID int32,
	limits Limits,
) (Snapshot, error) {
	snapshot := Snapshot{Backend: BackendWindowsAutomation, Nodes: make([]Node, 0, limits.MaxElements)}
	if expectedProcessID <= 0 || !validUIALimits(limits) {
		query.release(root)
		return Snapshot{}, ErrInvalidTree
	}
	budget := &uiaStringBudget{remaining: int(limits.MaxStringBytes)}
	seen := make(map[[sha256.Size]byte]struct{}, limits.MaxElements)
	referenceBytes := 0
	var zero T

	var visit func(T, int, uint32, bool) error
	visit = func(reference T, parent int, depth uint32, isRoot bool) error {
		defer query.release(reference)
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(snapshot.Nodes) >= int(limits.MaxElements) {
			snapshot.Truncated = true
			return nil
		}

		processID, err := query.processID(ctx, reference)
		if err != nil {
			return err
		}
		if processID != expectedProcessID {
			if isRoot {
				return ErrStaleTarget
			}
			// UIA may embed surfaces owned by another process. Never cross the
			// exact process boundary selected by policy.
			snapshot.Truncated = true
			return nil
		}
		structure, err := query.structure(ctx, reference)
		if err != nil {
			return err
		}
		referenceData, err := encodeUIAReference(processID, structure.RuntimeID)
		if err != nil || len(referenceData) > int(limits.MaxReferenceBytes) ||
			len(referenceData) > int(limits.MaxTotalReferenceBytes)-referenceBytes {
			return ErrInvalidTree
		}
		digest := sha256.Sum256(referenceData)
		if _, duplicate := seen[digest]; duplicate {
			return ErrInvalidTree
		}
		seen[digest] = struct{}{}
		referenceBytes += len(referenceData)

		role := mapUIAControlType(structure.ControlType, structure.Password)
		node := Node{
			Reference: referenceData, Parent: parent, Depth: depth, Role: role,
			Sensitive: structure.Password, Offscreen: structure.Offscreen,
		}
		roleAllowed := limits.AllowedRoles[role]
		if structure.Password || structure.Offscreen {
			snapshot.Truncated = true
		}
		if roleAllowed && !structure.Password && !structure.Offscreen {
			details, err := query.details(ctx, reference, role, limits)
			if err != nil {
				return err
			}
			if limits.ReadName {
				node.Name = budget.take(details.Name)
			}
			if limits.ReadDescription {
				node.Description = budget.take(details.Description)
			}
			if limits.ReadValue {
				node.Value = budget.take(details.Value)
			}
			if limits.ReadStates {
				node.States = append([]string(nil), details.States...)
			}
			if limits.ReadBounds {
				node.Bounds = details.Bounds
			}
			if limits.ReadFocus {
				node.Focused = details.Focused
			}
			if limits.ReadActions {
				node.Actions = append([]string(nil), details.Actions...)
			}
		}

		nodeIndex := len(snapshot.Nodes)
		snapshot.Nodes = append(snapshot.Nodes, node)
		if structure.Password || structure.Offscreen {
			return nil
		}
		if depth == limits.MaxDepth {
			child, err := query.firstChild(ctx, reference)
			if err != nil {
				return err
			}
			if child != zero {
				query.release(child)
				snapshot.Truncated = true
			}
			return nil
		}

		child, err := query.firstChild(ctx, reference)
		if err != nil {
			return err
		}
		for child != zero {
			if len(snapshot.Nodes) >= int(limits.MaxElements) {
				query.release(child)
				snapshot.Truncated = true
				break
			}
			next, err := query.nextSibling(ctx, child)
			if err != nil {
				query.release(child)
				return err
			}
			if err := visit(child, nodeIndex, depth+1, false); err != nil {
				if next != zero {
					query.release(next)
				}
				return err
			}
			child = next
		}
		return nil
	}

	if err := visit(root, -1, 0, true); err != nil {
		clearSnapshot(&snapshot)
		return Snapshot{}, err
	}
	snapshot.Truncated = snapshot.Truncated || budget.truncated
	return snapshot, nil
}

func validUIALimits(limits Limits) bool {
	return limits.MaxElements > 0 && limits.MaxDepth > 0 && limits.MaxStringBytes > 0 &&
		limits.MaxReferenceBytes > 0 && limits.MaxTotalReferenceBytes > 0
}

func encodeUIAReference(processID int32, runtimeID []int32) ([]byte, error) {
	if processID <= 0 || len(runtimeID) == 0 || len(runtimeID) > maxUIARuntimeIDInts {
		return nil, ErrInvalidTree
	}
	reference := make([]byte, 7+len(runtimeID)*4)
	reference[0] = uiaReferenceVersion
	binary.LittleEndian.PutUint32(reference[1:5], uint32(processID))
	binary.LittleEndian.PutUint16(reference[5:7], uint16(len(runtimeID)))
	for index, value := range runtimeID {
		binary.LittleEndian.PutUint32(reference[7+index*4:], uint32(value))
	}
	return reference, nil
}

type uiaStringBudget struct {
	remaining int
	truncated bool
}

func (budget *uiaStringBudget) take(value string) string {
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

func mapUIAControlType(controlType int32, password bool) string {
	if password {
		return "password"
	}
	switch controlType {
	case uiaControlButton, uiaControlSplitButton:
		return "button"
	case uiaControlCheckBox:
		return "checkbox"
	case uiaControlComboBox:
		return "combobox"
	case uiaControlEdit, uiaControlDocument:
		return "textbox"
	case uiaControlHyperlink:
		return "link"
	case uiaControlImage:
		return "image"
	case uiaControlListItem, uiaControlTreeItem:
		return "list-item"
	case uiaControlList, uiaControlTree:
		return "list"
	case uiaControlMenu, uiaControlMenuBar:
		return "menu"
	case uiaControlMenuItem:
		return "menu-item"
	case uiaControlProgressBar:
		return "progress"
	case uiaControlRadioButton:
		return "radio"
	case uiaControlScrollBar, uiaControlSlider, uiaControlSpinner:
		return "slider"
	case uiaControlTabItem:
		return "tab"
	case uiaControlTab:
		return "tab-panel"
	case uiaControlText, uiaControlHeader, uiaControlHeaderItem:
		return "label"
	case uiaControlDataGrid, uiaControlTable:
		return "table"
	case uiaControlDataItem:
		return "row"
	case uiaControlWindow:
		return "window"
	case uiaControlGroup, uiaControlPane:
		return "group"
	default:
		return "generic"
	}
}

func uiaNumericValue(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", ErrInvalidTree
	}
	return strconv.FormatFloat(value, 'g', -1, 64), nil
}
