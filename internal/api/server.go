package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/identity"
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
	Logger          *slog.Logger
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

	s.server = &http.Server{
		Addr:              options.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	web.RegisterRoutes(mux, web.Options{
		Version:         options.Version,
		Database:        options.Database,
		IdentityService: options.IdentityService,
		Logger:          options.Logger,
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
