package adaptivesizer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Simhash (verified against Rust reference) ----------

func TestSimhash_EmptyString(t *testing.T) {
	// md5("")[:16] = "d41d8cd98f00b204"
	assert.Equal(t, uint64(0xd41d8cd98f00b204), Simhash(""))
}

func TestSimhash_SingleChar(t *testing.T) {
	// md5("a")[:16] = "0cc175b9c0f1b6a8"
	assert.Equal(t, uint64(0x0cc175b9c0f1b6a8), Simhash("a"))
}

func TestSimhash_ShortStrings(t *testing.T) {
	// For n <= 3, single iteration; fp = md5(text)[:16] as u64.
	assert.Equal(t, uint64(0x187ef4436122d1cc), Simhash("ab"))
	assert.Equal(t, uint64(0x900150983cd24fb0), Simhash("abc"))
}

func TestSimhash_FourChars(t *testing.T) {
	// n=4: max(1, 4-3)=1, single iteration on full string.
	assert.Equal(t, uint64(0xe2fc714c4727ee93), Simhash("abcd"))
}

func TestSimhash_MultiWindow(t *testing.T) {
	// n>=5: bit voting from multiple grams.
	assert.Equal(t, uint64(0x0209020130100020), Simhash("hello"))
	assert.Equal(t, uint64(0x4681260120120222), Simhash("hello world"))
}

func TestSimhash_Unicode(t *testing.T) {
	// "cafe" is 4 codepoints -- should iterate once on the full string.
	assert.Equal(t, uint64(0x07117fe4a1ebd544), Simhash("café"))
}

func TestSimhash_Lowercasing(t *testing.T) {
	assert.Equal(t, Simhash("abc"), Simhash("ABC"))
	assert.Equal(t, Simhash("hello"), Simhash("Hello"))
}

func TestSimhash_Deterministic(t *testing.T) {
	for i := 0; i < 10; i++ {
		assert.Equal(t, Simhash("hello world"), Simhash("hello world"))
	}
}

func TestSimhash_DifferentInputs(t *testing.T) {
	assert.NotEqual(t, Simhash("hello"), Simhash("world"))
}

func TestSimhash_LongerText(t *testing.T) {
	assert.Equal(t, uint64(0x30875e2639b3cb98), Simhash("The quick brown fox jumps"))
}

func TestSimhash_SimilarInputs(t *testing.T) {
	// Similar strings should have low Hamming distance.
	a := Simhash("the cat sat on the mat")
	b := Simhash("the cat sat on a mat")
	dist := HammingDistance(a, b)
	assert.Less(t, dist, 20, "similar strings should have low Hamming distance")
}

// ---------- HammingDistance ----------

func TestHammingDistance_Same(t *testing.T) {
	assert.Equal(t, 0, HammingDistance(0, 0))
	assert.Equal(t, 0, HammingDistance(0xff, 0xff))
}

func TestHammingDistance_OneBit(t *testing.T) {
	assert.Equal(t, 1, HammingDistance(0b0000, 0b0001))
}

func TestHammingDistance_FourBits(t *testing.T) {
	assert.Equal(t, 4, HammingDistance(0b0000, 0b1111))
	assert.Equal(t, 4, HammingDistance(0b1010, 0b0101))
}

func TestHammingDistance_TwoBits(t *testing.T) {
	assert.Equal(t, 2, HammingDistance(0b1100, 0b1010))
}

func TestHammingDistance_Full64(t *testing.T) {
	assert.Equal(t, 64, HammingDistance(^uint64(0), 0))
}

// ---------- CountUniqueSimhash ----------

func TestCountUniqueSimhash_Empty(t *testing.T) {
	assert.Equal(t, 0, CountUniqueSimhash(nil, 3))
}

func TestCountUniqueSimhash_AllIdentical(t *testing.T) {
	items := []string{"abc", "abc", "abc"}
	assert.Equal(t, 1, CountUniqueSimhash(items, 3))
}

func TestCountUniqueSimhash_DiverseItems(t *testing.T) {
	items := []string{
		"the cat sat on the mat",
		"the dog ran in the park",
		"a fish swam in the sea",
	}
	assert.Equal(t, 3, CountUniqueSimhash(items, 3))
}

func TestCountUniqueSimhash_ThresholdZero(t *testing.T) {
	// Same fingerprint -- distance 0, still clusters at threshold 0.
	items := []string{"abc", "abc"}
	assert.Equal(t, 1, CountUniqueSimhash(items, 0))
}

// ---------- FindKnee ----------

func TestFindKnee_TooShort(t *testing.T) {
	assert.Nil(t, FindKnee(nil))
	assert.Nil(t, FindKnee([]int{1}))
	assert.Nil(t, FindKnee([]int{1, 2}))
}

func TestFindKnee_FlatCurve(t *testing.T) {
	result := FindKnee([]int{5, 5, 5, 5, 5})
	require.NotNil(t, result)
	assert.Equal(t, 1, *result)
}

func TestFindKnee_ConcaveCurve(t *testing.T) {
	// Reference: Rust test [1,5,8,9,10,10,10,10,10] -> Some(3)
	result := FindKnee([]int{1, 5, 8, 9, 10, 10, 10, 10, 10})
	require.NotNil(t, result)
	assert.Equal(t, 3, *result)
}

func TestFindKnee_Linear(t *testing.T) {
	// Diagonal curve: max_diff = 0 < 0.05 -> nil.
	assert.Nil(t, FindKnee([]int{1, 2, 3, 4, 5, 6, 7, 8, 9}))
}

// ---------- ComputeUniqueBigramCurve ----------

func TestBigramCurve_DistinctWords(t *testing.T) {
	items := []string{"the cat", "the dog", "a fish"}
	assert.Equal(t, []int{1, 2, 3}, ComputeUniqueBigramCurve(items))
}

func TestBigramCurve_SingleWordDedup(t *testing.T) {
	// ["hello", "world", "hello"] -> [1, 2, 2]
	items := []string{"hello", "world", "hello"}
	assert.Equal(t, []int{1, 2, 2}, ComputeUniqueBigramCurve(items))
}

func TestBigramCurve_EmptyStringContributesOne(t *testing.T) {
	// "" -> ("", ""), "a" -> ("a", ""), "a b" -> ("a", "b")
	items := []string{"", "a", "a b"}
	assert.Equal(t, []int{1, 2, 3}, ComputeUniqueBigramCurve(items))
}

func TestBigramCurve_LowercasesForDedup(t *testing.T) {
	items := []string{"Hello", "hello"}
	assert.Equal(t, []int{1, 1}, ComputeUniqueBigramCurve(items))
}

func TestBigramCurve_MultiWordBigrams(t *testing.T) {
	// "a b c" produces bigrams (a,b) and (b,c) = 2 unique bigrams.
	items := []string{"a b c"}
	assert.Equal(t, []int{2}, ComputeUniqueBigramCurve(items))
}

// ---------- ValidateWithZlib ----------

func TestValidateZlib_PassthroughWhenKAtMax(t *testing.T) {
	items := []string{"a", "b", "c"}
	assert.Equal(t, 3, ValidateWithZlib(items, 3, 10, 0.15))
}

func TestValidateZlib_PassthroughWhenTotalTooSmall(t *testing.T) {
	items := []string{"short", "short", "short", "short", "short"}
	assert.Equal(t, 2, ValidateWithZlib(items, 2, 100, 0.15))
}

func TestValidateZlib_BumpsKWhenSubsetUndercompresses(t *testing.T) {
	// 20 identical lines: longer redundant text compresses better per byte.
	// Validator sees ratio_diff > 0.15, bumps k by 20%.
	items := make([]string, 20)
	for i := range items {
		items[i] = "the quick brown fox jumps over the lazy dog"
	}
	result := ValidateWithZlib(items, 5, 100, 0.15)
	assert.Equal(t, 6, result, "expected 1.2x bump from 5 to 6")
}

func TestValidateZlib_PassthroughWhenSubsetRepresentative(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = fmt.Sprintf("entry id=%d payload=item value with content for item number %d", i, i)
	}
	result := ValidateWithZlib(items, 10, 100, 0.15)
	assert.Equal(t, 10, result, "expected passthrough for representative subset")
}

// ---------- ComputeOptimalK ----------

func TestComputeOptimalK_NLE8(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	assert.Equal(t, 5, ComputeOptimalK(items, 1.0, 3, nil))
}

func TestComputeOptimalK_LowDiversity(t *testing.T) {
	// 10 identical -> unique=1 -> max(min_k=3, 1) = 3.
	items := make([]string, 10)
	for i := range items {
		items[i] = "abc"
	}
	assert.Equal(t, 3, ComputeOptimalK(items, 1.0, 3, nil))
}

func TestComputeOptimalK_AllUnique(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = fmt.Sprintf("unique item number %d with some long content", i)
	}
	assert.Equal(t, 20, ComputeOptimalK(items, 1.0, 3, nil))
}

func TestComputeOptimalK_RespectsMaxK(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = fmt.Sprintf("item %d", i)
	}
	maxK := 10
	k := ComputeOptimalK(items, 1.0, 3, &maxK)
	assert.LessOrEqual(t, k, 10, "k should be <= max_k=10")
}

func TestComputeOptimalK_RespectsMinK(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = "abc"
	}
	k := ComputeOptimalK(items, 1.0, 5, nil)
	assert.Equal(t, 5, k)
}

func TestComputeOptimalK_BiasKeepsMore(t *testing.T) {
	items := make([]string, 30)
	for i := range items {
		items[i] = fmt.Sprintf("item content %d", i)
	}
	kLow := ComputeOptimalK(items, 0.7, 3, nil)
	kMid := ComputeOptimalK(items, 1.0, 3, nil)
	kHigh := ComputeOptimalK(items, 1.5, 3, nil)
	assert.LessOrEqual(t, kLow, kMid, "bias 0.7 -> %d should be <= bias 1.0 -> %d", kLow, kMid)
	assert.LessOrEqual(t, kMid, kHigh, "bias 1.0 -> %d should be <= bias 1.5 -> %d", kMid, kHigh)
}
