//go:build cgo

package robotgo

import "strings"

const (
	waylandTransformNormal = iota
	waylandTransform90
	waylandTransform180
	waylandTransform270
	waylandTransformFlipped
	waylandTransformFlipped90
	waylandTransformFlipped180
	waylandTransformFlipped270
)

func parseWaylandTransform(value string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal":
		return waylandTransformNormal, true
	case "90":
		return waylandTransform90, true
	case "180":
		return waylandTransform180, true
	case "270":
		return waylandTransform270, true
	case "flipped":
		return waylandTransformFlipped, true
	case "flipped-90":
		return waylandTransformFlipped90, true
	case "flipped-180":
		return waylandTransformFlipped180, true
	case "flipped-270":
		return waylandTransformFlipped270, true
	default:
		return waylandTransformNormal, false
	}
}

func waylandTransformRotatesDimensions(transform int) bool {
	switch transform {
	case waylandTransform90, waylandTransform270,
		waylandTransformFlipped90, waylandTransformFlipped270:
		return true
	default:
		return false
	}
}
