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
	"github.com/PeterTerpe/MeshBan/internal/minecraft"
	nodesclient "github.com/PeterTerpe/MeshBan/internal/nodes"
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
	Minecraft       *minecraft.Service
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

	// Register node-to-node API endpoints.
	mux.HandleFunc("/api/v1/identity", s.handleNodeIdentity)
	mux.HandleFunc("/api/v1/player", s.handlePlayerQuery)

	// Node API calls are public but restricted to GET method.
	// The admin middleware runs after, ensuring WebUI paths still require auth.
	handler := adminAccessMiddleware(
		nodeMethodMiddleware(mux),
		options.SecretManager,
	)
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
		Minecraft:       options.Minecraft,
		NodeClient:      nodesclient.NewClient(options.Logger),
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
	for _, ip := range clientIPs(r) {
		if ip.IsLoopback() {
			return true
		}
	}
	return false
}

func clientIPs(r *http.Request) []net.IP {
	var ips []net.IP

	// Respect X-Forwarded-For set by reverse proxies like nginx.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			ip := net.ParseIP(strings.TrimSpace(part))
			if ip != nil {
				ips = append(ips, ip)
			}
		}
	}

	// X-Real-IP is commonly set by nginx as well.
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		ip := net.ParseIP(strings.TrimSpace(xri))
		if ip != nil {
			ips = append(ips, ip)
		}
	}

	// Always include the direct remote address.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		ips = append(ips, ip)
	}

	return ips
}

func adminAccessMiddleware(next http.Handler, secretManager *secrets.Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow local loopback access without authentication.
		if isLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token := ""
		if secretManager != nil {
			token = strings.TrimSpace(secretManager.Get(secrets.WebTokenEnv))
		}

		if token == "" {
			http.Error(w, "access token is not configured", http.StatusForbidden)
			return
		}

		if auth.HasValidSession(r, token) {
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

func isPublicPath(path string) bool {
	if path == "/ui/login" || path == "/ui/logout" {
		return true
	}

	if strings.HasPrefix(path, "/ui/static/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/") {
		return true
	}

	return false
}

func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") || accept == ""
}

// nodeMethodMiddleware restricts requests to /api/v1/* to GET only.
// Non-node paths pass through unchanged.
func nodeMethodMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "method not allowed",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}
