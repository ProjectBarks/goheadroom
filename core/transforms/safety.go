package transforms

// ToolCall represents an assistant tool invocation.
type ToolCall struct {
	ID       string
	Type     string
	Function string
}

// Message represents a conversation message.
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

// ToolPair holds indices of a matched assistant tool_call and its tool response.
type ToolPair struct {
	AssistantIdx int
	ToolIdx      int
}

// ToolPairIndices finds matching pairs of assistant tool_calls and tool responses.
// Port of Rust tool_pair_indices().
// Matches OpenAI shape: assistant.ToolCalls[].ID <-> tool.ToolCallID.
func ToolPairIndices(messages []Message) []ToolPair {
	// Map from tool_call ID -> assistant index that announced it.
	announced := make(map[string]int)
	for i, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" {
				announced[tc.ID] = i
			}
		}
	}

	var pairs []ToolPair
	seen := make(map[[2]int]bool)

	for i, msg := range messages {
		if msg.Role != "tool" || msg.ToolCallID == "" {
			continue
		}
		assistantIdx, ok := announced[msg.ToolCallID]
		if !ok {
			continue
		}
		key := [2]int{assistantIdx, i}
		if seen[key] {
			continue
		}
		seen[key] = true
		pairs = append(pairs, ToolPair{
			AssistantIdx: assistantIdx,
			ToolIdx:      i,
		})
	}

	return pairs
}
