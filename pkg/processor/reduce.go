package processor

import (
	"image"
	"slices"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// reduceData: reduce 에 필요한 색상 목록과 픽셀별 색상 번호입니다.
// 색상 수집/정렬/OKLab 변환은 한 번만 하고, 군집화(cluster)만 기준값마다 반복할 수 있습니다.
type reduceData struct {
	bounds         image.Rectangle
	pixEntry       []int32 // 픽셀별 색상 번호, 완전 투명이면 -1
	keys           []uint32
	labs           []common.OKLab
	order          []int // 많이 쓰인 색상 순서(동률이면 먼저 등장한 순서)
	hasTransparent bool
}

func newReduceData(src image.Image) *reduceData {
	bounds := src.Bounds()
	n := common.ToNRGBA(src)
	d := &reduceData{bounds: bounds, pixEntry: make([]int32, bounds.Dx()*bounds.Dy())}

	index := make(map[uint32]int32)
	var counts []int
	pix := n.Pix
	lastKey, lastIdx := uint32(0), int32(-1)
	for i := range d.pixEntry {
		p := pix[i*4 : i*4+4 : i*4+4]
		if p[3] == 0 {
			d.hasTransparent = true
			d.pixEntry[i] = -1
			continue
		}
		key := common.PackNRGBA(p[0], p[1], p[2], p[3])
		if lastIdx < 0 || key != lastKey {
			idx, ok := index[key]
			if !ok {
				idx = int32(len(d.keys))
				index[key] = idx
				d.keys = append(d.keys, key)
				counts = append(counts, 0)
			}
			lastKey, lastIdx = key, idx
		}
		counts[lastIdx]++
		d.pixEntry[i] = lastIdx
	}

	d.labs = make([]common.OKLab, len(d.keys))
	d.order = make([]int, len(d.keys))
	for i, k := range d.keys {
		d.labs[i] = common.PackedToOKLab(k)
		d.order[i] = i
	}
	slices.SortStableFunc(d.order, func(a, b int) int {
		return counts[b] - counts[a]
	})
	return d
}

// cluster: 많이 쓰인 색상부터 대표로 삼아, 기준값 미만으로 가까운 색상을 대표에 합칩니다.
// 색상 번호별 대표 색상 번호와 대표 개수를 반환합니다.
func (d *reduceData) cluster(threshold float64) (repOf []int, repCount int) {
	repOf = make([]int, len(d.keys))
	var repLabs []common.OKLab
	var repIdx []int
	for _, i := range d.order {
		lab := d.labs[i]
		matched := false
		for j, rl := range repLabs {
			if common.DeltaOKLabWeighted(lab, rl) < threshold {
				repOf[i] = repIdx[j]
				matched = true
				break
			}
		}
		if !matched {
			repLabs = append(repLabs, lab)
			repIdx = append(repIdx, i)
			repOf[i] = i
		}
	}
	return repOf, len(repLabs)
}

// apply: 군집화 결과대로 색상을 바꾼 이미지를 만듭니다. 완전 투명 픽셀은 (0,0,0,0) 그대로입니다.
func (d *reduceData) apply(repOf []int) *image.NRGBA {
	dst := image.NewNRGBA(d.bounds)
	colors := make([][4]uint8, len(d.keys))
	for i := range colors {
		r, g, b, a := common.UnpackNRGBA(d.keys[repOf[i]])
		colors[i] = [4]uint8{r, g, b, a}
	}
	for i, e := range d.pixEntry {
		if e >= 0 {
			copy(dst.Pix[i*4:i*4+4], colors[e][:])
		}
	}
	return dst
}

// ReduceImage: 비슷한 색상(OKLab 가중 거리 < threshold)을 하나의 대표 색상으로 합칩니다.
// 많이 쓰인 색상부터 대표로 선택하므로 결과가 픽셀 순서에 좌우되지 않습니다.
// 합칠 색상이 없으면 Skip 에러를 반환합니다.
func ReduceImage(src image.Image, threshold float64) (image.Image, string, error) {
	d := newReduceData(src)
	repOf, repCount := d.cluster(threshold)

	origCount, finalCount := len(d.keys), repCount
	if d.hasTransparent {
		origCount++
		finalCount++
	}
	if origCount == finalCount {
		return nil, "", common.Skip("skip.reduce.nothing", origCount)
	}
	return d.apply(repOf), i18n.T("msg.reduce.result", origCount, finalCount), nil
}

// ReduceToCount: 불투명 색상이 maxColors 개 이하가 되는 가장 작은 기준값을 이분 탐색으로 찾아 ReduceImage 와 같은 방식으로 합칩니다.
// 이미 maxColors 개 이하면 Skip 에러를 반환합니다.
func ReduceToCount(src image.Image, maxColors int) (image.Image, string, error) {
	if maxColors < 1 {
		return nil, "", i18n.Errorf("err.reduce.max_colors", maxColors)
	}
	d := newReduceData(src)
	orig := len(d.keys)
	if orig <= maxColors {
		return nil, "", common.Skip("skip.reduce.already", orig, maxColors)
	}

	// 상한에서도 목표를 못 맞추면 기준값을 크게 늘려 한 번 더 시도합니다.
	lo, hi := 0.001, 1.5
	if _, n := d.cluster(hi); n > maxColors {
		hi = 8.0
		if _, n := d.cluster(hi); n > maxColors {
			return nil, "", i18n.Errorf("err.reduce.cannot", maxColors)
		}
	}
	for i := 0; i < 20; i++ {
		mid := (lo + hi) / 2
		if _, n := d.cluster(mid); n <= maxColors {
			hi = mid
		} else {
			lo = mid
		}
	}
	repOf, repCount := d.cluster(hi)
	return d.apply(repOf), i18n.T("msg.reduce.result_max", orig, repCount, hi), nil
}

// ReduceMax: 파일 단위 ReduceToCount.
func ReduceMax(inputPath, outputPath string, maxColors int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return ReduceToCount(src, maxColors)
	})
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
