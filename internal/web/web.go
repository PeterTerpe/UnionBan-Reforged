package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/auth"
	"github.com/PeterTerpe/MeshBan/internal/config"
	"github.com/PeterTerpe/MeshBan/internal/database"
	peerdebug "github.com/PeterTerpe/MeshBan/internal/debug/peer"
	"github.com/PeterTerpe/MeshBan/internal/identity"
	"github.com/PeterTerpe/MeshBan/internal/logs"
	"github.com/PeterTerpe/MeshBan/internal/minecraft"
	"github.com/PeterTerpe/MeshBan/internal/secrets"
)

//go:embed templates/*.html static/*
var content embed.FS

var templateFuncs = template.FuncMap{
	"formatUnix":  formatUnix,
	"statusClass": statusClass,
	"sub":         func(a, b int) int { return a - b },
	"add":         func(a, b int) int { return a + b },
}

type Options struct {
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

type Handler struct {
	version         string
	database        *database.Database
	identityService *identity.Service
	config          *config.Config
	configPath      string
	secretManager   *secrets.Manager
	logger          *slog.Logger
	logBuffer       *logs.Buffer
	minecraft       *minecraft.Service
	loginLimiter    *auth.LoginLimiter
	templateCache   map[string]*template.Template
}

type MinecraftPolicyFormData struct {
	KickMessage    string
	KickReason     string
	SupportContact string
	Ultimate       int
	UltimateSet    bool
	Trusted        int
	TrustedSet     bool
	Friend         int
	FriendSet      bool
	Unknown        int
	UnknownSet     bool
	Untrusted      int
	UntrustedSet   bool
}

type MinecraftUUIDResolverFormData struct {
	Enabled           bool
	Endpoint          string
	ResponseUUIDField string
	TimeoutSeconds    int
	RetryCount        int
	ProxyType         string
	ProxyURL          string
	ProxyURLEnv       string
	ProxyAuth         bool
	ProxyUsernameEnv  string
	ProxyPassEnv      string
}

type MinecraftInstanceFormData struct {
	Index                            int
	ID                               string
	Enabled                          bool
	Mode                             string
	RCONHost                         string
	RCONPort                         int
	RCONPasswordEnv                  string
	RCONPollInterval                 int
	RCONCommandTimeout               int
	LogPath                          string
	LogPollInterval                  int
	LogPollIntervalSet               bool
	BannedPlayersPollInterval        int
	BannedPlayersPollIntervalSet     bool
	BannedPlayersPath                string
	HasRCONPassword                  bool
	Policy                           MinecraftPolicyFormData
	PolicyDefaults                   MinecraftPolicyFormData
	DefaultLogPollInterval           int
	DefaultBannedPlayersPollInterval int
	UUIDResolver                     MinecraftUUIDResolverFormData
	Status                           *minecraft.ConnectorStatus
	LogLines                         []string
}

type PageData struct {
	Title                            string
	Version                          string
	DatabaseResult                   *database.DebugInfo
	PeerResult                       *peerdebug.Result
	PeerAddress                      string
	BanEntries                       []database.BanEntry
	BanTotalCount                    int64
	BanCurrentPage                   int
	BanTotalPages                    int
	BanPerPage                       int
	BanSearchUUID                    string
	BanSearchName                    string
	BanSearchSource                  string
	BanSearchReason                  string
	Message                          string
	ErrorMessage                     string
	LocalIdentity                    *identity.Identity
	Config                           *config.Config
	HasKeyPassphrase                 bool
	HasWebToken                      bool
	WebToken                         string
	LoginNext                        string
	LogLines                         []string
	MinecraftStatus                  []minecraft.ConnectorStatus
	MinecraftPolicy                  MinecraftPolicyFormData
	MinecraftResolver                MinecraftUUIDResolverFormData
	MinecraftInstances               []MinecraftInstanceFormData
	DefaultLogPollInterval           int
	DefaultBannedPlayersPollInterval int
}

func RegisterRoutes(mux *http.ServeMux, options Options) {
	handler := &Handler{
		version:         options.Version,
		database:        options.Database,
		identityService: options.IdentityService,
		config:          options.Config,
		configPath:      options.ConfigPath,
		secretManager:   options.SecretManager,
		logger:          options.Logger,
		logBuffer:       options.LogBuffer,
		minecraft:       options.Minecraft,
		loginLimiter:    auth.NewLoginLimiter(5, 10*time.Minute, 15*time.Minute),
		templateCache:   make(map[string]*template.Template),
	}

	// Pre-parse all page templates once at startup.
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
		tmpl, err := template.New("").Funcs(templateFuncs).ParseFS(
			content,
			"templates/base.html",
			"templates/"+page,
		)
		if err != nil {
			panic("failed to parse template " + page + ": " + err.Error())
		}
		handler.templateCache[page] = tmpl
	}

	// Serve embedded static files.
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}

	mux.Handle("/ui/static/", http.StripPrefix("/ui/static/", http.FileServer(http.FS(staticFS))))

	mux.HandleFunc("/ui", handler.handleDashboard)
	mux.HandleFunc("/ui/", handler.handleDashboard)
	mux.HandleFunc("/ui/login", handler.handleLoginPage)
	mux.HandleFunc("/ui/logout", handler.handleLogout)

	mux.HandleFunc("/ui/debug/database", handler.handleDatabaseDebug)
	mux.HandleFunc("/ui/debug/peer", handler.handlePeerDebug)

	mux.HandleFunc("/ui/database", handler.handleDatabasePage)
	mux.HandleFunc("/ui/database/", handler.handleDatabasePage)

	mux.HandleFunc("/ui/identity", handler.handleIdentityPage)
	mux.HandleFunc("/ui/identity/export", handler.handleExportIdentity)
	mux.HandleFunc("/ui/identity/", handler.handleIdentityPage)

	mux.HandleFunc("/ui/settings/security", handler.handleSecuritySettingsPage)
	mux.HandleFunc("/ui/settings/security/", handler.handleSecuritySettingsPage)

	mux.HandleFunc("/ui/logs", handler.handleLogsPage)
	mux.HandleFunc("/ui/minecraft", handler.handleMinecraftPage)
	mux.HandleFunc("/ui/minecraft/", handler.handleMinecraftPage)
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

func (h *Handler) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lines := []string{}
	if h.logBuffer != nil {
		lines = h.logBuffer.Lines()
	}

	h.renderPage(w, "logs.html", PageData{
		Title:    "Logs",
		Version:  h.version,
		LogLines: lines,
	})
}

func (h *Handler) handleMinecraftPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.renderMinecraftPage(w, PageData{
			Title:   "Minecraft",
			Version: h.version,
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.flashMinecraft(w, "", "invalid form")
		return
	}

	switch r.FormValue("_action") {
	case "save":
		h.minecraftDoSave(w, r)
	case "add":
		h.minecraftDoAdd(w)
	case "delete":
		h.minecraftDoDelete(w, r)
	case "health-check":
		h.minecraftDoHealthCheck(w, r)
	default:
		h.flashMinecraft(w, "", "unknown action")
	}
}

func (h *Handler) minecraftDoSave(w http.ResponseWriter, r *http.Request) {
	minecraftConfig, err := h.parseMinecraftConfigForm(r)
	if err != nil {
		h.flashMinecraft(w, "", err.Error())
		return
	}

	h.config.Minecraft = minecraftConfig
	config.ApplyDefaults(h.config)
	h.saveMinecraftConfig(w, "Minecraft settings updated")
}

func (h *Handler) minecraftDoAdd(w http.ResponseWriter) {
	nextID := nextMinecraftInstanceID(h.config.Minecraft.Instances)
	h.config.Minecraft.Instances = append(h.config.Minecraft.Instances, config.MinecraftInstanceConfig{
		ID:      nextID,
		Enabled: false,
		Mode:    "rcon",
		RCON: config.MinecraftRCONConfig{
			Host:                  "127.0.0.1",
			Port:                  25575,
			PasswordEnv:           strings.ToUpper(strings.ReplaceAll(nextID, "-", "_")) + "_RCON_PASS",
			CommandTimeoutSeconds: 3,
		},
		Log: config.MinecraftLogConfig{},
	})

	config.ApplyDefaults(h.config)
	h.saveMinecraftConfig(w, "Minecraft connector added")
}

func (h *Handler) minecraftDoDelete(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(strings.TrimSpace(r.FormValue("index")))
	if err != nil || index < 0 || index >= len(h.config.Minecraft.Instances) {
		h.flashMinecraft(w, "", "invalid connector index")
		return
	}

	h.config.Minecraft.Instances = append(h.config.Minecraft.Instances[:index], h.config.Minecraft.Instances[index+1:]...)
	config.ApplyDefaults(h.config)
	h.saveMinecraftConfig(w, "Minecraft connector deleted")
}

// saveMinecraftConfig persists the current config and applies it to the Minecraft service.
func (h *Handler) saveMinecraftConfig(w http.ResponseWriter, successMsg string) {
	if err := config.Save(h.configPath, h.config); err != nil {
		h.flashMinecraft(w, "", err.Error())
		return
	}

	if h.minecraft != nil {
		h.minecraft.ApplyConfig(h.config.Minecraft)
	}

	h.flashMinecraft(w, successMsg, "")
}

func (h *Handler) minecraftDoHealthCheck(w http.ResponseWriter, r *http.Request) {
	if h.minecraft == nil {
		h.flashMinecraft(w, "", "Minecraft service is not running")
		return
	}

	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		h.flashMinecraft(w, "", "connector id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	if err := h.minecraft.CheckHealth(ctx, id); err != nil {
		h.flashMinecraft(w, "", "health check failed: "+err.Error())
		return
	}

	h.flashMinecraft(w, "health check passed for "+id, "")
}

func (h *Handler) renderMinecraftPage(w http.ResponseWriter, data PageData) {
	data.Config = h.config
	data.MinecraftStatus = h.minecraftStatuses()
	data.MinecraftPolicy = minecraftPolicyFormData(h.config.Minecraft.DefaultPolicy)
	data.MinecraftResolver = minecraftResolverFormData(h.config.Minecraft.UUIDResolver)
	data.MinecraftInstances = h.minecraftInstanceFormData(data.MinecraftStatus)
	data.DefaultLogPollInterval = intOrDefault(h.config.Minecraft.LogPollIntervalSeconds, 1)
	data.DefaultBannedPlayersPollInterval = intOrDefault(h.config.Minecraft.BannedPlayersPollIntervalSeconds, 60)

	h.renderPage(w, "minecraft.html", data)
}

func (h *Handler) minecraftStatuses() []minecraft.ConnectorStatus {
	if h.minecraft == nil {
		return nil
	}

	statuses := h.minecraft.Statuses()
	sort.Slice(statuses, func(i int, j int) bool {
		return statuses[i].ID < statuses[j].ID
	})

	return statuses
}

func (h *Handler) minecraftInstanceFormData(statuses []minecraft.ConnectorStatus) []MinecraftInstanceFormData {
	statusByID := make(map[string]minecraft.ConnectorStatus, len(statuses))
	for _, status := range statuses {
		statusByID[status.ID] = status
	}

	logLines := []string{}
	if h.logBuffer != nil {
		logLines = h.logBuffer.Lines()
	}

	instances := make([]MinecraftInstanceFormData, 0, len(h.config.Minecraft.Instances))
	for i, instance := range h.config.Minecraft.Instances {
		instanceID := minecraftInstanceID(instance)

		status, hasStatus := statusByID[instanceID]
		var statusPtr *minecraft.ConnectorStatus
		if hasStatus {
			statusCopy := status
			statusPtr = &statusCopy
		}

		hasPassword := false
		if h.secretManager != nil && strings.TrimSpace(instance.RCON.PasswordEnv) != "" {
			hasPassword = strings.TrimSpace(h.secretManager.Get(instance.RCON.PasswordEnv)) != ""
		}

		instances = append(instances, MinecraftInstanceFormData{
			Index:                            i,
			ID:                               instance.ID,
			Enabled:                          instance.Enabled,
			Mode:                             firstNonEmpty(instance.Mode, "rcon"),
			RCONHost:                         firstNonEmpty(instance.RCON.Host, "127.0.0.1"),
			RCONPort:                         intOrDefault(instance.RCON.Port, 25575),
			RCONPasswordEnv:                  instance.RCON.PasswordEnv,
			RCONPollInterval:                 intPtrVal(instance.RCON.PollIntervalSeconds),
			RCONCommandTimeout:               intOrDefault(instance.RCON.CommandTimeoutSeconds, 3),
			LogPath:                          instance.Log.Path,
			LogPollInterval:                  intPtrVal(instance.Log.PollIntervalSeconds),
			LogPollIntervalSet:               instance.Log.PollIntervalSeconds != nil,
			BannedPlayersPollInterval:        intPtrVal(instance.RCON.PollIntervalSeconds),
			BannedPlayersPollIntervalSet:     instance.RCON.PollIntervalSeconds != nil,
			BannedPlayersPath:                instance.BannedPlayersPath,
			HasRCONPassword:                  hasPassword,
			Policy:                           minecraftPolicyFormData(instance.Policy),
			PolicyDefaults:                   minecraftPolicyFormData(h.config.Minecraft.DefaultPolicy),
			DefaultLogPollInterval:           intOrDefault(h.config.Minecraft.LogPollIntervalSeconds, 1),
			DefaultBannedPlayersPollInterval: intOrDefault(h.config.Minecraft.BannedPlayersPollIntervalSeconds, 60),
			UUIDResolver:                     minecraftResolverFormData(instance.UUIDResolver),
			Status:                           statusPtr,
			LogLines:                         minecraftLogLinesForInstance(logLines, instanceID),
		})
	}

	return instances
}

func (h *Handler) parseMinecraftConfigForm(r *http.Request) (config.MinecraftConfig, error) {
	instanceCount, err := strconv.Atoi(strings.TrimSpace(r.FormValue("instance_count")))
	if err != nil || instanceCount < 0 {
		return config.MinecraftConfig{}, errors.New("invalid connector count")
	}

	cfg := config.MinecraftConfig{
		Enabled:                          r.FormValue("minecraft_enabled") == "on",
		DefaultPolicy:                    parseMinecraftPolicyForm(r, "default"),
		UUIDResolver:                     parseMinecraftResolverForm(r, "default"),
		LogPollIntervalSeconds:           parseNonNegativeIntValue(r.FormValue("default_log_poll_interval")),
		BannedPlayersPollIntervalSeconds: parseNonNegativeIntValue(r.FormValue("default_banned_players_poll_interval")),
		Instances:                        make([]config.MinecraftInstanceConfig, 0, instanceCount),
	}

	seenIDs := make(map[string]bool, instanceCount)
	for i := 0; i < instanceCount; i++ {
		prefix := "instance_" + strconv.Itoa(i)
		var existing config.MinecraftInstanceConfig
		if i < len(h.config.Minecraft.Instances) {
			existing = h.config.Minecraft.Instances[i]
		}

		id := strings.TrimSpace(r.FormValue(prefix + "_id"))
		if id == "" {
			return config.MinecraftConfig{}, errors.New("connector id is required")
		}
		if seenIDs[id] {
			return config.MinecraftConfig{}, errors.New("connector id must be unique")
		}
		seenIDs[id] = true

		port, err := parsePositiveIntForm(r, prefix+"_rcon_port", "RCON port")
		if err != nil {
			return config.MinecraftConfig{}, err
		}
		if port > 65535 {
			return config.MinecraftConfig{}, errors.New("RCON port must be at most 65535")
		}

		pollInterval := parseOptionalPositiveIntForm(r, prefix+"_banned_players_poll_interval")
		commandTimeout, err := parsePositiveIntForm(r, prefix+"_rcon_command_timeout", "RCON command timeout")
		if err != nil {
			return config.MinecraftConfig{}, err
		}

		logPollInterval := parseOptionalPositiveIntForm(r, prefix+"_log_poll_interval")

		passwordEnv := strings.TrimSpace(r.FormValue(prefix + "_rcon_password_env"))
		password := strings.TrimSpace(r.FormValue(prefix + "_rcon_password"))
		if password != "" {
			if passwordEnv == "" {
				return config.MinecraftConfig{}, errors.New("RCON password env is required before setting a password")
			}

			if h.secretManager == nil {
				return config.MinecraftConfig{}, errors.New("secret manager is unavailable")
			}

			if err := h.secretManager.Set(passwordEnv, password); err != nil {
				return config.MinecraftConfig{}, err
			}
		}

		mode := strings.TrimSpace(r.FormValue(prefix + "_mode"))
		if mode == "" {
			mode = "rcon"
		}

		cfg.Instances = append(cfg.Instances, config.MinecraftInstanceConfig{
			ID:                id,
			Enabled:           r.FormValue(prefix+"_enabled") == "on",
			Mode:              mode,
			BannedPlayersPath: strings.TrimSpace(r.FormValue(prefix + "_banned_players_path")),
			RCON: config.MinecraftRCONConfig{
				Host:                  strings.TrimSpace(r.FormValue(prefix + "_rcon_host")),
				Port:                  port,
				PasswordEnv:           passwordEnv,
				PollIntervalSeconds:   pollInterval,
				CommandTimeoutSeconds: commandTimeout,
			},
			Log: config.MinecraftLogConfig{
				Path:                strings.TrimSpace(r.FormValue(prefix + "_log_path")),
				PollIntervalSeconds: logPollInterval,
			},
			Policy:          parseMinecraftPolicyForm(r, prefix),
			UUIDResolver:    existing.UUIDResolver,
			PaperAdapter:    existing.PaperAdapter,
			AdapterTokenEnv: existing.AdapterTokenEnv,
		})
	}

	return cfg, nil
}

func parseMinecraftPolicyForm(r *http.Request, prefix string) config.MinecraftPolicyConfig {
	return config.MinecraftPolicyConfig{
		KickMessage:    strings.TrimSpace(r.FormValue(prefix + "_kick_message")),
		KickReason:     strings.TrimSpace(r.FormValue(prefix + "_kick_reason")),
		SupportContact: strings.TrimSpace(r.FormValue(prefix + "_support_contact")),
		Ultimate:       parseOptionalIntForm(r, prefix+"_ultimate"),
		Trusted:        parseOptionalIntForm(r, prefix+"_trusted"),
		Friend:         parseOptionalIntForm(r, prefix+"_friend"),
		Unknown:        parseOptionalIntForm(r, prefix+"_unknown"),
		Untrusted:      parseOptionalIntForm(r, prefix+"_untrusted"),
	}
}

func parseMinecraftResolverForm(r *http.Request, prefix string) config.MinecraftUUIDResolverConfig {
	return config.MinecraftUUIDResolverConfig{
		Enabled:           r.FormValue(prefix+"_uuid_enabled") == "on",
		Endpoint:          strings.TrimSpace(r.FormValue(prefix + "_uuid_endpoint")),
		ResponseUUIDField: strings.TrimSpace(r.FormValue(prefix + "_uuid_response_field")),
		TimeoutSeconds:    parseNonNegativeIntValue(r.FormValue(prefix + "_uuid_timeout")),
		RetryCount:        parseNonNegativeIntValue(r.FormValue(prefix + "_uuid_retries")),
		ProxyType:         strings.TrimSpace(r.FormValue(prefix + "_uuid_proxy_type")),
		ProxyURL:          strings.TrimSpace(r.FormValue(prefix + "_uuid_proxy_url")),
		ProxyURLEnv:       strings.TrimSpace(r.FormValue(prefix + "_uuid_proxy_url_env")),
		ProxyAuth:         r.FormValue(prefix+"_uuid_proxy_auth") == "on",
		ProxyUsernameEnv:  strings.TrimSpace(r.FormValue(prefix + "_uuid_proxy_username_env")),
		ProxyPassEnv:      strings.TrimSpace(r.FormValue(prefix + "_uuid_proxy_pass_env")),
	}
}

func minecraftPolicyFormData(policy config.MinecraftPolicyConfig) MinecraftPolicyFormData {
	return MinecraftPolicyFormData{
		KickMessage:    policy.KickMessage,
		KickReason:     policy.KickReason,
		SupportContact: policy.SupportContact,
		Ultimate:       intPtrVal(policy.Ultimate),
		UltimateSet:    policy.Ultimate != nil,
		Trusted:        intPtrVal(policy.Trusted),
		TrustedSet:     policy.Trusted != nil,
		Friend:         intPtrVal(policy.Friend),
		FriendSet:      policy.Friend != nil,
		Unknown:        intPtrVal(policy.Unknown),
		UnknownSet:     policy.Unknown != nil,
		Untrusted:      intPtrVal(policy.Untrusted),
		UntrustedSet:   policy.Untrusted != nil,
	}
}

func minecraftResolverFormData(resolver config.MinecraftUUIDResolverConfig) MinecraftUUIDResolverFormData {
	return MinecraftUUIDResolverFormData{
		Enabled:           resolver.Enabled,
		Endpoint:          resolver.Endpoint,
		ResponseUUIDField: firstNonEmpty(resolver.ResponseUUIDField, "id"),
		TimeoutSeconds:    intOrDefault(resolver.TimeoutSeconds, 5),
		RetryCount:        resolver.RetryCount,
		ProxyType:         firstNonEmpty(resolver.ProxyType, "none"),
		ProxyURL:          resolver.ProxyURL,
		ProxyURLEnv:       resolver.ProxyURLEnv,
		ProxyAuth:         resolver.ProxyAuth,
		ProxyUsernameEnv:  resolver.ProxyUsernameEnv,
		ProxyPassEnv:      resolver.ProxyPassEnv,
	}
}

func parsePositiveIntForm(r *http.Request, name string, label string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(r.FormValue(name)))
	if err != nil || value <= 0 {
		return 0, errors.New(label + " must be a positive integer")
	}

	return value, nil
}

func parseNonNegativeIntValue(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0
	}

	return parsed
}

// parseOptionalIntForm returns an *int from a form field.  An empty or
// whitespace-only value produces nil (meaning "use default").  Otherwise
// the value is parsed as a non-negative integer (0 is allowed).
func parseOptionalIntForm(r *http.Request, name string) *int {
	raw := strings.TrimSpace(r.FormValue(name))
	if raw == "" {
		return nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return nil
	}

	return intPtr(parsed)
}

// parseOptionalPositiveIntForm returns an *int from a form field.  An empty
// or whitespace-only value produces nil (meaning "use default").  Otherwise
// the value must be a positive integer (> 0).
func parseOptionalPositiveIntForm(r *http.Request, name string) *int {
	raw := strings.TrimSpace(r.FormValue(name))
	if raw == "" {
		return nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil
	}

	return intPtr(parsed)
}

func intPtr(value int) *int {
	return &value
}

func intPtrVal(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func intOrDefault(value int, fallback int) int {
	if value == 0 {
		return fallback
	}

	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}

	return ""
}

func nextMinecraftInstanceID(instances []config.MinecraftInstanceConfig) string {
	used := make(map[string]bool, len(instances))
	for _, instance := range instances {
		used[strings.TrimSpace(instance.ID)] = true
	}

	for i := 1; ; i++ {
		id := "minecraft-" + strconv.Itoa(i)
		if !used[id] {
			return id
		}
	}
}

func minecraftInstanceID(instance config.MinecraftInstanceConfig) string {
	if id := strings.TrimSpace(instance.ID); id != "" {
		return id
	}

	host := firstNonEmpty(instance.RCON.Host, "127.0.0.1")
	port := intOrDefault(instance.RCON.Port, 25575)

	return host + ":" + strconv.Itoa(port)
}

func minecraftLogLinesForInstance(lines []string, instanceID string) []string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil
	}

	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if !minecraftLogLineMatchesInstance(line, instanceID) {
			continue
		}

		filtered = append(filtered, line)
		if len(filtered) > 120 {
			filtered = filtered[1:]
		}
	}

	return filtered
}

func minecraftLogLineMatchesInstance(line string, instanceID string) bool {
	return strings.Contains(line, "instance="+instanceID) ||
		strings.Contains(line, "instance="+strconv.Quote(instanceID))
}

func statusClass(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "ok":
		return "status-ok"
	case "starting":
		return "status-pending"
	case "disabled", "stopped", "unsupported":
		return "status-muted"
	default:
		return "status-error"
	}
}

func (h *Handler) renderPage(w http.ResponseWriter, page string, data PageData) {
	tmpl := h.templateCache[page]
	if tmpl == nil {
		h.logger.Error("WebUI template not found in cache", "page", page)
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	var buffer bytes.Buffer

	if err := tmpl.ExecuteTemplate(&buffer, "base", data); err != nil {
		h.logger.Error("failed to render WebUI template", "error", err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(buffer.Bytes())
}

func formatUnix(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}

	return time.Unix(timestamp, 0).Local().Format("2006-01-02 15:04:05")
}

func (h *Handler) handleDatabasePage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.renderDatabasePage(w, r, PageData{
			Title:   "Database",
			Version: h.version,
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.flashDatabase(w, r, "", "invalid form")
		return
	}

	switch r.FormValue("_action") {
	case "create":
		h.databaseDoCreate(w, r)
	case "update":
		h.databaseDoUpdate(w, r)
	case "delete":
		h.databaseDoDelete(w, r)
	case "clear_cache":
		h.databaseDoClearCache(w, r)
	case "verify_signature":
		h.databaseDoVerifySignature(w, r)
	default:
		h.flashDatabase(w, r, "", "unknown action")
	}
}

func (h *Handler) databaseDoCreate(w http.ResponseWriter, r *http.Request) {
	entry, err := parseBanEntryForm(r)
	if err != nil {
		h.flashDatabase(w, r, "", err.Error())
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
		h.flashDatabase(w, r, "", err.Error())
		return
	}

	entry.Signature = signature

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if _, err := h.database.CreateBanEntry(ctx, entry); err != nil {
		h.flashDatabase(w, r, "", err.Error())
		return
	}

	h.flashDatabase(w, r, "ban entry created", "")
}

func (h *Handler) databaseDoUpdate(w http.ResponseWriter, r *http.Request) {
	entry, err := parseBanEntryForm(r)
	if err != nil {
		h.flashDatabase(w, r, "", err.Error())
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
		h.flashDatabase(w, r, "", err.Error())
		return
	}

	entry.Signature = signature

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.database.UpdateBanEntry(ctx, entry); err != nil {
		h.flashDatabase(w, r, "", err.Error())
		return
	}

	h.flashDatabase(w, r, "ban entry updated", "")
}

func (h *Handler) databaseDoDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		h.flashDatabase(w, r, "", "invalid ban entry id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.database.DeleteBanEntry(ctx, id); err != nil {
		h.flashDatabase(w, r, "", err.Error())
		return
	}

	h.flashDatabase(w, r, "ban entry deleted", "")
}

func (h *Handler) databaseDoClearCache(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.database.ClearAllPlayerDecisionCache(ctx); err != nil {
		h.flashDatabase(w, r, "", err.Error())
		return
	}

	h.flashDatabase(w, r, "player decision cache cleared", "")
}

func (h *Handler) databaseDoVerifySignature(w http.ResponseWriter, r *http.Request) {
	playerUUID := r.FormValue("player_uuid")
	reason := r.FormValue("reason")
	sourceNodeID := r.FormValue("source_node_id")
	signature := r.FormValue("signature")
	updatedAtStr := r.FormValue("updated_at")

	updatedAt, err := strconv.ParseInt(updatedAtStr, 10, 64)
	if err != nil {
		h.flashDatabase(w, r, "", "invalid updated_at timestamp")
		return
	}

	if err := h.identityService.VerifyBanSignature(playerUUID, reason, sourceNodeID, signature, updatedAt); err != nil {
		h.flashDatabase(w, r, "", "signature verification failed: "+err.Error())
		return
	}

	h.flashDatabase(w, r, "signature is valid - signed by local node "+h.identityService.Current().NodeID, "")
}

func (h *Handler) renderDatabasePage(w http.ResponseWriter, r *http.Request, data PageData) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Parse pagination and filter query parameters.
	query := r.URL.Query()
	perPage := 25
	if v := query.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			perPage = n
		}
	}
	page := 1
	if v := query.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}

	data.BanPerPage = perPage
	data.BanCurrentPage = page
	data.BanSearchUUID = query.Get("uuid")
	data.BanSearchName = query.Get("name")
	data.BanSearchSource = query.Get("source")
	data.BanSearchReason = query.Get("reason")

	filter := database.BanListFilter{
		PlayerUUID:   data.BanSearchUUID,
		PlayerName:   data.BanSearchName,
		SourceNodeID: data.BanSearchSource,
		UUIDSource:   data.BanSearchSource,
		Reason:       data.BanSearchReason,
		Limit:        perPage,
		Offset:       (page - 1) * perPage,
	}

	result, err := h.database.ListBanEntries(ctx, filter)
	if err != nil {
		data.ErrorMessage = err.Error()
	} else {
		data.BanEntries = result.Entries
		data.BanTotalCount = result.TotalCount
		if perPage > 0 {
			data.BanTotalPages = int((result.TotalCount + int64(perPage) - 1) / int64(perPage))
		}
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
		PlayerName: r.FormValue("player_name"),
		Reason:     r.FormValue("reason"),
		UUIDSource: r.FormValue("uuid_source"),
	}, nil
}

// flashMinecraft renders the Minecraft page with a flash message/error directly,
// avoiding a redirect that would cause the page to jump.
func (h *Handler) flashMinecraft(w http.ResponseWriter, msg string, errMsg string) {
	h.renderMinecraftPage(w, PageData{Title: "Minecraft", Version: h.version, Message: msg, ErrorMessage: errMsg})
}

// flashDatabase renders the Database page with a flash message/error directly.
func (h *Handler) flashDatabase(w http.ResponseWriter, r *http.Request, msg string, errMsg string) {
	h.renderDatabasePage(w, r, PageData{Title: "Database", Version: h.version, Message: msg, ErrorMessage: errMsg})
}

// flashIdentity renders the Identity page with a flash message/error directly.
func (h *Handler) flashIdentity(w http.ResponseWriter, msg string, errMsg string) {
	current := h.identityService.Current()
	h.renderPage(w, "identity.html", PageData{
		Title:         "Identity",
		Version:       h.version,
		LocalIdentity: &current,
		Message:       msg,
		ErrorMessage:  errMsg,
	})
}

// flashSecurity renders the Security page with a flash message/error directly.
func (h *Handler) flashSecurity(w http.ResponseWriter, msg string, errMsg string) {
	webToken := strings.TrimSpace(h.secretManager.Get(secrets.WebTokenEnv))
	keyPassphrase := strings.TrimSpace(h.secretManager.Get(secrets.KeyPassphraseEnv))
	h.renderPage(w, "security.html", PageData{
		Title:            "Security Settings",
		Version:          h.version,
		Config:           h.config,
		HasKeyPassphrase: keyPassphrase != "",
		HasWebToken:      webToken != "",
		WebToken:         webToken,
		Message:          msg,
		ErrorMessage:     errMsg,
	})
}

func (h *Handler) handleIdentityPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		current := h.identityService.Current()

		h.renderPage(w, "identity.html", PageData{
			Title:         "Identity",
			Version:       h.version,
			LocalIdentity: &current,
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.flashIdentity(w, "", "invalid form")
		return
	}

	switch r.FormValue("_action") {
	case "import":
		h.identityDoImport(w, r)
	case "new-keypair":
		h.identityDoNewKeyPair(w, r)
	default:
		h.flashIdentity(w, "", "unknown action")
	}
}

func (h *Handler) identityDoImport(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.FormValue("keypair_json"))
	if raw == "" {
		h.flashIdentity(w, "", "key pair json is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.identityService.ImportKeyPairJSON(ctx, []byte(raw)); err != nil {
		h.flashIdentity(w, "", err.Error())
		return
	}

	h.flashIdentity(w, "key pair imported", "")
}

func (h *Handler) identityDoNewKeyPair(w http.ResponseWriter, r *http.Request) {
	displayName := "MeshBan Node"
	if h.config != nil && strings.TrimSpace(h.config.Node.DisplayName) != "" {
		displayName = strings.TrimSpace(h.config.Node.DisplayName)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.identityService.CreateNewIdentity(ctx, displayName); err != nil {
		h.flashIdentity(w, "", err.Error())
		return
	}

	h.flashIdentity(w, "new key pair created", "")
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

func (h *Handler) handleSecuritySettingsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		webToken := strings.TrimSpace(h.secretManager.Get(secrets.WebTokenEnv))
		keyPassphrase := strings.TrimSpace(h.secretManager.Get(secrets.KeyPassphraseEnv))

		h.renderPage(w, "security.html", PageData{
			Title:            "Security Settings",
			Version:          h.version,
			Config:           h.config,
			HasKeyPassphrase: keyPassphrase != "",
			HasWebToken:      webToken != "",
			WebToken:         webToken,
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.flashSecurity(w, "", "invalid form")
		return
	}

	switch r.FormValue("_action") {
	case "webui":
		h.securityDoWebUI(w, r)
	case "passphrase":
		h.securityDoPassphrase(w, r)
	case "disable-encryption":
		h.securityDoDisableEncryption(w, r)
	case "token-regenerate":
		h.securityDoTokenRegenerate(w, r)
	case "token-update":
		h.securityDoTokenUpdate(w, r)
	default:
		h.flashSecurity(w, "", "unknown action")
	}
}

func (h *Handler) securityDoWebUI(w http.ResponseWriter, r *http.Request) {
	listen := strings.TrimSpace(r.FormValue("listen"))
	if listen == "" {
		h.flashSecurity(w, "", "listen address is required")
		return
	}

	h.config.WebUI.Listen = listen

	if err := config.Save(h.configPath, h.config); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	h.flashSecurity(w, "WebUI settings updated. Restart is required for listen address changes.", "")
}

func (h *Handler) securityDoPassphrase(w http.ResponseWriter, r *http.Request) {
	newPassphrase := strings.TrimSpace(r.FormValue("new_passphrase"))
	if newPassphrase == "" {
		h.flashSecurity(w, "", "new passphrase is required")
		return
	}

	keyOptions := identity.KeyOptions{
		EncryptPrivateKey: true,
		Passphrase:        newPassphrase,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.identityService.UpdateKeyProtection(ctx, keyOptions); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	if err := h.secretManager.Set(secrets.KeyPassphraseEnv, newPassphrase); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	h.config.Security.EncryptPrivateKey = true

	if err := config.Save(h.configPath, h.config); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	h.flashSecurity(w, "private key passphrase updated", "")
}

func (h *Handler) securityDoDisableEncryption(w http.ResponseWriter, r *http.Request) {
	currentPassphrase := h.secretManager.Get(secrets.KeyPassphraseEnv)

	keyOptions := identity.KeyOptions{
		EncryptPrivateKey: false,
		Passphrase:        currentPassphrase,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.identityService.UpdateKeyProtection(ctx, keyOptions); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	if err := h.secretManager.Delete(secrets.KeyPassphraseEnv); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	h.config.Security.EncryptPrivateKey = false

	if err := config.Save(h.configPath, h.config); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	h.flashSecurity(w, "private key encryption disabled", "")
}

func (h *Handler) securityDoTokenRegenerate(w http.ResponseWriter, r *http.Request) {
	newToken, err := h.secretManager.RegenerateRandom(secrets.WebTokenEnv, 16)
	if err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	// Keep the current browser logged in after the token changes.
	auth.SetSessionCookie(w, r, newToken)

	h.flashSecurity(w, "WebUI token regenerated", "")
}

func (h *Handler) securityDoTokenUpdate(w http.ResponseWriter, r *http.Request) {
	newToken := strings.TrimSpace(r.FormValue("web_token"))
	if newToken == "" {
		h.flashSecurity(w, "", "new WebUI token is required")
		return
	}

	if len(newToken) < 2 {
		h.flashSecurity(w, "", "WebUI token must be at least 2 characters")
		return
	}

	if len(newToken) > 128 {
		h.flashSecurity(w, "", "WebUI token must be at most 128 characters")
		return
	}

	if strings.ContainsAny(newToken, "\r\n\t ") {
		h.flashSecurity(w, "", "WebUI token must not contain whitespace")
		return
	}

	if err := h.secretManager.Set(secrets.WebTokenEnv, newToken); err != nil {
		h.flashSecurity(w, "", err.Error())
		return
	}

	// Keep the current browser logged in after the token changes.
	auth.SetSessionCookie(w, r, newToken)

	h.flashSecurity(w, "WebUI token updated", "")
}

func (h *Handler) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.renderPage(w, "login.html", PageData{
			Title:     "Login",
			Version:   h.version,
			LoginNext: safeLoginRedirect(r.URL.Query().Get("next")),
		})
		return

	case http.MethodPost:
		next := safeLoginRedirect(r.FormValue("next"))

		if retryAfter, locked := h.loginLimiter.IsLocked(r); locked {
			h.renderLoginError(w, next, "too many failed login attempts; try again in "+retryAfter.Round(time.Second).String())
			return
		}

		if err := r.ParseForm(); err != nil {
			h.renderLoginError(w, "/ui", "invalid form")
			return
		}

		providedToken := strings.TrimSpace(r.FormValue("token"))
		expectedToken := ""
		if h.secretManager != nil {
			expectedToken = strings.TrimSpace(h.secretManager.Get(secrets.WebTokenEnv))
		}

		if !auth.TokenMatches(providedToken, expectedToken) {
			message := "invalid token"
			if retryAfter, locked := h.loginLimiter.RecordFailure(r); locked {
				message = "too many failed login attempts; try again in " + retryAfter.Round(time.Second).String()
			}
			h.renderLoginError(w, next, message)
			return
		}

		h.loginLimiter.Reset(r)
		auth.SetSessionCookie(w, r, expectedToken)
		http.Redirect(w, r, next, http.StatusSeeOther)
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
}

// renderLoginError renders the login page with an error message directly.
func (h *Handler) renderLoginError(w http.ResponseWriter, next string, message string) {
	h.renderPage(w, "login.html", PageData{
		Title:        "Login",
		Version:      h.version,
		LoginNext:    next,
		ErrorMessage: message,
	})
}

func safeLoginRedirect(value string) string {
	value = strings.TrimSpace(value)

	if value == "" {
		return "/ui"
	}

	if !strings.HasPrefix(value, "/") {
		return "/ui"
	}

	if strings.HasPrefix(value, "//") {
		return "/ui"
	}

	return value
}
