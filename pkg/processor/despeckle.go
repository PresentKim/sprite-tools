package processor

import (
	"image"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// Despeckle: 파일 단위 DespeckleImage.
func Despeckle(inputPath, outputPath string, minSize int, diagonal bool) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return DespeckleImage(src, minSize, diagonal)
	})
}

// DespeckleImage: 투명하지 않은(알파 != 0) 픽셀이 이어진 덩어리 가운데 픽셀 수가 minSize 보다 작은 것을 완전 투명(0,0,0,0)으로 지웁니다.
// diagonal 이면 대각선도 이어진 것으로 보고(8방향), 아니면 상하좌우(4방향)만 봅니다.
// 지울 덩어리가 없으면 Skip 에러를 반환합니다.
func DespeckleImage(src image.Image, minSize int, diagonal bool) (image.Image, string, error) {
	if minSize < 1 {
		return nil, "", i18n.Errorf("err.despeckle.min_size", minSize)
	}
	n := common.ToNRGBA(src)
	w, h := n.Rect.Dx(), n.Rect.Dy()
	if w == 0 || h == 0 {
		return nil, "", common.Skip("skip.despeckle.empty", w, h)
	}
	if minSize == 1 {
		return nil, "", common.Skip("skip.despeckle.min1")
	}

	// 알파가 있는 픽셀 지도 (1 = 아직 보지 않은 내용 픽셀)
	todo := make([]bool, w*h)
	for y := 0; y < h; y++ {
		row := n.Pix[y*n.Stride : y*n.Stride+w*4]
		for x := 0; x < w; x++ {
			todo[y*w+x] = row[x*4+3] != 0
		}
	}

	var dst *image.NRGBA
	comps, pixels := 0, 0
	var queue []int32 // 덩어리 하나의 픽셀 번호 (탐색 큐를 그대로 목록으로 씀)
	for start := 0; start < w*h; start++ {
		if !todo[start] {
			continue
		}
		queue = append(queue[:0], int32(start))
		todo[start] = false
		for head := 0; head < len(queue); head++ {
			i := int(queue[head])
			x, y := i%w, i/w
			for dy := -1; dy <= 1; dy++ {
				ny := y + dy
				if ny < 0 || ny >= h {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					if (dx == 0 && dy == 0) || (!diagonal && dx != 0 && dy != 0) {
						continue
					}
					nx := x + dx
					if nx < 0 || nx >= w {
						continue
					}
					if j := ny*w + nx; todo[j] {
						todo[j] = false
						queue = append(queue, int32(j))
					}
				}
			}
		}
		if len(queue) >= minSize {
			continue
		}
		if dst == nil {
			dst = &image.NRGBA{Pix: append([]byte(nil), n.Pix...), Stride: n.Stride, Rect: n.Rect}
		}
		for _, i := range queue {
			o := int(i) * 4
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = 0, 0, 0, 0
		}
		comps++
		pixels += len(queue)
	}
	if comps == 0 {
		return nil, "", common.Skip("skip.despeckle.none", minSize)
	}
	return dst, i18n.T("msg.despeckle.result", comps, pixels), nil
}
