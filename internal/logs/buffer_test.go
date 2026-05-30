package logs

import (
	"strings"
	"testing"
)

func TestBufferKeepsLatestLines(t *testing.T) {
	buffer := NewBuffer(2)

	if _, err := buffer.Write([]byte("one\ntwo\nthree\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	lines := buffer.Lines()
	if got, want := strings.Join(lines, ","), "two,three"; got != want {
		t.Fatalf("Lines() = %q, want %q", got, want)
	}
}

func TestBufferKeepsPartialLine(t *testing.T) {
	buffer := NewBuffer(3)

	if _, err := buffer.Write([]byte("one\nt")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if _, err := buffer.Write([]byte("wo\nthree")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	lines := buffer.Lines()
	if got, want := strings.Join(lines, ","), "one,two,three"; got != want {
		t.Fatalf("Lines() = %q, want %q", got, want)
	}
}
