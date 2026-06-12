package smartcrusher

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Mean returns the arithmetic mean. Returns (0, false) on empty input
// or non-finite result.
func Mean(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	m := sum / float64(len(values))
	if math.IsInf(m, 0) || math.IsNaN(m) {
		return 0, false
	}
	return m, true
}

// SampleVariance returns the sample variance (n-1 denominator).
// Requires at least 2 values.
func SampleVariance(values []float64) (float64, bool) {
	if len(values) < 2 {
		return 0, false
	}
	m, ok := Mean(values)
	if !ok {
		return 0, false
	}
	sumSqDiff := 0.0
	for _, v := range values {
		d := v - m
		sumSqDiff += d * d
	}
	variance := sumSqDiff / float64(len(values)-1)
	if math.IsInf(variance, 0) || math.IsNaN(variance) {
		return 0, false
	}
	return variance, true
}

// SampleStdev returns the sample standard deviation.
func SampleStdev(values []float64) (float64, bool) {
	v, ok := SampleVariance(values)
	if !ok {
		return 0, false
	}
	return math.Sqrt(v), true
}

// Median returns the median value.
func Median(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		lo := sorted[n/2-1]
		hi := sorted[n/2]
		return (lo + hi) / 2.0, true
	}
	return sorted[n/2], true
}

// FormatG formats a float like Python's f"{x:.4g}" -- 4 significant digits.
func FormatG(x float64) string {
	if math.IsNaN(x) {
		return "nan"
	}
	if math.IsInf(x, 1) {
		return "inf"
	}
	if math.IsInf(x, -1) {
		return "-inf"
	}
	if x == 0 {
		return "0"
	}

	abs := math.Abs(x)
	exp := int(math.Floor(math.Log10(abs)))

	if exp < -4 || exp >= 4 {
		s := fmt.Sprintf("%.3e", x)
		return normalizeScientificExp(s)
	}

	digitsAfter := 3 - exp
	if digitsAfter < 0 {
		digitsAfter = 0
	}
	s := fmt.Sprintf("%.*f", digitsAfter, x)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

func normalizeScientificExp(s string) string {
	epos := strings.IndexByte(s, 'e')
	if epos < 0 {
		return s
	}
	mantissa := s[:epos]
	expPart := s[epos+1:]

	expNum := 0
	neg := false
	ep := expPart
	if len(ep) > 0 && (ep[0] == '+' || ep[0] == '-') {
		if ep[0] == '-' {
			neg = true
		}
		ep = ep[1:]
	}
	for _, c := range ep {
		expNum = expNum*10 + int(c-'0')
	}
	if neg {
		expNum = -expNum
	}

	if strings.Contains(mantissa, ".") {
		mantissa = strings.TrimRight(mantissa, "0")
		mantissa = strings.TrimRight(mantissa, ".")
	}

	sign := "+"
	absExp := expNum
	if expNum < 0 {
		sign = "-"
		absExp = -expNum
	}
	return fmt.Sprintf("%se%s%02d", mantissa, sign, absExp)
}
