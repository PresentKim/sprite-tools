// Command icongen builds every app icon from the single 16x16 master logo (docs/logo/logo-16.png).
//
// Run it from the repository root after editing the master:
//
//	go run ./tools/icongen
//
// It only ever scales by whole numbers (nearest neighbour) so the pixel art stays crisp at every size, and writes:
//   - gui/build/appicon.png           1024x1024, used by Wails
//   - gui/build/windows/icon.ico      16/32/48/64/128/256 px PNG entries, embedded in the .exe
//   - gui/frontend/src/assets/logo.png 16x16, shown in the app's top bar (scaled with CSS, pixelated)
//   - docs/logo/logo-256.png          256x256, for the README
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	master, err := loadPNG(filepath.Join(*root, "docs/logo/logo-16.png"))
	if err != nil {
		fail(err)
	}
	if b := master.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		fail(fmt.Errorf("master logo must be 16x16, got %dx%d", b.Dx(), b.Dy()))
	}

	write := func(rel string, data []byte) {
		p := filepath.Join(*root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", rel)
	}

	write("gui/build/appicon.png", encode(scale(master, 64)))
	write("gui/frontend/src/assets/logo.png", encode(scale(master, 1)))
	write("docs/logo/logo-256.png", encode(scale(master, 16)))

	ico, err := buildICO(master, []int{1, 2, 3, 4, 8, 16})
	if err != nil {
		fail(err)
	}
	write("gui/build/windows/icon.ico", ico)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "icongen:", err)
	os.Exit(1)
}

func loadPNG(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out, nil
}

// scale enlarges src by an integer factor with nearest-neighbour sampling.
func scale(src *image.NRGBA, k int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx()*k, b.Dy()*k))
	for y := 0; y < b.Dy()*k; y++ {
		for x := 0; x < b.Dx()*k; x++ {
			dst.SetNRGBA(x, y, src.NRGBAAt(x/k, y/k))
		}
	}
	return dst
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		fail(err)
	}
	return buf.Bytes()
}

// buildICO writes an .ico file whose entries are PNG images (supported since Windows Vista) at the given factors.
func buildICO(master *image.NRGBA, factors []int) ([]byte, error) {
	type entry struct {
		size int
		data []byte
	}
	var entries []entry
	for _, k := range factors {
		img := scale(master, k)
		entries = append(entries, entry{img.Bounds().Dx(), encode(img)})
	}

	var buf bytes.Buffer
	le := binary.LittleEndian
	// ICONDIR
	binary.Write(&buf, le, uint16(0))            // reserved
	binary.Write(&buf, le, uint16(1))            // type: icon
	binary.Write(&buf, le, uint16(len(entries))) // count
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0 // 0 means 256
		}
		buf.WriteByte(dim)                          // width
		buf.WriteByte(dim)                          // height
		buf.WriteByte(0)                            // palette colors
		buf.WriteByte(0)                            // reserved
		binary.Write(&buf, le, uint16(1))           // color planes
		binary.Write(&buf, le, uint16(32))          // bits per pixel
		binary.Write(&buf, le, uint32(len(e.data))) // size of the image data
		binary.Write(&buf, le, uint32(offset))      // offset of the image data
		offset += len(e.data)
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return buf.Bytes(), nil
}
