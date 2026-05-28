package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/auth"
	"github.com/PeterTerpe/MeshBan/internal/config"
	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/identity"
	"github.com/PeterTerpe/MeshBan/internal/logs"
	"github.com/PeterTerpe/MeshBan/internal/secrets"
	"github.com/PeterTerpe/MeshBan/internal/web"
)

type Server struct {
	server          *http.Server
	version         string
	database        *database.Database
	identityService *identity.Service
	logger          *slog.Logger
}

type Options struct {
	ListenAddr      string
	Version         string
	Database        *database.Database
	IdentityService *identity.Service
	Config          *config.Config
	ConfigPath      string
	SecretManager   *secrets.Manager
	Logger          *slog.Logger
	LogBuffer       *logs.Buffer
}

type StatusResponse struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	Database  string `json:"database"`
	Timestamp int64  `json:"timestamp"`
}

func NewServer(options Options) *Server {
	mux := http.NewServeMux()

	s := &Server{
		version:         options.Version,
		database:        options.Database,
		identityService: options.IdentityService,
		logger:          options.Logger,
	}

	mux.HandleFunc("/api/v1/status", s.handleStatus)

	handler := adminAccessMiddleware(mux, options.Config, options.SecretManager)
	s.server = &http.Server{
		Addr:              options.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	web.RegisterRoutes(mux, web.Options{
		Version:         options.Version,
		Database:        options.Database,
		IdentityService: options.IdentityService,
		Config:          options.Config,
		ConfigPath:      options.ConfigPath,
		SecretManager:   options.SecretManager,
		Logger:          options.Logger,
		LogBuffer:       options.LogBuffer,
	})

	return s
}

func (s *Server) Start(ctx context.Context) error {
	// Stop the HTTP server when the root context is cancelled.
	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("failed to shut down HTTP server", "error", err)
		}
	}()

	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}

	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	dbStatus := "ok"
	if err := s.database.Ping(r.Context()); err != nil {
		dbStatus = "error"
	}

	response := StatusResponse{
		Name:      "MeshBan",
		Version:   s.version,
		Status:    "ok",
		Database:  dbStatus,
		Timestamp: time.Now().Unix(),
	}

	writeJSON(w, http.StatusOK, response)
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	_ = encoder.Encode(value)
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	host = strings.TrimSpace(host)

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback()
}

func adminAccessMiddleware(next http.Handler, cfg *config.Config, secretManager *secrets.Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		if cfg != nil && !cfg.WebUI.RequireTokenForRemote {
			next.ServeHTTP(w, r)
			return
		}

		if isPublicWebUIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token := ""
		if secretManager != nil {
			token = strings.TrimSpace(secretManager.Get(secrets.WebTokenEnv))
		}

		if token == "" {
			http.Error(w, "remote access token is not configured", http.StatusForbidden)
			return
		}

		if auth.HasValidSession(r, token) {
			next.ServeHTTP(w, r)
			return
		}

		if auth.RequestHasValidToken(r, token) {
			next.ServeHTTP(w, r)
			return
		}

		if wantsHTML(r) && strings.HasPrefix(r.URL.Path, "/ui") {
			loginURL := "/ui/login?next=" + url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, loginURL, http.StatusSeeOther)
			return
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func isPublicWebUIPath(path string) bool {
	if path == "/ui/login" || path == "/ui/logout" {
		return true
	}

	if strings.HasPrefix(path, "/ui/static/") {
		return true
	}

	return false
}

func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") || accept == ""
}
