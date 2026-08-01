package darwinwindow

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"unicode/utf8"
)

const accessibilityReferenceVersion = byte(1)

type axSemanticStructure struct {
	Role      string
	Sensitive bool
	Hidden    bool
	Offscreen bool
}

type axSemanticDetails struct {
	Name        string
	Description string
	Value       string
	States      []string
	Bounds      *AccessibilityBounds
	Focused     bool
	Actions     []string
}

type axSemanticQuery[T comparable] interface {
	processID(context.Context, T) (int32, error)
	structure(context.Context, T) (axSemanticStructure, error)
	details(context.Context, T, string, AccessibilityLimits) (axSemanticDetails, error)
	children(context.Context, T) ([]T, bool, error)
	release(T)
}

// buildAXSemanticTree takes ownership of root and every reference returned by
// query. References are observation-scoped paths, never native AX pointers.
func buildAXSemanticTree[T comparable](
	ctx context.Context,
	query axSemanticQuery[T],
	root T,
	processID int32,
	windowID uint32,
	limits AccessibilityLimits,
) (AccessibilitySnapshot, error) {
	snapshot := AccessibilitySnapshot{
		Backend: AccessibilityBackend,
		Nodes:   make([]AccessibilityNode, 0, limits.MaxElements),
	}
	if processID <= 0 || windowID == 0 || !validAccessibilityLimits(limits) {
		query.release(root)
		return AccessibilitySnapshot{}, ErrAccessibilityInvalidTree
	}
	budget := axStringBudget{remaining: int(limits.MaxStringBytes)}
	seen := make(map[[sha256.Size]byte]struct{}, limits.MaxElements)
	seenElements := make(map[T]struct{}, limits.MaxElements)
	referenceBytes := 0

	var visit func(T, int, uint32, []uint32, bool) error
	visit = func(element T, parent int, depth uint32, path []uint32, rootElement bool) error {
		defer query.release(element)
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(snapshot.Nodes) >= int(limits.MaxElements) {
			snapshot.Truncated = true
			return nil
		}
		if _, duplicate := seenElements[element]; duplicate {
			return ErrAccessibilityInvalidTree
		}
		seenElements[element] = struct{}{}
		owner, err := query.processID(ctx, element)
		if err != nil {
			return err
		}
		if owner != processID {
			if rootElement {
				return ErrAccessibilityStaleTarget
			}
			snapshot.Truncated = true
			return nil
		}
		structure, err := query.structure(ctx, element)
		if err != nil {
			return err
		}
		reference, err := encodeAccessibilityReference(processID, windowID, path)
		if err != nil || len(reference) > int(limits.MaxReferenceBytes) ||
			len(reference) > int(limits.MaxTotalReferenceBytes)-referenceBytes {
			return ErrAccessibilityInvalidTree
		}
		digest := sha256.Sum256(reference)
		if _, duplicate := seen[digest]; duplicate {
			return ErrAccessibilityInvalidTree
		}
		seen[digest] = struct{}{}
		referenceBytes += len(reference)

		node := AccessibilityNode{
			Reference: reference, Parent: parent, Depth: depth, Role: structure.Role,
			Sensitive: structure.Sensitive, Hidden: structure.Hidden, Offscreen: structure.Offscreen,
		}
		roleAllowed := limits.AllowedRoles[structure.Role]
		if structure.Sensitive || structure.Hidden || structure.Offscreen {
			snapshot.Truncated = true
		}
		if roleAllowed && !structure.Sensitive && !structure.Hidden && !structure.Offscreen {
			details, detailErr := query.details(ctx, element, structure.Role, limits)
			if detailErr != nil {
				return detailErr
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
		if structure.Sensitive || structure.Hidden || structure.Offscreen {
			return nil
		}

		children, childrenTruncated, childErr := query.children(ctx, element)
		if childErr != nil {
			return childErr
		}
		snapshot.Truncated = snapshot.Truncated || childrenTruncated
		if depth == limits.MaxDepth {
			if len(children) != 0 {
				snapshot.Truncated = true
			}
			for _, child := range children {
				query.release(child)
			}
			return nil
		}
		for index, child := range children {
			if len(snapshot.Nodes) >= int(limits.MaxElements) {
				snapshot.Truncated = true
				for _, remaining := range children[index:] {
					query.release(remaining)
				}
				break
			}
			childPath := append(append([]uint32(nil), path...), uint32(index))
			if err := visit(child, nodeIndex, depth+1, childPath, false); err != nil {
				for _, remaining := range children[index+1:] {
					query.release(remaining)
				}
				return err
			}
		}
		return nil
	}

	if err := visit(root, -1, 0, nil, true); err != nil {
		clearAccessibilitySnapshot(&snapshot)
		return AccessibilitySnapshot{}, err
	}
	snapshot.Truncated = snapshot.Truncated || budget.truncated
	return snapshot, nil
}

func validAccessibilityLimits(limits AccessibilityLimits) bool {
	return limits.MaxElements > 0 && limits.MaxDepth > 0 && limits.MaxStringBytes > 0 &&
		limits.MaxReferenceBytes > 0 && limits.MaxTotalReferenceBytes > 0
}

func encodeAccessibilityReference(processID int32, windowID uint32, path []uint32) ([]byte, error) {
	if processID <= 0 || windowID == 0 || len(path) > int(^uint16(0)) {
		return nil, ErrAccessibilityInvalidTree
	}
	reference := make([]byte, 11+len(path)*4)
	reference[0] = accessibilityReferenceVersion
	binary.LittleEndian.PutUint32(reference[1:5], uint32(processID))
	binary.LittleEndian.PutUint32(reference[5:9], windowID)
	binary.LittleEndian.PutUint16(reference[9:11], uint16(len(path)))
	for index, child := range path {
		binary.LittleEndian.PutUint32(reference[11+index*4:], child)
	}
	return reference, nil
}

type axStringBudget struct {
	remaining int
	truncated bool
}

func (budget *axStringBudget) take(value string) string {
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

func mapAXRole(role, subrole string, sensitive bool) string {
	if sensitive {
		return "password"
	}
	switch role {
	case "AXApplication":
		return "application"
	case "AXWindow":
		if subrole == "AXDialog" || subrole == "AXSystemDialog" {
			return "dialog"
		}
		return "window"
	case "AXButton":
		return "button"
	case "AXCheckBox":
		return "checkbox"
	case "AXComboBox", "AXPopUpButton":
		return "combobox"
	case "AXRadioButton":
		return "radio"
	case "AXSwitch":
		return "switch"
	case "AXTextField", "AXTextArea", "AXSearchField":
		return "textbox"
	case "AXStaticText", "AXHeading":
		return "label"
	case "AXLink":
		return "link"
	case "AXList", "AXOutline":
		return "list"
	case "AXRow":
		return "row"
	case "AXMenu", "AXMenuBar":
		return "menu"
	case "AXMenuItem":
		return "menu-item"
	case "AXTab":
		return "tab"
	case "AXTabGroup":
		return "tab-panel"
	case "AXSlider", "AXScrollBar", "AXIncrementor":
		return "slider"
	case "AXProgressIndicator":
		return "progress"
	case "AXImage":
		return "image"
	case "AXTable":
		return "table"
	case "AXCell", "AXColumn":
		return "cell"
	case "AXGroup", "AXScrollArea", "AXSplitGroup", "AXToolbar":
		return "group"
	default:
		return "generic"
	}
}

func structuralAXRole(role string) bool {
	switch role {
	case "application", "window", "dialog", "group", "list", "menu", "table", "row":
		return true
	default:
		return false
	}
}
