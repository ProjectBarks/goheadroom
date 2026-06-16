package authmode

import (
	"net/http"
	"strings"
)

type AuthMode int

const (
	Payg         AuthMode = iota
	OAuth
	Subscription
)

func (m AuthMode) String() string {
	switch m {
	case Payg:
		return "payg"
	case OAuth:
		return "oauth"
	case Subscription:
		return "subscription"
	default:
		return "unknown"
	}
}

var subscriptionUAPrefixes = []string{
	"claude-cli/",
	"claude-code/",
	"codex-cli/",
	"cursor/",
	"claude-vscode/",
	"github-copilot/",
	"anthropic-cli/",
	"antigravity/",
}

func Classify(h http.Header) AuthMode {
	ua := strings.ToLower(h.Get("User-Agent"))
	for _, prefix := range subscriptionUAPrefixes {
		if strings.Contains(ua, prefix) {
			return Subscription
		}
	}

	auth := h.Get("Authorization")
	if auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			token := auth[7:]
			if strings.HasPrefix(token, "sk-ant-oat-") {
				return OAuth
			}
			if strings.HasPrefix(token, "sk-ant-api") || strings.HasPrefix(token, "sk-") {
				return Payg
			}
			if strings.Count(token, ".") == 2 {
				return OAuth
			}
			return OAuth
		}
		return OAuth
	}

	if h.Get("x-api-key") != "" {
		return Payg
	}
	if h.Get("x-goog-api-key") != "" {
		return Payg
	}

	return Payg
}
