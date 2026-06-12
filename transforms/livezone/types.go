package livezone

import (
	"errors"

	"github.com/uber/goheadroom/authmode"
)

const DefaultModel = "claude-3-5-sonnet-20241022"

// BlockAction describes what happened to a message block.
type BlockAction int

const (
	BlockActionCompressed BlockAction = iota
	BlockActionExcluded
	BlockActionPassthrough
)

func (a BlockAction) String() string {
	switch a {
	case BlockActionCompressed:
		return "Compressed"
	case BlockActionExcluded:
		return "Excluded"
	case BlockActionPassthrough:
		return "Passthrough"
	default:
		return "Unknown"
	}
}

// ExclusionReason explains why a block was not compressed.
type ExclusionReason int

const (
	ExclusionReasonSystemMessage ExclusionReason = iota
	ExclusionReasonToolDefinition
	ExclusionReasonFrozenByPolicy
	ExclusionReasonCacheControl
	ExclusionReasonTooSmall
	ExclusionReasonLatestAssistant
	ExclusionReasonLatestUser
)

func (r ExclusionReason) String() string {
	switch r {
	case ExclusionReasonSystemMessage:
		return "SystemMessage"
	case ExclusionReasonToolDefinition:
		return "ToolDefinition"
	case ExclusionReasonFrozenByPolicy:
		return "FrozenByPolicy"
	case ExclusionReasonCacheControl:
		return "CacheControl"
	case ExclusionReasonTooSmall:
		return "TooSmall"
	case ExclusionReasonLatestAssistant:
		return "LatestAssistant"
	case ExclusionReasonLatestUser:
		return "LatestUser"
	default:
		return "Unknown"
	}
}

// BlockOutcome records what happened to a single message block.
type BlockOutcome struct {
	Index            int              `json:"index"`
	Action           BlockAction      `json:"action"`
	ExclusionReason  *ExclusionReason `json:"exclusion_reason,omitempty"`
	Strategy         string           `json:"strategy,omitempty"`
	OriginalTokens   int              `json:"original_tokens"`
	CompressedTokens int              `json:"compressed_tokens,omitempty"`
}

// CompressionManifest tracks per-block outcomes and aggregate token counts.
type CompressionManifest struct {
	Blocks                []BlockOutcome   `json:"blocks"`
	TotalOriginalTokens   int              `json:"total_original_tokens"`
	TotalCompressedTokens int              `json:"total_compressed_tokens"`
	Model                 string           `json:"model"`
	AuthMode              authmode.AuthMode `json:"auth_mode"`
}

// OutcomeKind discriminates LiveZoneOutcome variants.
type OutcomeKind int

const (
	OutcomeCompressed OutcomeKind = iota
	OutcomeNoCompression
	OutcomeError
)

// LiveZoneOutcome is the result of a live zone compression attempt.
type LiveZoneOutcome struct {
	Kind     OutcomeKind
	Body     []byte               // set when Kind == OutcomeCompressed
	Manifest *CompressionManifest // set when Kind == OutcomeCompressed
	Reason   string               // set when Kind == OutcomeNoCompression
	Err      error                // set when Kind == OutcomeError
}

// Sentinel errors.
var (
	ErrInvalidBody       = errors.New("livezone: invalid request body")
	ErrNoMessages        = errors.New("livezone: no messages in request")
	ErrCompressionFailed = errors.New("livezone: compression failed")
	ErrTokenizerFailed   = errors.New("livezone: tokenizer failed")
	ErrUnsupportedFormat = errors.New("livezone: unsupported format")
)
