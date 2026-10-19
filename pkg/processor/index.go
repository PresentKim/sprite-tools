package processor

import (
	"fmt"
	"image"
	"image/color"

	"sprite-tools/pkg/common"
)

// Index: 이미지를 인덱스(팔레트) PNG로 변환하고 0번 색상을 투명(#ffffff00)으로 고정합니다.
// 색상 순서는 이미지에 처음 등장한 순서를 따르므로 실행할 때마다 같은 결과가 나옵니다.
func Index(inputPath, outputPath string) (string, error) {
	src, err := common.LoadPNG(inputPath)
	if err != nil {
		return "", err
	}
	bounds := src.Bounds()

	transparent := color.NRGBA{255, 255, 255, 0}
	pal := color.Palette{transparent}
	index := make(map[color.NRGBA]uint8)
	dst := image.NewPaletted(bounds, nil)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			if c.A == 0 {
				continue // 모든 완전 투명 픽셀은 0번 인덱스로 통일
			}
			i, ok := index[c]
			if !ok {
				if len(pal) >= 256 {
					return "", fmt.Errorf("색상이 255개를 넘어 팔레트로 만들 수 없습니다. 먼저 reduce 나 recolor 로 색상을 줄이세요")
				}
				i = uint8(len(pal))
				index[c] = i
				pal = append(pal, c)
			}
			dst.SetColorIndex(x, y, i)
		}
	}
	dst.Palette = pal

	if err := common.SavePNG(outputPath, dst); err != nil {
		return "", err
	}
	return fmt.Sprintf("팔레트 %d색, 0번 투명", len(pal)), nil
}
