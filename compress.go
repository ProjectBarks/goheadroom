package headroom

import (
	"github.com/uber/goheadroom/authmode"
	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/compressionpolicy"
	"github.com/uber/goheadroom/transforms/livezone"
)

// RequestFormat identifies which API format the request body uses.
type RequestFormat string

const (
	FormatAnthropic       RequestFormat = "anthropic"
	FormatOpenAIChat      RequestFormat = "openai_chat"
	FormatOpenAIResponses RequestFormat = "openai_responses"
)

// CompressRequest bundles all inputs needed for live zone compression.
type CompressRequest struct {
	Body      []byte
	Format    RequestFormat
	Mode      compressionpolicy.Mode
	AuthMode  authmode.AuthMode
	RequestID string

	// CCR fields (optional)
	EnableCCR bool
	CCRStore  ccr.CcrStore
	CCRConfig *livezone.CCRConfig
}

// CompressLiveZone is the top-level entry point for genai-api.
// It dispatches to the appropriate format-specific compression function.
func CompressLiveZone(req CompressRequest) livezone.LiveZoneOutcome {
	switch req.Format {
	case FormatAnthropic:
		if req.EnableCCR {
			return livezone.CompressAnthropicLiveZoneWithCCR(
				req.Body, req.Mode, req.AuthMode, req.RequestID,
				req.CCRStore, req.CCRConfig,
			)
		}
		return livezone.CompressAnthropicLiveZone(req.Body, req.Mode, req.AuthMode, req.RequestID)

	case FormatOpenAIChat:
		return livezone.CompressOpenAIChatLiveZone(req.Body, req.Mode, req.AuthMode, req.RequestID)

	case FormatOpenAIResponses:
		return livezone.CompressOpenAIResponsesLiveZone(req.Body, req.Mode, req.AuthMode, req.RequestID)

	default:
		return livezone.LiveZoneOutcome{
			Kind: livezone.OutcomeError,
			Err:  livezone.ErrUnsupportedFormat,
		}
	}
}
