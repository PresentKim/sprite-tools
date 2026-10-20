package common

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// LoadPNG: PNG 파일을 메모리로 읽어 디코딩합니다. 파일 핸들은 즉시 닫히므로 같은 경로에 바로 덮어쓸 수 있습니다.
func LoadPNG(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("PNG 파일이 아니거나 손상됨: %v", err)
	}
	return img, nil
}

// SavePNG: 이미지를 임시 파일에 먼저 기록한 뒤 교체하여, 실패 시 기존 파일이 손상되지 않도록 합니다.
func SavePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return WriteFileAtomic(path, buf.Bytes())
}

// WriteFileAtomic: 같은 디렉터리의 임시 파일에 쓴 뒤 rename 하여 원자적으로 저장합니다.
func WriteFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sprite-tools-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ToNRGBA: 이미지를 경계가 (0,0)에서 시작하는 *image.NRGBA 로 바꿉니다.
// 이미 그런 NRGBA 면 복사 없이 그대로 돌려주므로, 결과를 수정하면 원본도 바뀝니다(읽기 전용으로 쓰세요).
// 반환값은 항상 행 사이 여백이 없는(Stride == 4*너비) 형태라 Pix 를 픽셀 순서대로 바로 훑을 수 있습니다.
func ToNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) && n.Stride == 4*n.Rect.Dx() {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// PackNRGBA: 8비트 NRGBA 값을 맵 키로 쓸 수 있는 uint32 하나로 묶습니다.
func PackNRGBA(r, g, b, a uint8) uint32 {
	return uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a)
}

// UnpackNRGBA: PackNRGBA 의 역변환입니다.
func UnpackNRGBA(k uint32) (r, g, b, a uint8) {
	return uint8(k >> 24), uint8(k >> 16), uint8(k >> 8), uint8(k)
}

// PackedToOKLab: 묶인 NRGBA 값을 OKLab 으로 변환합니다(알파 제외, ColorToOKLab 과 같은 결과).
func PackedToOKLab(k uint32) OKLab {
	r, g, b, _ := UnpackNRGBA(k)
	return RGBToOKLab(uint32(r)<<8, uint32(g)<<8, uint32(b)<<8)
}
