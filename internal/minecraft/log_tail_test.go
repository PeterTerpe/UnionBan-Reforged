package minecraft

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PeterTerpe/MeshBan/internal/config"
)

func TestLogTailerStartsAtEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latest.log")
	if err := os.WriteFile(path, []byte("old line\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	tailer := newLogTailer(config.MinecraftLogConfig{
		Path: path,
	})

	lines, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatalf("ReadNewLines returned error: %v", err)
	}

	if len(lines) != 0 {
		t.Fatalf("lines length = %d, want 0", len(lines))
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile returned error: %v", err)
	}
	if _, err := file.WriteString("new line\n"); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	lines, err = tailer.ReadNewLines()
	if err != nil {
		t.Fatalf("ReadNewLines returned error: %v", err)
	}

	if len(lines) != 1 || lines[0] != "new line" {
		t.Fatalf("lines = %#v, want new line", lines)
	}
}

func TestLogTailerKeepsPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latest.log")

	// Start with an empty file (tailer always reads from end).
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	tailer := newLogTailer(config.MinecraftLogConfig{
		Path: path,
	})

	// Initialize the tailer — sets offset to end of (empty) file.
	if _, err := tailer.ReadNewLines(); err != nil {
		t.Fatalf("ReadNewLines returned error: %v", err)
	}

	// Write a partial line without a newline.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile returned error: %v", err)
	}
	if _, err := file.WriteString("partial"); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	lines, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatalf("ReadNewLines returned error: %v", err)
	}

	if len(lines) != 0 {
		t.Fatalf("lines length = %d, want 0", len(lines))
	}

	// Append the rest of the line.
	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile returned error: %v", err)
	}
	if _, err := file.WriteString(" line\n"); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	lines, err = tailer.ReadNewLines()
	if err != nil {
		t.Fatalf("ReadNewLines returned error: %v", err)
	}

	if len(lines) != 1 || lines[0] != "partial line" {
		t.Fatalf("lines = %#v, want partial line", lines)
	}
}
