package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/database"
	peerdebug "github.com/PeterTerpe/MeshBan/internal/debug/peer"
	"github.com/PeterTerpe/MeshBan/internal/identity"
)

//go:embed templates/*.html static/*
var content embed.FS

type Options struct {
	Version         string
	Database        *database.Database
	IdentityService *identity.Service
	Logger          *slog.Logger
}

type Handler struct {
	version         string
	database        *database.Database
	identityService *identity.Service
	logger          *slog.Logger
}

type PageData struct {
	Title           string
	Version         string
	DatabaseResult  *database.DebugInfo
	PeerResult      *peerdebug.Result
	PeerAddress     string
	BanEntries      []database.BanEntry
	Message         string
	ErrorMessage    string
	LocalIdentity   *identity.Identity
	ExportedKeyPair string
}

func RegisterRoutes(mux *http.ServeMux, options Options) {
	handler := &Handler{
		version:         options.Version,
		database:        options.Database,
		identityService: options.IdentityService,
		logger:          options.Logger,
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

	mux.HandleFunc("/ui/database", handler.handleDatabasePage)
	mux.HandleFunc("/ui/database/banlist/create", handler.handleCreateBanEntry)
	mux.HandleFunc("/ui/database/banlist/update", handler.handleUpdateBanEntry)
	mux.HandleFunc("/ui/database/banlist/delete", handler.handleDeleteBanEntry)

	mux.HandleFunc("/ui/identity", handler.handleIdentityPage)
	mux.HandleFunc("/ui/identity/export", handler.handleExportIdentity)
	mux.HandleFunc("/ui/identity/import", handler.handleImportIdentity)
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
	h.renderPage(w, "dashboard.html", data)
}

func (h *Handler) renderPage(w http.ResponseWriter, page string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	funcs := template.FuncMap{
		"formatUnix": formatUnix,
	}

	templates, err := template.New("").Funcs(funcs).ParseFS(
		content,
		"templates/base.html",
		"templates/"+page,
	)
	if err != nil {
		h.logger.Error("failed to parse WebUI template", "error", err)
		http.Error(w, "failed to parse page", http.StatusInternalServerError)
		return
	}

	if err := templates.ExecuteTemplate(w, "base", data); err != nil {
		h.logger.Error("failed to render WebUI template", "error", err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

func formatUnix(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}

	return time.Unix(timestamp, 0).Local().Format("2006-01-02 15:04:05")
}

func (h *Handler) handleDatabasePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.renderDatabasePage(w, r, PageData{
		Title:        "Database",
		Version:      h.version,
		Message:      r.URL.Query().Get("message"),
		ErrorMessage: r.URL.Query().Get("error"),
	})
}

func (h *Handler) handleCreateBanEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, err := parseBanEntryForm(r)
	if err != nil {
		redirectWithError(w, r, "/ui/database", err.Error())
		return
	}

	now := time.Now().Unix()
	current := h.identityService.Current()

	entry.SourceNodeID = current.NodeID
	entry.CreatedAt = now
	entry.UpdatedAt = now

	signature, err := h.identityService.SignLocalBan(
		entry.PlayerUUID,
		entry.Reason,
		entry.SourceNodeID,
		entry.UpdatedAt,
	)

	if err != nil {
		redirectWithError(w, r, "/ui/database", err.Error())
		return
	}

	entry.Signature = signature

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if _, err := h.database.CreateBanEntry(ctx, entry); err != nil {
		redirectWithError(w, r, "/ui/database", err.Error())
		return
	}

	redirectWithMessage(w, r, "/ui/database", "ban entry created")
}

func (h *Handler) handleUpdateBanEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, err := parseBanEntryForm(r)
	if err != nil {
		redirectWithError(w, r, "/ui/database", err.Error())
		return
	}

	now := time.Now().Unix()
	current := h.identityService.Current()

	entry.SourceNodeID = current.NodeID
	entry.UpdatedAt = now

	signature, err := h.identityService.SignLocalBan(
		entry.PlayerUUID,
		entry.Reason,
		entry.SourceNodeID,
		entry.UpdatedAt,
	)

	if err != nil {
		redirectWithError(w, r, "/ui/database", err.Error())
		return
	}

	entry.Signature = signature

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.database.UpdateBanEntry(ctx, entry); err != nil {
		redirectWithError(w, r, "/ui/database", err.Error())
		return
	}

	http.Redirect(w, r, "/ui/database?message=ban entry updated", http.StatusSeeOther)
}

func (h *Handler) handleDeleteBanEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "/ui/database", "invalid form")
		return
	}

	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		redirectWithError(w, r, "/ui/database", "invalid ban entry id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.database.DeleteBanEntry(ctx, id); err != nil {
		redirectWithError(w, r, "/ui/database", err.Error())
		return
	}

	http.Redirect(w, r, "/ui/database?message=ban entry deleted", http.StatusSeeOther)
}

func (h *Handler) renderDatabasePage(w http.ResponseWriter, r *http.Request, data PageData) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	entries, err := h.database.ListBanEntries(ctx)
	if err != nil {
		data.ErrorMessage = err.Error()
	} else {
		data.BanEntries = entries
	}

	h.renderPage(w, "database.html", data)
}

func parseBanEntryForm(r *http.Request) (database.BanEntry, error) {
	if err := r.ParseForm(); err != nil {
		return database.BanEntry{}, err
	}

	var id int64

	if idText := strings.TrimSpace(r.FormValue("id")); idText != "" {
		parsedID, err := strconv.ParseInt(idText, 10, 64)
		if err != nil {
			return database.BanEntry{}, err
		}

		id = parsedID
	}

	return database.BanEntry{
		ID:         id,
		PlayerUUID: r.FormValue("player_uuid"),
		Reason:     r.FormValue("reason"),
	}, nil
}

func redirectWithError(w http.ResponseWriter, r *http.Request, path string, message string) {
	http.Redirect(w, r, path+"?error="+url.QueryEscape(message), http.StatusSeeOther)
}

func redirectWithMessage(w http.ResponseWriter, r *http.Request, path string, message string) {
	http.Redirect(w, r, path+"?message="+url.QueryEscape(message), http.StatusSeeOther)
}

func (h *Handler) handleIdentityPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	current := h.identityService.Current()

	h.renderPage(w, "identity.html", PageData{
		Title:         "Identity",
		Version:       h.version,
		LocalIdentity: &current,
		Message:       r.URL.Query().Get("message"),
		ErrorMessage:  r.URL.Query().Get("error"),
	})
}

func (h *Handler) handleExportIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	raw, err := h.identityService.ExportKeyPairJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	filename := "meshban-keypair-" + time.Now().Format("20060102-150405") + ".json"
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(raw)
}

func (h *Handler) handleImportIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "/ui/identity", "invalid form")
		return
	}

	raw := strings.TrimSpace(r.FormValue("keypair_json"))
	if raw == "" {
		redirectWithError(w, r, "/ui/identity", "key pair json is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.identityService.ImportKeyPairJSON(ctx, []byte(raw)); err != nil {
		redirectWithError(w, r, "/ui/identity", err.Error())
		return
	}

	http.Redirect(w, r, "/ui/identity?message=key pair imported", http.StatusSeeOther)
}
