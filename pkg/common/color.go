package common

import (
	"image/color"
	"math"
)

// OKLab color struct
type OKLab struct {
	L, A, B float64
}

// RGBToOKLab converts 16-bit (0-65535) R, G, B values to the OKLab color space.
func RGBToOKLab(r, g, b uint32) OKLab {
	rf, gf, bf := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

	// Remove gamma correction (linearize)
	fLinear := func(c float64) float64 {
		if c > 0.04045 {
			return math.Pow((c+0.055)/1.055, 2.4)
		}
		return c / 12.92
	}
	rf, gf, bf = fLinear(rf), fLinear(gf), fLinear(bf)

	// Linear RGB -> LMS space conversion
	l := 0.4122214708*rf + 0.5363325363*gf + 0.0514459929*bf
	m := 0.2119034982*rf + 0.6806995451*gf + 0.1073969566*bf
	s := 0.0883024619*rf + 0.2817188376*gf + 0.6299787005*bf

	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	return OKLab{
		L: 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_,
		A: 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_,
		B: 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757986*s_,
	}
}

// ColorToOKLab converts a color.Color directly to OKLab.
// It uses non-premultiplied RGB (ignoring alpha) so translucent colors are not computed as darker.
func ColorToOKLab(c color.Color) OKLab {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return RGBToOKLab(uint32(n.R)<<8, uint32(n.G)<<8, uint32(n.B)<<8)
}

// Alpha8 returns the color's alpha in the 0-255 range.
func Alpha8(c color.Color) uint8 {
	_, _, _, a := c.RGBA()
	return uint8(a >> 8)
}

// Chroma computes the chroma (distance from the origin) of a color in OKLab.
func (c OKLab) Chroma() float64 {
	return math.Sqrt(c.A*c.A + c.B*c.B)
}

// Hue computes the hue angle (radians) of a color in OKLab.
func (c OKLab) Hue() float64 {
	return math.Atan2(c.B, c.A)
}

// DeltaOKLabWeighted is a distance formula with penalties that prevents gray/chromatic mismatches and hue jumps.
func DeltaOKLabWeighted(c1, c2 OKLab) float64 {
	dL := c1.L - c2.L
	dA := c1.A - c2.A
	dB := c1.B - c2.B

	baseDist := math.Sqrt(dL*dL + dA*dA + dB*dB)

	chroma1 := c1.Chroma()
	chroma2 := c2.Chroma()

	// 1. Chroma penalty:
	// Add a distance penalty when the source is grayscale (very low chroma, e.g. < 0.03) but the palette color is chromatic
	chromaDiff := math.Abs(chroma1 - chroma2)
	penalty := 1.0 + chromaDiff*2.5

	// 2. Hue penalty:
	// When both colors have some chroma, add a penalty if the hue difference is large
	if chroma1 > 0.02 && chroma2 > 0.02 {
		h1 := c1.Hue()
		h2 := c2.Hue()
		hDiff := math.Abs(h1 - h2)
		if hDiff > math.Pi {
			hDiff = 2*math.Pi - hDiff
		}
		// Add in proportion to the hue difference (0 ~ Pi)
		penalty += (hDiff / math.Pi) * 1.5
	}

	return baseDist * penalty
}

// DeltaOKLab is the plain Euclidean distance (currently unused).
func DeltaOKLab(c1, c2 OKLab) float64 {
	dL := c1.L - c2.L
	dA := c1.A - c2.A
	dB := c1.B - c2.B
	return math.Sqrt(dL*dL + dA*dA + dB*dB)
}
