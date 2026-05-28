package logs

import (
	"strings"
	"sync"
)

// Buffer keeps the latest log lines in memory and also acts as an io.Writer.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	lines    []string
	partial  string
}

func NewBuffer(capacity int) *Buffer {
	if capacity < 1 {
		capacity = 1
	}

	return &Buffer{
		capacity: capacity,
		lines:    make([]string, 0, capacity),
	}
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	text := b.partial + string(p)
	parts := strings.Split(text, "\n")

	b.partial = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		b.appendLocked(strings.TrimRight(line, "\r"))
	}

	return len(p), nil
}

func (b *Buffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	lines := make([]string, 0, len(b.lines)+1)
	lines = append(lines, b.lines...)
	if b.partial != "" {
		lines = append(lines, b.partial)
	}

	return lines
}

func (b *Buffer) Text() string {
	return strings.Join(b.Lines(), "\n")
}

func (b *Buffer) appendLocked(line string) {
	if line == "" {
		return
	}

	if len(b.lines) == b.capacity {
		copy(b.lines, b.lines[1:])
		b.lines[len(b.lines)-1] = line
		return
	}

	b.lines = append(b.lines, line)
}
