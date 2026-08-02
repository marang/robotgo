package agent

import (
	"context"
	"image"
	"image/color"
	"math"
	"sort"
)

func detectVisualComponents(ctx context.Context, source *image.RGBA, region CaptureRegion, minimum float64, maximum int) ([]VisualElementProposal, bool, error) {
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	visited := make([]bool, width*height)
	defer clear(visited)
	background := visualBackground(source)
	foreground := func(x, y int) (bool, float64) {
		pixel := source.RGBAAt(x, y)
		delta := float64(absInt(int(pixel.R)-int(background.R)) + absInt(int(pixel.G)-int(background.G)) + absInt(int(pixel.B)-int(background.B)))
		return delta >= 48, delta / (255 * 3)
	}
	proposals := make([]VisualElementProposal, 0, maximum)
	truncated := false
	queue := make([]int, 0, 256)
	for y := 0; y < height; y++ {
		if y&63 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		for x := 0; x < width; x++ {
			index := y*width + x
			isForeground, _ := foreground(x, y)
			if visited[index] || !isForeground {
				continue
			}
			visited[index] = true
			queue = append(queue[:0], index)
			minX, maxX, minY, maxY, count, contrast := x, x, y, y, 0, 0.0
			for head := 0; head < len(queue); head++ {
				current := queue[head]
				cx, cy := current%width, current/width
				count++
				_, value := foreground(cx, cy)
				contrast += value
				minX, maxX, minY, maxY = min(minX, cx), max(maxX, cx), min(minY, cy), max(maxY, cy)
				for _, neighbor := range [][2]int{{cx - 1, cy}, {cx + 1, cy}, {cx, cy - 1}, {cx, cy + 1}} {
					nx, ny := neighbor[0], neighbor[1]
					if nx < 0 || nx >= width || ny < 0 || ny >= height {
						continue
					}
					next := ny*width + nx
					active, _ := foreground(nx, ny)
					if !visited[next] && active {
						visited[next] = true
						queue = append(queue, next)
					}
				}
			}
			boxWidth, boxHeight := maxX-minX+1, maxY-minY+1
			if count < 4 || boxWidth < 2 || boxHeight < 2 {
				continue
			}
			density := float64(count) / float64(boxWidth*boxHeight)
			confidence := math.Min(1, 0.5+(contrast/float64(count))*0.25+density*0.25)
			if confidence < minimum {
				continue
			}
			if len(proposals) >= maximum {
				truncated = true
				continue
			}
			proposals = append(proposals, VisualElementProposal{
				Kind: "visual-region",
				Bounds: CaptureRegion{X: region.X + minX, Y: region.Y + minY,
					Width: boxWidth, Height: boxHeight, DisplayID: region.DisplayID},
				Confidence: confidence,
			})
		}
	}
	clear(queue)
	sort.Slice(proposals, func(i, j int) bool {
		if proposals[i].Bounds.Y != proposals[j].Bounds.Y {
			return proposals[i].Bounds.Y < proposals[j].Bounds.Y
		}
		return proposals[i].Bounds.X < proposals[j].Bounds.X
	})
	return proposals, truncated, nil
}

func visualBackground(source *image.RGBA) color.RGBA {
	maximumX, maximumY := source.Bounds().Dx()-1, source.Bounds().Dy()-1
	corners := [...]color.RGBA{
		source.RGBAAt(0, 0), source.RGBAAt(maximumX, 0),
		source.RGBAAt(0, maximumY), source.RGBAAt(maximumX, maximumY),
	}
	best, bestDistance := corners[0], int(^uint(0)>>1)
	for _, candidate := range corners {
		distance := 0
		for _, other := range corners {
			distance += colorDistance(candidate, other)
		}
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

func colorDistance(left, right color.RGBA) int {
	return absInt(int(left.R)-int(right.R)) + absInt(int(left.G)-int(right.G)) + absInt(int(left.B)-int(right.B))
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
