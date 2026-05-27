package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/database"
	peerdebug "github.com/PeterTerpe/MeshBan/internal/debug/peer"
)

//go:embed templates/*.html static/*
var content embed.FS

type Options struct {
	Version  string
	Database *database.Database
	Logger   *slog.Logger
}

type Handler struct {
	version   string
	database  *database.Database
	logger    *slog.Logger
	templates *template.Template
}

type PageData struct {
	Title          string
	Version        string
	DatabaseResult *database.DebugInfo
	PeerResult     *peerdebug.Result
	PeerAddress    string
}

func RegisterRoutes(mux *http.ServeMux, options Options) {
	// Parse embedded HTML templates.
	templates := template.Must(template.ParseFS(content, "templates/*.html"))

	handler := &Handler{
		version:   options.Version,
		database:  options.Database,
		logger:    options.Logger,
		templates: templates,
	}

	// Serve embedded static files.
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}

	mux.Handle("/ui/static/", http.StripPrefix("/ui/static/", http.FileServer(http.FS(staticFS))))

	mux.HandleFunc("/ui", handler.handleDashboard)
	mux.HandleFunc("/ui/", handler.handleDashboard)
	mux.HandleFunc("/ui/debug/database", handler.handleDatabaseDebug)
	mux.HandleFunc("/ui/debug/peer", handler.handlePeerDebug)
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ui" && r.URL.Path != "/ui/" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.renderDashboard(w, PageData{
		Title:   "Dashboard",
		Version: h.version,
	})
}

func (h *Handler) handleDatabaseDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit the database debug operation to avoid blocking the WebUI.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	result := h.database.DebugInfo(ctx)

	h.renderDashboard(w, PageData{
		Title:          "Dashboard",
		Version:        h.version,
		DatabaseResult: &result,
	})
}

func (h *Handler) handlePeerDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	address := strings.TrimSpace(r.FormValue("address"))

	var result peerdebug.Result

	if address == "" {
		result = peerdebug.Result{
			OK:      false,
			Message: "address is required",
		}
	} else {
		// Limit the peer connection test to avoid long waits.
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		result = peerdebug.TestTCP(ctx, address, 3*time.Second)
	}

	h.renderDashboard(w, PageData{
		Title:       "Dashboard",
		Version:     h.version,
		PeerResult:  &result,
		PeerAddress: address,
	})
}

func (h *Handler) renderDashboard(w http.ResponseWriter, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := h.templates.ExecuteTemplate(w, "base", data); err != nil {
		h.logger.Error("failed to render WebUI template", "error", err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}
