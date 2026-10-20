package common

import (
	"slices"
	"strings"
)

// NaturalLess is a "natural sort" order that compares digit runs by numeric value (frame-2 < frame-10).
// Case is ignored, and when numeric values are equal the one with fewer leading zeros (the shorter one) comes first.
func NaturalLess(a, b string) bool {
	return naturalCompare(a, b) < 0
}

// NaturalSort sorts a list of strings in natural order.
func NaturalSort(list []string) {
	slices.SortStableFunc(list, naturalCompare)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func naturalCompare(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(la) && j < len(lb) {
		if isDigit(la[i]) && isDigit(lb[j]) {
			si := i
			for i < len(la) && isDigit(la[i]) {
				i++
			}
			sj := j
			for j < len(lb) && isDigit(lb[j]) {
				j++
			}
			na, nb := strings.TrimLeft(la[si:i], "0"), strings.TrimLeft(lb[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			if zi, zj := i-si, j-sj; zi != zj {
				return zi - zj
			}
			continue
		}
		if la[i] != lb[j] {
			return int(la[i]) - int(lb[j])
		}
		i++
		j++
	}
	if c := (len(la) - i) - (len(lb) - j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}
