package common

import (
	"bytes"
	"fmt"
	"image"
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
