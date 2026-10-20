package processor

import (
	"image"
	"slices"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// reduceData holds the color list and per-pixel color indices that reduce needs.
// Color collection, sorting and OKLab conversion happen only once; only the clustering (cluster) can be repeated per threshold.
type reduceData struct {
	bounds         image.Rectangle
	pixEntry       []int32 // color index per pixel, -1 if fully transparent
	keys           []uint32
	labs           []common.OKLab
	order          []int // colors ordered by usage (ties broken by first appearance)
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

// cluster takes the most-used colors as representatives and merges colors closer than the threshold into them.
// It returns the representative color index and the representative count for each color index.
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

// apply builds the image with colors replaced according to the clustering result. Fully transparent pixels stay (0,0,0,0).
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

// ReduceImage merges similar colors (OKLab weighted distance < threshold) into a single representative color.
// It picks representatives starting from the most-used colors, so the result does not depend on pixel order.
// If there are no colors to merge, it returns a Skip error.
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

// ReduceToCount binary-searches for the smallest threshold that leaves at most maxColors opaque colors, then merges the same way as ReduceImage.
// If there are already maxColors or fewer, it returns a Skip error.
func ReduceToCount(src image.Image, maxColors int) (image.Image, string, error) {
	if maxColors < 1 {
		return nil, "", i18n.Errorf("err.reduce.max_colors", maxColors)
	}
	d := newReduceData(src)
	orig := len(d.keys)
	if orig <= maxColors {
		return nil, "", common.Skip("skip.reduce.already", orig, maxColors)
	}

	// If the target is still missed at the upper bound, increase the threshold substantially and try once more.
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

// ReduceMax is the file-level ReduceToCount.
func ReduceMax(inputPath, outputPath string, maxColors int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return ReduceToCount(src, maxColors)
	})
}

// Reduce is the file-level ReduceImage.
func Reduce(inputPath, outputPath string, threshold float64) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return ReduceImage(src, threshold)
	})
}

// convertFile is the common routine that reads a PNG, converts it and saves it.
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
