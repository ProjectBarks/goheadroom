package cachecontrol

import "log"

func ComputeFrozenCount(parsed map[string]interface{}) int {
	var highestMessageIndex *int

	walkMessages(parsed, &highestMessageIndex)
	walkSystem(parsed)
	walkTools(parsed)

	if highestMessageIndex != nil {
		return *highestMessageIndex + 1
	}
	return 0
}

func walkMessages(parsed map[string]interface{}, highest **int) {
	messagesRaw, ok := parsed["messages"]
	if !ok {
		return
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok {
		return
	}

	ttlWalk := newTTLOrderingWalk()

	for i, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]interface{})
		if !ok {
			continue
		}
		contentRaw, ok := msg["content"]
		if !ok {
			continue
		}
		blocks, ok := contentRaw.([]interface{})
		if !ok {
			continue
		}
		for _, blockRaw := range blocks {
			block, ok := blockRaw.(map[string]interface{})
			if !ok {
				continue
			}
			if _, hasCacheControl := block["cache_control"]; hasCacheControl {
				marker, _ := block["cache_control"].(map[string]interface{})
				ttl := extractTTL(marker)
				ttlWalk.observe(ttl)
				idx := i
				if *highest == nil || idx > **highest {
					*highest = &idx
				}
			}
		}
	}

	ttlWalk.warnIfViolated("messages")
}

func walkSystem(parsed map[string]interface{}) {
	systemRaw, ok := parsed["system"]
	if !ok {
		return
	}
	blocks, ok := systemRaw.([]interface{})
	if !ok {
		return
	}
	ttlWalk := newTTLOrderingWalk()
	for _, blockRaw := range blocks {
		block, ok := blockRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, hasCacheControl := block["cache_control"]; hasCacheControl {
			marker, _ := block["cache_control"].(map[string]interface{})
			ttl := extractTTL(marker)
			ttlWalk.observe(ttl)
		}
	}
	ttlWalk.warnIfViolated("system")
}

func walkTools(parsed map[string]interface{}) {
	toolsRaw, ok := parsed["tools"]
	if !ok {
		return
	}
	tools, ok := toolsRaw.([]interface{})
	if !ok {
		return
	}
	ttlWalk := newTTLOrderingWalk()
	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, hasCacheControl := tool["cache_control"]; hasCacheControl {
			marker, _ := tool["cache_control"].(map[string]interface{})
			ttl := extractTTL(marker)
			ttlWalk.observe(ttl)
		}
	}
	ttlWalk.warnIfViolated("tools")
}

func extractTTL(marker map[string]interface{}) *string {
	if marker == nil {
		return nil
	}
	ttlRaw, ok := marker["ttl"]
	if !ok {
		return nil
	}
	ttl, ok := ttlRaw.(string)
	if !ok {
		return nil
	}
	return &ttl
}

type ttlOrderingWalk struct {
	seen5m   bool
	violated bool
}

func newTTLOrderingWalk() *ttlOrderingWalk {
	return &ttlOrderingWalk{}
}

func (w *ttlOrderingWalk) observe(ttl *string) {
	is5m := ttl == nil || *ttl == "5m"
	is1h := ttl != nil && *ttl == "1h"

	if is5m {
		w.seen5m = true
	} else if is1h && w.seen5m {
		w.violated = true
	}
}

func (w *ttlOrderingWalk) warnIfViolated(field string) {
	if w.violated {
		log.Printf("WARNING: cache_control TTL ordering violation in %s: 1h marker appears after 5m marker", field)
	}
}
