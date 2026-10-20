package processor

import (
	"fmt"
	"image"
	"image/color"

	"sprite-tools/pkg/common"
)

// Index: 파일 단위 IndexImage.
func Index(inputPath, outputPath string) (string, error) {
	return convertFile(inputPath, outputPath, IndexImage)
}

// IndexImage: 이미지를 인덱스(팔레트) 이미지로 변환하고 0번 색상을 투명(#ffffff00)으로 고정합니다.
// 색상 순서는 이미지에 처음 등장한 순서를 따르므로 실행할 때마다 같은 결과가 나옵니다.
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
			continue // 모든 완전 투명 픽셀은 0번 인덱스로 통일
		}
		key := common.PackNRGBA(p[0], p[1], p[2], p[3])
		if !hasLast || key != lastKey {
			idx, ok := index[key]
			if !ok {
				if len(pal) >= 256 {
					return nil, "", fmt.Errorf("색상이 255개를 넘어 팔레트로 만들 수 없습니다. 먼저 reduce 나 recolor 로 색상을 줄이세요")
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
	return dst, fmt.Sprintf("팔레트 %d색, 0번 투명", len(pal)), nil
}
