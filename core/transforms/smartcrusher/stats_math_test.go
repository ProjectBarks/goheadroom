package smartcrusher

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func approxEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestMeanEmptyIsNone(t *testing.T) {
	_, ok := Mean(nil)
	assert.False(t, ok)
}

func TestMeanSingle(t *testing.T) {
	v, ok := Mean([]float64{5.0})
	require.True(t, ok)
	assert.True(t, approxEq(v, 5.0))
}

func TestMeanBasic(t *testing.T) {
	v, ok := Mean([]float64{1, 2, 3, 4, 5})
	require.True(t, ok)
	assert.True(t, approxEq(v, 3.0))
}

func TestSampleVarianceTooFewIsNone(t *testing.T) {
	_, ok := SampleVariance(nil)
	assert.False(t, ok)
	_, ok = SampleVariance([]float64{5.0})
	assert.False(t, ok)
}

func TestSampleVarianceUsesNMinus1(t *testing.T) {
	v, ok := SampleVariance([]float64{1, 2, 3, 4, 5})
	require.True(t, ok)
	assert.True(t, approxEq(v, 2.5), "got %f, expected 2.5", v)
}

func TestSampleStdevBasic(t *testing.T) {
	s, ok := SampleStdev([]float64{1, 2, 3, 4, 5})
	require.True(t, ok)
	assert.True(t, approxEq(s, math.Sqrt(2.5)), "got %f", s)
}

func TestSampleVarianceConstantIsZero(t *testing.T) {
	v, ok := SampleVariance([]float64{7, 7, 7})
	require.True(t, ok)
	assert.True(t, approxEq(v, 0.0))
}

func TestMeanNonFiniteOverflowReturnsNone(t *testing.T) {
	huge := math.MaxFloat64 / 2.0
	nums := []float64{huge, huge, huge, huge}
	_, ok := Mean(nums)
	assert.False(t, ok)
}

func TestSampleVarianceNonFiniteReturnsNone(t *testing.T) {
	huge := 1e200
	_, ok := SampleVariance([]float64{huge, -huge})
	assert.False(t, ok)
}

func TestSampleStdevNonFiniteReturnsNone(t *testing.T) {
	huge := 1e200
	_, ok := SampleStdev([]float64{huge, -huge})
	assert.False(t, ok)
}

func TestMedianEmptyIsNone(t *testing.T) {
	_, ok := Median(nil)
	assert.False(t, ok)
}

func TestMedianOddCount(t *testing.T) {
	v, ok := Median([]float64{3, 1, 2})
	require.True(t, ok)
	assert.Equal(t, 2.0, v)
}

func TestMedianEvenCountMeanOfMiddles(t *testing.T) {
	v, ok := Median([]float64{4, 1, 2, 3})
	require.True(t, ok)
	assert.Equal(t, 2.5, v)
}

func TestMedianSingleElement(t *testing.T) {
	v, ok := Median([]float64{42})
	require.True(t, ok)
	assert.Equal(t, 42.0, v)
}

func TestFormatGZeroAndSpecial(t *testing.T) {
	assert.Equal(t, "0", FormatG(0.0))
	assert.Equal(t, "0", FormatG(math.Copysign(0.0, -1)))
	assert.Equal(t, "nan", FormatG(math.NaN()))
	assert.Equal(t, "inf", FormatG(math.Inf(1)))
	assert.Equal(t, "-inf", FormatG(math.Inf(-1)))
}

func TestFormatGFixedRange(t *testing.T) {
	assert.Equal(t, "1.5", FormatG(1.5))
	assert.Equal(t, "1", FormatG(1.0))
	assert.Equal(t, "1234", FormatG(1234.0))
	assert.Equal(t, "0.1235", FormatG(0.123456))
}

func TestFormatGScientificRange(t *testing.T) {
	assert.Equal(t, "1.235e+04", FormatG(12345.678))
	assert.Equal(t, "1.234e-05", FormatG(0.00001234))
}

func TestFormatGNegative(t *testing.T) {
	assert.Equal(t, "-1.5", FormatG(-1.5))
	assert.Equal(t, "-1.235e+04", FormatG(-12345.678))
}
