package processor

import (
	"fmt"
	"image"
	"image/color"
	"slices"

	"sprite-tools/pkg/common"
)

// Simplify: 비슷한 색상(OKLab 가중 거리 < threshold)을 하나의 대표 색상으로 합칩니다.
// 많이 쓰인 색상부터 대표로 선택하므로 결과가 픽셀 순서에 좌우되지 않습니다.
func Simplify(inputPath, outputPath string, threshold float64) (string, error) {
	src, err := common.LoadPNG(inputPath)
	if err != nil {
		return "", err
	}
	bounds := src.Bounds()

	type entry struct {
		c            color.Color
		count, first int
	}
	index := make(map[color.Color]int)
	var entries []entry
	hasTransparent := false

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := src.At(x, y)
			if common.Alpha8(c) == 0 {
				hasTransparent = true
				continue
			}
			if i, ok := index[c]; ok {
				entries[i].count++
			} else {
				index[c] = len(entries)
				entries = append(entries, entry{c, 1, len(entries)})
			}
		}
	}

	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return entries[b].count - entries[a].count
	})

	type rep struct {
		oklab common.OKLab
		raw   color.Color
	}
	var reps []rep
	mapping := make(map[color.Color]color.Color, len(entries))

	for _, i := range order {
		c := entries[i].c
		lab := common.ColorToOKLab(c)
		matched := false
		for _, r := range reps {
			if common.DeltaOKLabWeighted(lab, r.oklab) < threshold {
				mapping[c] = r.raw
				matched = true
				break
			}
		}
		if !matched {
			reps = append(reps, rep{lab, c})
			mapping[c] = c
		}
	}

	origCount, finalCount := len(entries), len(reps)
	if hasTransparent {
		origCount++
		finalCount++
	}
	if origCount == finalCount {
		return "", common.Skip("합칠 색상 없음, 색상 %d개", origCount)
	}

	dst := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := src.At(x, y)
			if common.Alpha8(c) == 0 {
				continue
			}
			dst.Set(x, y, mapping[c])
		}
	}

	if err := common.SavePNG(outputPath, dst); err != nil {
		return "", err
	}
	return fmt.Sprintf("색상 %d → %d", origCount, finalCount), nil
}
