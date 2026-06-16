package diffcompressor_test

import (
	"path/filepath"
	"testing"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/parity/comparators"
)

func TestDiffCompressorParity(t *testing.T) {
	parity.RunFixtures(t,
		filepath.Join("..", "..", "testdata", "parity", "diff_compressor"),
		comparators.DiffCompressor{}, parity.WithMinFixtures(27))
}
