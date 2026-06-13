package textutil

import "strings"

type LineIndex struct {
	content string
	offsets []int32
}

func NewLineIndex(content string) LineIndex {
	n := strings.Count(content, "\n") + 1
	offsets := make([]int32, 0, n+1)
	offsets = append(offsets, 0)
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			offsets = append(offsets, int32(i+1))
		}
	}
	return LineIndex{content: content, offsets: offsets}
}

func (li *LineIndex) Line(i int) string {
	start := int(li.offsets[i])
	var end int
	if i+1 < len(li.offsets) {
		end = int(li.offsets[i+1]) - 1
	} else {
		end = len(li.content)
	}
	if end < start {
		end = start
	}
	return li.content[start:end]
}

func (li *LineIndex) LineCount() int {
	return len(li.offsets)
}
