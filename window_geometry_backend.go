//go:build cgo

package robotgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	swayNodeTypeContainer         = "con"
	swayNodeTypeFloatingContainer = "floating_con"
)

type compositorWindowRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"width"`
	H int `json:"height"`
}

func (rect compositorWindowRect) publicRect() Rect {
	return Rect{
		Point: Point{X: rect.X, Y: rect.Y},
		Size:  Size{W: rect.W, H: rect.H},
	}
}

type swayTreeNode struct {
	Name          string               `json:"name"`
	Type          string               `json:"type"`
	Focused       bool                 `json:"focused"`
	Rect          compositorWindowRect `json:"rect"`
	WindowRect    compositorWindowRect `json:"window_rect"`
	Nodes         []swayTreeNode       `json:"nodes"`
	FloatingNodes []swayTreeNode       `json:"floating_nodes"`
}

func findFocusedSwayWindow(node swayTreeNode) (swayTreeNode, bool) {
	if node.Focused &&
		(node.Type == swayNodeTypeContainer ||
			node.Type == swayNodeTypeFloatingContainer) {
		return node, true
	}
	for _, child := range node.Nodes {
		if focused, ok := findFocusedSwayWindow(child); ok {
			return focused, true
		}
	}
	for _, child := range node.FloatingNodes {
		if focused, ok := findFocusedSwayWindow(child); ok {
			return focused, true
		}
	}
	return swayTreeNode{}, false
}

func getSwayActiveWindow() (swayTreeNode, error) {
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	out, err := runWindowCommand(ctx, cmdSwayMsg, argType, argGetTree, argRawJSON)
	if err != nil {
		return swayTreeNode{}, fmt.Errorf("query sway tree: %w", err)
	}
	var root swayTreeNode
	if err := json.Unmarshal(out, &root); err != nil {
		return swayTreeNode{}, fmt.Errorf("invalid sway tree json: %w", err)
	}
	node, ok := findFocusedSwayWindow(root)
	if !ok {
		return swayTreeNode{}, errors.New("sway tree has no focused window")
	}
	return node, nil
}

func getSwayActiveWindowTitle() (string, error) {
	node, err := getSwayActiveWindow()
	if err != nil {
		return "", fmt.Errorf("%w: %w", errWindowTitleUnavailable, err)
	}
	if strings.TrimSpace(node.Name) == "" {
		return "", errWindowTitleUnavailable
	}
	return node.Name, nil
}

type hyprlandActiveWindow struct {
	Title            string `json:"title"`
	At               []int  `json:"at"`
	Size             []int  `json:"size"`
	Fullscreen       *int   `json:"fullscreen"`
	FullscreenClient *int   `json:"fullscreenClient"`
}

func validateWindowRect(operation string, rect Rect) (Rect, error) {
	if rect.W <= 0 || rect.H <= 0 {
		return Rect{}, fmt.Errorf(
			"%w: %s returned invalid size %dx%d",
			errWindowGeometryUnavailable,
			operation,
			rect.W,
			rect.H,
		)
	}
	return rect, nil
}

func checkedWindowCoordinate(base, relative int) (int, error) {
	if relative > 0 && base > math.MaxInt-relative {
		return 0, errors.New("integer overflow")
	}
	if relative < 0 && base < math.MinInt-relative {
		return 0, errors.New("integer underflow")
	}
	return base + relative, nil
}
