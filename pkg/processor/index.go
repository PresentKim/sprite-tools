package processor

import (
	"image"
	"image/color"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// Index is the file-level IndexImage.
func Index(inputPath, outputPath string) (string, error) {
	return convertFile(inputPath, outputPath, IndexImage)
}

// IndexImage converts an image to an indexed (paletted) image and pins color 0 to transparent (#ffffff00).
// Colors follow the order of first appearance in the image, so every run gives the same result.
func IndexImage(src image.Image) (image.Image, string, error) {
	bounds := src.Bounds()
	n := common.ToNRGBA(src)

	transparent := color.NRGBA{255, 255, 255, 0}
	pal := color.Palette{transparent}
	index := make(map[uint32]uint8)
	dst := image.NewPaletted(bounds, nil)

	pix := n.Pix
	lastKey, lastIdx, hasLast := uint32(0), uint8(0), false
	for i := range dst.Pix {
		p := pix[i*4 : i*4+4 : i*4+4]
		if p[3] == 0 {
			continue // all fully transparent pixels are unified to index 0
		}
		key := common.PackNRGBA(p[0], p[1], p[2], p[3])
		if !hasLast || key != lastKey {
			idx, ok := index[key]
			if !ok {
				if len(pal) >= 256 {
					return nil, "", i18n.Errorf("err.index.too_many_colors")
				}
				idx = uint8(len(pal))
				index[key] = idx
				pal = append(pal, color.NRGBA{p[0], p[1], p[2], p[3]})
			}
			lastKey, lastIdx, hasLast = key, idx, true
		}
		dst.Pix[i] = lastIdx
	}
	dst.Palette = pal
	return dst, i18n.T("msg.index.result", len(pal)), nil
}
