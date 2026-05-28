package minecraft

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/PeterTerpe/MeshBan/internal/config"
)

type logTailer struct {
	path               string
	readFromEndOnStart bool
	initialized        bool
	offset             int64
	pending            string
}

func newLogTailer(cfg config.MinecraftLogConfig) *logTailer {
	readFromEnd := true
	if cfg.ReadFromEndOnStart != nil {
		readFromEnd = *cfg.ReadFromEndOnStart
	}

	return &logTailer{
		path:               strings.TrimSpace(cfg.Path),
		readFromEndOnStart: readFromEnd,
	}
}

func (t *logTailer) ReadNewLines() ([]string, error) {
	if t.path == "" {
		return nil, errors.New("Minecraft log path is missing")
	}

	file, err := os.Open(t.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if !t.initialized {
		t.initialized = true
		if t.readFromEndOnStart {
			t.offset = size
			return nil, nil
		}
	}

	if size < t.offset {
		t.offset = 0
		t.pending = ""
	}

	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(file)
	lines := []string{}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if strings.HasSuffix(line, "\n") {
				lines = append(lines, strings.TrimRight(t.pending+line, "\r\n"))
				t.pending = ""
			} else {
				t.pending += line
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, err
		}
	}

	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	t.offset = offset

	return lines, nil
}
