package processor

import (
	"fmt"
	"image"
	"image/color"
	"slices"

	"sprite-tools/pkg/common"
)

// ReduceImage: 비슷한 색상(OKLab 가중 거리 < threshold)을 하나의 대표 색상으로 합칩니다.
// 많이 쓰인 색상부터 대표로 선택하므로 결과가 픽셀 순서에 좌우되지 않습니다.
// 합칠 색상이 없으면 Skip 에러를 반환합니다.
func ReduceImage(src image.Image, threshold float64) (image.Image, string, error) {
	bounds := src.Bounds()

	type entry struct {
		c     color.Color
		count int
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
				entries = append(entries, entry{c, 1})
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
		return nil, "", common.Skip("합칠 색상 없음, 색상 %d개", origCount)
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
	return dst, fmt.Sprintf("색상 %d → %d", origCount, finalCount), nil
}

// Reduce: 파일 단위 ReduceImage.
func Reduce(inputPath, outputPath string, threshold float64) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return ReduceImage(src, threshold)
	})
}

// convertFile: PNG 를 읽어 변환한 뒤 저장하는 공통 처리입니다.
func convertFile(inputPath, outputPath string, fn func(image.Image) (image.Image, string, error)) (string, error) {
	src, err := common.LoadPNG(inputPath)
	if err != nil {
		return "", err
	}
	dst, msg, err := fn(src)
	if err != nil {
		return "", err
	}
	if err := common.SavePNG(outputPath, dst); err != nil {
		return "", err
	}
	return msg, nil
}
