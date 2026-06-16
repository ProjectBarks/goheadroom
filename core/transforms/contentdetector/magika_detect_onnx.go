//go:build onnx

package contentdetector

import (
	"sync"
)

var (
	_scannerOnce sync.Once
	_scannerErr  error
)

// MagikaDetect uses the Magika ONNX model to detect content type.
// modelPath should be the directory containing the Magika assets.
// NOTE: Full Magika Go integration pending package API confirmation.
func MagikaDetect(text string, modelPath string) (ContentType, error) {
	// TODO: Wire up actual Magika scanner once the Go package API is confirmed.
	// The github.com/google/magika/go package is available but its exact API
	// for accepting raw text strings needs further investigation.
	_ = text
	_ = modelPath
	_scannerOnce.Do(func() {
		_scannerErr = &MagikaDetectorError{
			Kind:    "not_implemented",
			Message: "Magika ONNX integration pending — wire up github.com/google/magika/go",
		}
	})
	return PlainText, _scannerErr
}

// MagikaAvailable reports whether the Magika ONNX runtime is compiled in.
func MagikaAvailable() bool { return true }
