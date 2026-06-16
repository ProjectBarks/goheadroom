package logcompressor_test

import (
	"path/filepath"
	"testing"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/parity/comparators"
)

func TestLogCompressorParity(t *testing.T) {
	parity.RunFixtures(t,
		filepath.Join("..", "..", "testdata", "parity", "log_compressor"),
		comparators.LogCompressor{}, parity.WithMinFixtures(20))
}
