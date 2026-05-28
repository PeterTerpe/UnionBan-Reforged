package web

import (
	"html/template"
	"io"
	"testing"

	"github.com/PeterTerpe/MeshBan/internal/config"
)

func TestTemplatesRender(t *testing.T) {
	pages := []string{
		"dashboard.html",
		"database.html",
		"identity.html",
		"login.html",
		"logs.html",
		"minecraft.html",
		"security.html",
	}

	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			funcs := template.FuncMap{
				"formatUnix":  formatUnix,
				"statusClass": statusClass,
			}

			templates, err := template.New("").Funcs(funcs).ParseFS(
				content,
				"templates/base.html",
				"templates/"+page,
			)
			if err != nil {
				t.Fatalf("ParseFS returned error: %v", err)
			}

			data := PageData{
				Title:   "Test",
				Version: "test",
				Config:  &config.Config{},
			}

			if err := templates.ExecuteTemplate(io.Discard, "base", data); err != nil {
				t.Fatalf("ExecuteTemplate returned error: %v", err)
			}
		})
	}
}

func TestMinecraftLogLinesForInstance(t *testing.T) {
	lines := []string{
		`time=2026-05-28T12:00:00Z level=INFO msg="starting Minecraft monitor" instance=survival`,
		`time=2026-05-28T12:00:01Z level=INFO msg="starting Minecraft monitor" instance=creative`,
		`time=2026-05-28T12:00:02Z level=INFO msg="starting Minecraft monitor" instance="server one"`,
	}

	filtered := minecraftLogLinesForInstance(lines, "survival")
	if len(filtered) != 1 {
		t.Fatalf("filtered length = %d, want 1", len(filtered))
	}

	if filtered[0] != lines[0] {
		t.Fatalf("filtered[0] = %q, want survival line", filtered[0])
	}

	filtered = minecraftLogLinesForInstance(lines, "server one")
	if len(filtered) != 1 {
		t.Fatalf("quoted filtered length = %d, want 1", len(filtered))
	}

	if filtered[0] != lines[2] {
		t.Fatalf("filtered[0] = %q, want quoted instance line", filtered[0])
	}
}
