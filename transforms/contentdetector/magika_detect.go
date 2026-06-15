//go:build !onnx

package contentdetector

// MagikaDetect is the stub implementation (no ONNX runtime).
// Always returns an error indicating ONNX is not available.
// Build with -tags=onnx for the real ONNX implementation.
func MagikaDetect(text string, modelPath string) (ContentType, error) {
	return PlainText, &MagikaDetectorError{
		Kind:    "not_available",
		Message: "ONNX runtime not available (build without onnx tag)",
	}
}

// MagikaAvailable reports whether the Magika ONNX runtime is compiled in.
func MagikaAvailable() bool { return false }
