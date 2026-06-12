package tokenizer

type Tokenizer interface {
	CountText(text string) int
	Backend() Backend
}

type Backend int

const (
	BackendTiktoken Backend = iota
	BackendHuggingFace
	BackendEstimator
)

func (b Backend) String() string {
	switch b {
	case BackendTiktoken:
		return "tiktoken"
	case BackendHuggingFace:
		return "huggingface"
	case BackendEstimator:
		return "estimator"
	default:
		return "unknown"
	}
}
