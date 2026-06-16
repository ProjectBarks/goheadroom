package jsoncompressor_test

import (
	"path/filepath"
	"testing"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/parity/comparators"
)

func TestJSONCompressorParity(t *testing.T) {
	parity.RunFixtures(t,
		filepath.Join("..", "..", "testdata", "parity", "json_compressor"),
		comparators.JSONCompressor{}, parity.WithMinFixtures(10))
}
