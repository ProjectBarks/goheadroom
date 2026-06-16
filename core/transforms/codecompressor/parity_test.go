package codecompressor_test

import (
	"path/filepath"
	"testing"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/parity/comparators"
)

func TestCodeCompressorParity(t *testing.T) {
	parity.RunFixtures(t,
		filepath.Join("..", "..", "testdata", "parity", "code_compressor"),
		comparators.CodeCompressor{}, parity.WithMinFixtures(12))
}
