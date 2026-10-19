package common

import (
	"image/color"
	"math"
)

// OKLab 색상 구조체
type OKLab struct {
	L, A, B float64
}

// RGBToOKLab: 16비트(0~65535) R, G, B 값을 OKLab 색상 공간으로 변환합니다.
func RGBToOKLab(r, g, b uint32) OKLab {
	rf, gf, bf := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

	// Gamma Correction 제거 (Linearize)
	fLinear := func(c float64) float64 {
		if c > 0.04045 {
			return math.Pow((c+0.055)/1.055, 2.4)
		}
		return c / 12.92
	}
	rf, gf, bf = fLinear(rf), fLinear(gf), fLinear(bf)

	// Linear RGB -> LMS 공간 변환
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

// ColorToOKLab: image/color.Color 인터페이스를 직접 받아 OKLab으로 변환합니다.
func ColorToOKLab(c color.Color) OKLab {
	r, g, b, _ := c.RGBA()
	return RGBToOKLab(r, g, b)
}

// Chroma: OKLab 상에서 색상의 채도(원점으로부터의 거리)를 계산합니다.
func (c OKLab) Chroma() float64 {
	return math.Sqrt(c.A*c.A + c.B*c.B)
}

// Hue: OKLab 상에서 색상각(라디안)을 계산합니다.
func (c OKLab) Hue() float64 {
	return math.Atan2(c.B, c.A)
}

// DeltaOKLabWeighted: 회색-유채색 간 오매칭 및 색상 튐을 방지하도록 페널티가 반영된 거리 산식입니다.
func DeltaOKLabWeighted(c1, c2 OKLab) float64 {
	dL := c1.L - c2.L
	dA := c1.A - c2.A
	dB := c1.B - c2.B

	baseDist := math.Sqrt(dL*dL + dA*dA + dB*dB)

	chroma1 := c1.Chroma()
	chroma2 := c2.Chroma()

	// 1. Chroma(채도) 페널티:
	// 원본이 회색조(Chroma가 매우 낮음,예: < 0.03)인데 팔레트 색상이 유채색인 경우 거리 페널티 부여
	chromaDiff := math.Abs(chroma1 - chroma2)
	penalty := 1.0 + chromaDiff*2.5

	// 2. Hue(색상각) 페널티:
	// 두 색상 모두 어느 정도 채도가 있을 때 색상각 차이가 크면 페널티 부여
	if chroma1 > 0.02 && chroma2 > 0.02 {
		h1 := c1.Hue()
		h2 := c2.Hue()
		hDiff := math.Abs(h1 - h2)
		if hDiff > math.Pi {
			hDiff = 2*math.Pi - hDiff
		}
		// 색상각 차이(0 ~ Pi)에 비례하여 페널티 가산
		penalty += (hDiff / math.Pi) * 1.5
	}

	return baseDist * penalty
}

// DeltaOKLab: 기본 순수 유클리드 거리 (Simplify 등에서 사용)
func DeltaOKLab(c1, c2 OKLab) float64 {
	dL := c1.L - c2.L
	dA := c1.A - c2.A
	dB := c1.B - c2.B
	return math.Sqrt(dL*dL + dA*dA + dB*dB)
}