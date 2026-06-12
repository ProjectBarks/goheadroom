package transforms

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolPairIndicesSimple(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1"}}},
		{Role: "tool", Content: "result", ToolCallID: "call_1"},
	}
	pairs := ToolPairIndices(messages)
	assert.Equal(t, 1, len(pairs))
	assert.Equal(t, ToolPair{AssistantIdx: 1, ToolIdx: 2}, pairs[0])
}

func TestToolPairIndicesMultiple(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1"}, {ID: "call_2"}}},
		{Role: "tool", Content: "result1", ToolCallID: "call_1"},
		{Role: "tool", Content: "result2", ToolCallID: "call_2"},
		{Role: "assistant", Content: "done"},
	}
	pairs := ToolPairIndices(messages)
	assert.Equal(t, 2, len(pairs))
}

func TestToolPairIndicesNoTools(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi back"},
	}
	pairs := ToolPairIndices(messages)
	assert.Equal(t, 0, len(pairs))
}

func TestToolPairIndicesUnmatched(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1"}}},
		// No matching tool response
		{Role: "assistant", Content: "continuing"},
	}
	pairs := ToolPairIndices(messages)
	assert.Equal(t, 0, len(pairs))
}

func TestToolPairIndicesInterleavedConversation(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "search for X"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1"}}},
		{Role: "tool", Content: "results for X", ToolCallID: "call_1"},
		{Role: "assistant", Content: "Found results. Let me also check Y."},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_2"}}},
		{Role: "tool", Content: "results for Y", ToolCallID: "call_2"},
		{Role: "assistant", Content: "Here are both results."},
	}
	pairs := ToolPairIndices(messages)
	assert.Equal(t, 2, len(pairs))
	assert.Equal(t, ToolPair{AssistantIdx: 1, ToolIdx: 2}, pairs[0])
	assert.Equal(t, ToolPair{AssistantIdx: 4, ToolIdx: 5}, pairs[1])
}
