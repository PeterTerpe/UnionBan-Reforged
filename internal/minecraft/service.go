package minecraft

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/config"
	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/identity"
	"github.com/PeterTerpe/MeshBan/internal/secrets"
)

const (
	trustUltimate  = "ultimate"
	trustTrusted   = "trusted"
	trustFriend    = "friend"
	trustUnknown   = "unknown"
	trustUntrusted = "untrusted"
)

type Options struct {
	Config          config.MinecraftConfig
	Database        *database.Database
	IdentityService *identity.Service
	SecretManager   *secrets.Manager
	LocalNodeID     string
	Logger          *slog.Logger
}

type Service struct {
	config          config.MinecraftConfig
	database        *database.Database
	identityService *identity.Service
	secretManager   *secrets.Manager
	localNodeID     string
	logger          *slog.Logger

	lifecycleMu sync.Mutex
	rootCtx     context.Context
	started     bool
	group       *monitorGroup

	statusMu sync.RWMutex
	statuses map[string]ConnectorStatus

	rconErrorMu sync.RWMutex
	rconErrors  map[string]string
}

type monitorGroup struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type ConnectorStatus struct {
	ID              string
	Mode            string
	Enabled         bool
	Address         string
	State           string
	Message         string
	OnlinePlayers   int
	LastPollUnix    int64
	LastSuccessUnix int64
	LastErrorUnix   int64
	LastError       string
	StartedAtUnix   int64
}

type resolvedPolicy struct {
	kickMessage    string
	supportContact string
	thresholds     map[string]int
}

type playerDecision struct {
	Decision  string
	Reason    string
	PolicyMet string
	FromCache bool
}

func NewService(options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	service := &Service{
		config:          options.Config,
		database:        options.Database,
		identityService: options.IdentityService,
		secretManager:   options.SecretManager,
		localNodeID:     strings.TrimSpace(options.LocalNodeID),
		logger:          logger,
		statuses:        make(map[string]ConnectorStatus),
		rconErrors:      make(map[string]string),
	}

	service.initializeStatuses(options.Config)

	return service
}

func (s *Service) Run(ctx context.Context) {
	s.lifecycleMu.Lock()
	s.rootCtx = ctx
	s.started = true
	s.startMonitorsLocked(ctx)
	s.lifecycleMu.Unlock()

	<-ctx.Done()

	s.lifecycleMu.Lock()
	s.stopMonitorsLocked()
	s.started = false
	s.lifecycleMu.Unlock()
}

func (s *Service) ApplyConfig(cfg config.MinecraftConfig) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	s.config = cfg
	if !s.started || s.rootCtx == nil {
		s.initializeStatuses(cfg)
		return
	}

	s.stopMonitorsLocked()
	s.startMonitorsLocked(s.rootCtx)
}

func (s *Service) Statuses() []ConnectorStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()

	statuses := make([]ConnectorStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		statuses = append(statuses, status)
	}

	return statuses
}

func (s *Service) startMonitorsLocked(ctx context.Context) {
	cfg := s.config
	s.initializeStatuses(cfg)

	if !cfg.Enabled {
		s.logger.Info("Minecraft monitoring disabled")
		return
	}

	groupCtx, cancel := context.WithCancel(ctx)
	group := &monitorGroup{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	var wg sync.WaitGroup

	for _, instance := range cfg.Instances {
		instance := instance
		instanceID := instanceID(instance)

		if !instance.Enabled {
			continue
		}

		if strings.ToLower(strings.TrimSpace(instance.Mode)) != "rcon" {
			s.logger.Info("skipping non-RCON Minecraft instance", "instance", instanceID, "mode", instance.Mode)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.monitorRCON(groupCtx, cfg, instance)
		}()
	}

	go func() {
		wg.Wait()
		close(group.done)
	}()

	s.group = group
}

func (s *Service) stopMonitorsLocked() {
	if s.group == nil {
		return
	}

	s.group.cancel()
	<-s.group.done
	s.group = nil
}

func (s *Service) monitorRCON(ctx context.Context, cfg config.MinecraftConfig, instance config.MinecraftInstanceConfig) {
	instanceID := instanceID(instance)

	password := ""
	if s.secretManager != nil {
		password = strings.TrimSpace(s.secretManager.Get(instance.RCON.PasswordEnv))
	}

	if password == "" {
		s.updateStatus(instanceID, func(status *ConnectorStatus) {
			status.State = "error"
			status.Message = "RCON password is missing"
			status.LastError = "RCON password is missing"
			status.LastErrorUnix = time.Now().Unix()
		})
		s.logger.Error("RCON password is missing", "instance", instanceID, "password_env", instance.RCON.PasswordEnv)
		return
	}

	address := net.JoinHostPort(instance.RCON.Host, fmt.Sprintf("%d", instance.RCON.Port))
	commandTimeout := time.Duration(instance.RCON.CommandTimeoutSeconds) * time.Second
	if commandTimeout <= 0 {
		commandTimeout = 3 * time.Second
	}

	banPollInterval := time.Duration(ptrVal(instance.RCON.PollIntervalSeconds)) * time.Second
	if banPollInterval <= 0 {
		banPollInterval = time.Duration(cfg.BannedPlayersPollIntervalSeconds) * time.Second
	}
	if banPollInterval <= 0 {
		banPollInterval = 60 * time.Second
	}

	logInterval := time.Duration(ptrVal(instance.Log.PollIntervalSeconds)) * time.Second
	if logInterval <= 0 {
		logInterval = time.Duration(cfg.LogPollIntervalSeconds) * time.Second
	}
	if logInterval <= 0 {
		logInterval = time.Second
	}

	tailer := newLogTailer(instance.Log)
	if tailer.path == "" {
		s.updateStatus(instanceID, func(status *ConnectorStatus) {
			status.State = "error"
			status.Message = "Minecraft log path is missing"
			status.LastError = "Minecraft log path is missing"
			status.LastErrorUnix = time.Now().Unix()
		})
		s.logger.Error("Minecraft log path is missing", "instance", instanceID)
		return
	}

	client := NewRCONClient(address, password, commandTimeout)
	defer client.Close()

	policy := s.resolvePolicy(cfg.DefaultPolicy, instance.Policy)

	// Detect policy changes by comparing a hash stored in the database.
	// When the policy changes, clear all cached decisions for this server
	// so players are re-evaluated against the new thresholds immediately.
	policyHash := database.HashPolicy(policy.kickMessage, policy.supportContact, policy.thresholds)
	if storedHash, err := s.database.GetPolicyHash(ctx, instanceID); err == nil && storedHash != "" {
		if storedHash != policyHash {
			if err := s.database.ClearServerPlayerDecisionCache(ctx, instanceID); err != nil {
				s.logger.Warn("failed to clear decision cache after policy change", "instance", instanceID, "error", err)
			} else {
				s.logger.Info("cleared decision cache due to policy change", "instance", instanceID)
			}
			if err := s.database.SetPolicyHash(ctx, instanceID, policyHash); err != nil {
				s.logger.Warn("failed to store policy hash", "instance", instanceID, "error", err)
			}
		}
	} else {
		// First run: store the initial hash so future changes are detected.
		if err := s.database.SetPolicyHash(ctx, instanceID, policyHash); err != nil {
			s.logger.Warn("failed to store initial policy hash", "instance", instanceID, "error", err)
		}
	}

	resolverConfig := mergeUUIDResolverConfig(cfg.UUIDResolver, instance.UUIDResolver)
	resolver := NewUUIDResolver(resolverConfig, s.secretManager, s.logger)
	recentPlayers := make(map[string]Player)
	knownServerBans := make(map[string]ServerBan)
	logUUIDs := make(map[string]Player)
	onlinePlayers := make(map[string]Player)

	s.updateStatus(instanceID, func(status *ConnectorStatus) {
		status.State = "starting"
		status.Message = "Minecraft monitor is starting"
		status.StartedAtUnix = time.Now().Unix()
	})
	s.logger.Info("starting Minecraft monitor", "instance", instanceID, "address", address, "log_path", tailer.path, "log_interval", logInterval.String(), "ban_poll_interval", banPollInterval.String())

	// One-time startup health check and online player discovery.
	// Parse the list response to seed onlinePlayers with currently
	// connected players and evaluate each against the banlist policy.
	listResponse, err := client.Command(ctx, "list")
	if err != nil {
		s.setRCONError(instanceID, "RCON connection failed: "+err.Error())
		s.logger.Warn("RCON health check failed on startup", "instance", instanceID, "error", err)
	} else {
		s.setRCONError(instanceID, "")

		startupPlayers := parseListUUIDs(listResponse)
		if len(startupPlayers) == 0 {
			startupPlayers = parseListNames(listResponse)
		}

		for _, player := range startupPlayers {
			if player.UUID == "" {
				if uuid, ok := s.playerUUIDFromRCON(ctx, client, player.Name); ok {
					player.UUID = uuid
					player.UUIDSource = "official"
				} else if resolver != nil && resolver.config.Enabled {
					resolved, resolveErr := resolver.ResolveWithSource(ctx, player.Name)
					if resolveErr == nil {
						player.UUID = resolved.UUID
						player.UUIDSource = resolved.Source
					}
				}
			}

			if player.UUID == "" {
				s.logger.Warn("skipping online player at startup because UUID could not be resolved", "instance", instanceID, "player", player.Name)
				continue
			}

			key := strings.ToLower(player.Name)
			onlinePlayers[key] = player
			s.handleJoinedPlayer(ctx, instanceID, client, policy, player)
		}
	}

	s.readJoinLog(ctx, instanceID, tailer, client, resolver, policy, logUUIDs, recentPlayers, onlinePlayers)
	s.importServerBans(ctx, instanceID, instance.BannedPlayersPath, policy, knownServerBans)

	logTicker := time.NewTicker(logInterval)
	defer logTicker.Stop()

	banTicker := time.NewTicker(banPollInterval)
	defer banTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.updateStatus(instanceID, func(status *ConnectorStatus) {
				status.State = "stopped"
				status.Message = "Minecraft monitor stopped"
			})
			s.logger.Info("stopping Minecraft monitor", "instance", instanceID)
			return
		case <-logTicker.C:
			s.readJoinLog(ctx, instanceID, tailer, client, resolver, policy, logUUIDs, recentPlayers, onlinePlayers)
		case <-banTicker.C:
			s.importServerBans(ctx, instanceID, instance.BannedPlayersPath, policy, knownServerBans)
		}
	}
}

func (s *Service) readJoinLog(ctx context.Context, instanceID string, tailer *logTailer, client *RCONClient, resolver *UUIDResolver, policy resolvedPolicy, logUUIDs map[string]Player, recentPlayers map[string]Player, onlinePlayers map[string]Player) {
	lines, err := tailer.ReadNewLines()
	now := time.Now().Unix()
	if err != nil {
		s.updateStatus(instanceID, func(status *ConnectorStatus) {
			status.State = "error"
			status.Message = "Minecraft log tail failed"
			status.LastPollUnix = now
			status.LastErrorUnix = now
			status.LastError = err.Error()
		})
		s.logger.Error("failed to read Minecraft log", "instance", instanceID, "path", tailer.path, "error", err)
		return
	}

	for _, line := range lines {
		s.processLogLine(ctx, instanceID, line, client, resolver, policy, logUUIDs, recentPlayers, onlinePlayers)
	}

	rconError := s.getRCONError(instanceID)
	state := "ok"
	message := "log tail active"
	if rconError != "" {
		state = "error"
		message = rconError
	}

	s.updateStatus(instanceID, func(status *ConnectorStatus) {
		status.State = state
		status.Message = message
		status.OnlinePlayers = len(onlinePlayers)
		status.LastPollUnix = now
		status.LastSuccessUnix = now
		if rconError != "" {
			status.LastError = rconError
		} else {
			status.LastError = ""
		}
	})
}

func (s *Service) processLogLine(ctx context.Context, instanceID string, line string, client *RCONClient, resolver *UUIDResolver, policy resolvedPolicy, logUUIDs map[string]Player, recentPlayers map[string]Player, onlinePlayers map[string]Player) {
	if player, ok := parseLogUUID(line); ok {
		logUUIDs[strings.ToLower(player.Name)] = player
		return
	}

	if playerName, ok := parseLogLeave(line); ok {
		delete(onlinePlayers, strings.ToLower(playerName))
		return
	}

	playerName, ok := parseLogJoin(line)
	if !ok {
		return
	}

	key := strings.ToLower(playerName)
	if _, alreadyOnline := onlinePlayers[key]; alreadyOnline {
		return
	}

	player, ok := s.resolveJoinedPlayer(ctx, instanceID, client, resolver, playerName, logUUIDs)
	if !ok {
		s.logger.Warn("skipping joined player because UUID could not be resolved", "instance", instanceID, "player", playerName)
		return
	}

	recentPlayers[key] = player
	onlinePlayers[key] = player

	s.handleJoinedPlayer(ctx, instanceID, client, policy, player)
}

func (s *Service) resolveJoinedPlayer(ctx context.Context, instanceID string, client *RCONClient, resolver *UUIDResolver, playerName string, logUUIDs map[string]Player) (Player, bool) {
	key := strings.ToLower(playerName)
	if player, ok := logUUIDs[key]; ok && strings.TrimSpace(player.UUID) != "" {
		player.Name = playerName
		return player, true
	}

	if uuid, ok := s.playerUUIDFromRCON(ctx, client, playerName); ok {
		return Player{
			Name:       playerName,
			UUID:       uuid,
			UUIDSource: "official",
		}, true
	}

	if resolver != nil && resolver.config.Enabled {
		resolved, err := resolver.ResolveWithSource(ctx, playerName)
		if err == nil {
			return Player{
				Name:       playerName,
				UUID:       resolved.UUID,
				UUIDSource: resolved.Source,
			}, true
		}

		s.logger.Warn("failed to resolve joined player UUID", "instance", instanceID, "player", playerName, "error", err)
	}

	return Player{}, false
}

func (s *Service) handleJoinedPlayer(ctx context.Context, instanceID string, client *RCONClient, policy resolvedPolicy, player Player) {
	decision, err := s.decidePlayer(ctx, instanceID, player.Name, player.UUID, policy)
	if err != nil {
		s.logger.Error("failed to check Minecraft player policy", "instance", instanceID, "player", player.Name, "uuid", player.UUID, "error", err)
		return
	}

	if decision.Decision != database.PlayerDecisionKick {
		if !decision.FromCache {
			s.logger.Info("allowed Minecraft player", "instance", instanceID, "player", player.Name, "uuid", player.UUID)
		}
		return
	}

	if err := s.kickPlayer(ctx, client, player.Name, decision.Reason); err != nil {
		s.setRCONError(instanceID, "RCON kick failed: "+err.Error())
		s.logger.Error("failed to kick Minecraft player", "instance", instanceID, "player", player.Name, "uuid", player.UUID, "error", err)
		return
	}

	s.logger.Warn("kicked Minecraft player", "instance", instanceID, "player", player.Name, "uuid", player.UUID, "policy", decision.PolicyMet, "cached", decision.FromCache)
}

func (s *Service) importServerBans(ctx context.Context, instanceID string, bannedPlayersPath string, policy resolvedPolicy, knownServerBans map[string]ServerBan) {
	now := time.Now().Unix()

	// Read bans from banned-players.json — avoids performance impact on the
	// Minecraft server compared to querying the full banlist over RCON.
	bannedPlayers, err := loadBannedPlayersFile(bannedPlayersPath)
	if err != nil {
		s.logger.Warn("failed to read banned players JSON", "instance", instanceID, "path", bannedPlayersPath, "error", err)
		return
	}

	newBans := make(map[string]ServerBan, len(bannedPlayers))
	for key, serverBan := range bannedPlayers {
		if serverBan.UUID == "" {
			s.logger.Warn("skipping server ban with empty UUID", "instance", instanceID, "player", serverBan.Name)
			continue
		}

		newBans[key] = serverBan
		if _, alreadyKnown := knownServerBans[key]; alreadyKnown {
			continue
		}

		player := Player{
			Name:       serverBan.Name,
			UUID:       serverBan.UUID,
			UUIDSource: firstNonEmpty(serverBan.UUIDSource, "official"),
		}

		if err := s.importServerBan(ctx, instanceID, serverBan, player, policy); err != nil {
			s.logger.Error("failed to import server ban", "instance", instanceID, "player", serverBan.Name, "uuid", player.UUID, "error", err)
			continue
		}

		knownServerBans[key] = serverBan
	}

	// Clean up stale entries for bans that were removed from the file.
	for knownName, knownBan := range knownServerBans {
		if _, stillPresent := newBans[knownName]; stillPresent {
			continue
		}

		delete(knownServerBans, knownName)

		if knownBan.UUID == "" {
			continue
		}

		deleted, err := s.database.DeleteBanEntriesByPlayerUUIDAndSourceNodeID(ctx, knownBan.UUID, s.localNodeID)
		if err != nil {
			s.logger.Error("failed to delete removed server ban from database", "instance", instanceID, "player", knownBan.Name, "uuid", knownBan.UUID, "error", err)
			continue
		}

		if deleted > 0 {
			s.logger.Info("removed server ban entry from database", "instance", instanceID, "player", knownBan.Name, "uuid", knownBan.UUID, "deleted", deleted)

			// Remove stale cache entries so the next decidePlayer call
			// recomputes without the deleted ban.
			if err := s.database.DeletePlayerDecisionCache(ctx, instanceID, knownBan.UUID); err != nil {
				s.logger.Warn("failed to delete player decision cache after ban removal", "instance", instanceID, "player", knownBan.Name, "uuid", knownBan.UUID, "error", err)
			}
		}
	}

	s.updateStatus(instanceID, func(status *ConnectorStatus) {
		status.State = "ok"
		status.Message = "monitor active"
		status.LastPollUnix = now
		status.LastSuccessUnix = now
		status.LastError = ""
	})
}

func (s *Service) importServerBan(ctx context.Context, instanceID string, serverBan ServerBan, player Player, policy resolvedPolicy) error {
	entries, err := s.database.ListBanEntriesByPlayerUUID(ctx, player.UUID)
	if err != nil {
		return err
	}

	if hasLocalSourceBan(entries, s.localNodeID) {
		_, err := s.decidePlayer(ctx, instanceID, player.Name, player.UUID, policy)
		return err
	}

	reason := strings.TrimSpace(serverBan.Reason)
	if reason == "" {
		reason = "Banned on Minecraft server " + instanceID + " via RCON"
	}

	now := time.Now().Unix()
	sourceNodeID, signature, err := s.signImportedBan(player.UUID, reason, now)
	if err != nil {
		return err
	}

	if _, err := s.database.CreateBanEntry(ctx, database.BanEntry{
		PlayerUUID:   player.UUID,
		PlayerName:   player.Name,
		Reason:       reason,
		SourceNodeID: sourceNodeID,
		UUIDSource:   firstNonEmpty(player.UUIDSource, "official"),
		Signature:    signature,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return err
	}

	// Remove any stale "allow" cache so decidePlayer computes a fresh
	// decision against the updated ban list.
	if err := s.database.DeletePlayerDecisionCache(ctx, instanceID, player.UUID); err != nil {
		s.logger.Warn("failed to delete player decision cache after ban import", "instance", instanceID, "player", player.Name, "uuid", player.UUID, "error", err)
	}

	if _, err := s.decidePlayer(ctx, instanceID, player.Name, player.UUID, policy); err != nil {
		return err
	}

	s.logger.Info("imported Minecraft server ban", "instance", instanceID, "player", player.Name, "uuid", player.UUID, "uuid_source", player.UUIDSource)
	return nil
}

func (s *Service) signImportedBan(playerUUID string, reason string, updatedAt int64) (string, string, error) {
	if s.identityService == nil {
		return "", "", errors.New("identity service is unavailable")
	}

	current := s.identityService.Current()
	signature, err := s.identityService.SignLocalBan(playerUUID, reason, current.NodeID, updatedAt)
	if err != nil {
		return "", "", err
	}

	return current.NodeID, signature, nil
}

func hasLocalSourceBan(entries []database.BanEntry, localNodeID string) bool {
	for _, entry := range entries {
		sourceNodeID := strings.TrimSpace(entry.SourceNodeID)
		if sourceNodeID == "" || sourceNodeID == "local" || sourceNodeID == localNodeID {
			return true
		}
	}

	return false
}

func (s *Service) initializeStatuses(cfg config.MinecraftConfig) {
	statuses := make(map[string]ConnectorStatus)
	now := time.Now().Unix()

	if len(cfg.Instances) == 0 {
		s.replaceStatuses(statuses)
		return
	}

	for _, instance := range cfg.Instances {
		instanceID := instanceID(instance)
		state := "starting"
		message := "waiting for log tail"
		if !cfg.Enabled {
			state = "disabled"
			message = "Minecraft monitoring is disabled"
		} else if !instance.Enabled {
			state = "disabled"
			message = "connector is disabled"
		} else if strings.ToLower(strings.TrimSpace(instance.Mode)) != "rcon" {
			state = "unsupported"
			message = "only RCON connectors are implemented"
		}

		statuses[instanceID] = ConnectorStatus{
			ID:            instanceID,
			Mode:          firstNonEmpty(instance.Mode, "rcon"),
			Enabled:       cfg.Enabled && instance.Enabled,
			Address:       instanceAddress(instance),
			State:         state,
			Message:       message,
			StartedAtUnix: now,
		}
	}

	s.replaceStatuses(statuses)
}

func (s *Service) replaceStatuses(statuses map[string]ConnectorStatus) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	s.statuses = statuses
}

func (s *Service) updateStatus(instanceID string, update func(*ConnectorStatus)) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	status := s.statuses[instanceID]
	if status.ID == "" {
		status.ID = instanceID
	}

	update(&status)
	s.statuses[instanceID] = status
}

func (s *Service) getRCONError(instanceID string) string {
	s.rconErrorMu.RLock()
	defer s.rconErrorMu.RUnlock()

	return s.rconErrors[instanceID]
}

func (s *Service) setRCONError(instanceID string, value string) {
	s.rconErrorMu.Lock()
	defer s.rconErrorMu.Unlock()

	if value == "" {
		delete(s.rconErrors, instanceID)
	} else {
		s.rconErrors[instanceID] = value
	}
}

// CheckHealth performs a one-time connectivity and file accessibility check
// and updates the connector status.  It is meant to be called manually
// (e.g. from the WebUI).  It verifies:
//
//   - RCON connectivity (if the instance is configured for RCON)
//   - Minecraft log file is present and readable
//   - banned-players.json is present and readable (when path is set)
func (s *Service) CheckHealth(ctx context.Context, id string) error {
	var instance config.MinecraftInstanceConfig
	found := false
	for _, inst := range s.config.Instances {
		if instanceID(inst) == id {
			instance = inst
			found = true
			break
		}
	}
	if !found {
		return errors.New("instance not found")
	}

	now := time.Now().Unix()
	var failures []string
	var lastErr error

	// --- RCON connectivity ---
	password := ""
	if s.secretManager != nil {
		password = strings.TrimSpace(s.secretManager.Get(instance.RCON.PasswordEnv))
	}

	address := net.JoinHostPort(instance.RCON.Host, fmt.Sprintf("%d", instance.RCON.Port))
	client := NewRCONClient(address, password, 5*time.Second)

	if _, err := client.Command(ctx, "list"); err != nil {
		msg := "RCON: " + err.Error()
		failures = append(failures, msg)
		lastErr = err
		s.setRCONError(id, msg)
	} else {
		s.setRCONError(id, "")
	}
	client.Close()

	// --- Log file accessibility ---
	logPath := strings.TrimSpace(instance.Log.Path)
	if logPath == "" {
		failures = append(failures, "log: path is not configured")
	} else if _, err := os.Stat(logPath); err != nil {
		msg := "log: " + err.Error()
		failures = append(failures, msg)
		if lastErr == nil {
			lastErr = err
		}
	}

	// --- banned-players.json accessibility ---
	banPath := strings.TrimSpace(instance.BannedPlayersPath)
	if banPath != "" {
		if _, err := os.Stat(banPath); err != nil {
			msg := "banned-players: " + err.Error()
			failures = append(failures, msg)
			if lastErr == nil {
				lastErr = err
			}
		}
	}

	// --- Update status ---
	if len(failures) > 0 {
		msg := "health check: " + strings.Join(failures, "; ")
		s.updateStatus(id, func(status *ConnectorStatus) {
			status.State = "error"
			status.Message = msg
			status.LastPollUnix = now
			status.LastErrorUnix = now
			status.LastError = strings.Join(failures, "; ")
		})
		return fmt.Errorf("health check failed for %s: %s", id, strings.Join(failures, "; "))
	}

	s.updateStatus(id, func(status *ConnectorStatus) {
		status.State = "ok"
		status.Message = "health check passed"
		status.LastPollUnix = now
		status.LastSuccessUnix = now
		status.LastError = ""
	})
	return nil
}

func instanceID(instance config.MinecraftInstanceConfig) string {
	if id := strings.TrimSpace(instance.ID); id != "" {
		return id
	}

	return instanceAddress(instance)
}

func instanceAddress(instance config.MinecraftInstanceConfig) string {
	host := strings.TrimSpace(instance.RCON.Host)
	if host == "" {
		host = "127.0.0.1"
	}

	port := instance.RCON.Port
	if port == 0 {
		port = 25575
	}

	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

func (s *Service) playerUUIDFromRCON(ctx context.Context, client *RCONClient, playerName string) (string, bool) {
	if !isSafePlayerName(playerName) {
		return "", false
	}

	response, err := client.Command(ctx, "data get entity "+playerName+" UUID")
	if err != nil {
		return "", false
	}

	return parseEntityUUID(response)
}

func (s *Service) decidePlayer(ctx context.Context, instanceID string, playerName string, playerUUID string, policy resolvedPolicy) (playerDecision, error) {
	playerUUID = database.NormalizePlayerUUID(playerUUID)

	banlistVersion, err := s.database.BanlistCacheVersion(ctx)
	if err != nil {
		return playerDecision{}, err
	}

	cached, err := s.database.GetPlayerDecisionCache(ctx, instanceID, playerUUID, banlistVersion)
	if err == nil {
		return playerDecision{
			Decision:  cached.Decision,
			Reason:    cached.Reason,
			PolicyMet: cached.PolicyMet,
			FromCache: true,
		}, nil
	}

	if !database.IsPlayerDecisionCacheNotFound(err) {
		return playerDecision{}, err
	}

	entries, err := s.database.ListBanEntriesByPlayerUUID(ctx, playerUUID)
	if err != nil {
		return playerDecision{}, err
	}

	decision := s.evaluateBanEntries(entries, policy)

	if err := s.database.SavePlayerDecisionCache(ctx, database.PlayerDecisionCacheEntry{
		ServerID:       instanceID,
		PlayerUUID:     playerUUID,
		PlayerName:     playerName,
		Decision:       decision.Decision,
		Reason:         decision.Reason,
		PolicyMet:      decision.PolicyMet,
		BanlistVersion: banlistVersion,
	}); err != nil {
		return playerDecision{}, err
	}

	return decision, nil
}

func (s *Service) evaluateBanEntries(entries []database.BanEntry, policy resolvedPolicy) playerDecision {
	counts := map[string]int{
		trustUltimate:  0,
		trustTrusted:   0,
		trustFriend:    0,
		trustUnknown:   0,
		trustUntrusted: 0,
	}

	for _, entry := range entries {
		counts[s.trustLevelForBan(entry)]++
	}

	for _, level := range []string{trustUltimate, trustTrusted, trustFriend, trustUnknown, trustUntrusted} {
		threshold := policy.thresholds[level]
		if threshold <= 0 || counts[level] < threshold {
			continue
		}

		policyMet := fmt.Sprintf("%s bans %d/%d", level, counts[level], threshold)

		return playerDecision{
			Decision:  database.PlayerDecisionKick,
			Reason:    formatKickMessage(policy, policyMet),
			PolicyMet: policyMet,
		}
	}

	return playerDecision{
		Decision:  database.PlayerDecisionAllow,
		Reason:    "no kick policy matched",
		PolicyMet: "none",
	}
}

func (s *Service) trustLevelForBan(entry database.BanEntry) string {
	sourceNodeID := strings.TrimSpace(entry.SourceNodeID)
	if sourceNodeID == "" || sourceNodeID == "local" || sourceNodeID == s.localNodeID {
		return trustUltimate
	}

	return trustUnknown
}

func (s *Service) kickPlayer(ctx context.Context, client *RCONClient, playerName string, message string) error {
	if !isSafePlayerName(playerName) {
		return errors.New("unsafe player name")
	}

	_, err := client.Command(ctx, "kick "+playerName+" "+sanitizeKickMessage(message))
	return err
}

func (s *Service) resolvePolicy(base config.MinecraftPolicyConfig, override config.MinecraftPolicyConfig) resolvedPolicy {
	kickMessage := firstNonEmpty(override.KickMessage, override.KickReason, base.KickMessage, base.KickReason)
	supportContact := firstNonEmpty(override.SupportContact, base.SupportContact)

	policy := resolvedPolicy{
		kickMessage:    kickMessage,
		supportContact: resolveSupportContact(s.secretManager, supportContact),
		thresholds: map[string]int{
			trustUltimate:  thresholdValue(override.Ultimate, base.Ultimate, 1),
			trustTrusted:   thresholdValue(override.Trusted, base.Trusted, 2),
			trustFriend:    thresholdValue(override.Friend, base.Friend, 5),
			trustUnknown:   thresholdValue(override.Unknown, base.Unknown, 20),
			trustUntrusted: thresholdValue(override.Untrusted, base.Untrusted, 0),
		},
	}

	if strings.TrimSpace(policy.kickMessage) == "" {
		policy.kickMessage = "You have been kicked by MeshBan: {policy_met}."
	}

	return policy
}

func formatKickMessage(policy resolvedPolicy, policyMet string) string {
	message := strings.ReplaceAll(policy.kickMessage, "{policy_met}", policyMet)
	message = strings.ReplaceAll(message, "{support_contact}", policy.supportContact)

	return sanitizeKickMessage(message)
}

func thresholdValue(override *int, base *int, fallback int) int {
	if override != nil {
		return *override
	}

	if base != nil {
		return *base
	}

	return fallback
}

func ptrVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
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

func resolveSupportContact(secretManager *secrets.Manager, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if secretManager == nil {
		return value
	}

	if secretValue := strings.TrimSpace(secretManager.Get(value)); secretValue != "" {
		return secretValue
	}

	return value
}

func mergeUUIDResolverConfig(base config.MinecraftUUIDResolverConfig, override config.MinecraftUUIDResolverConfig) config.MinecraftUUIDResolverConfig {
	merged := base

	if override.Enabled {
		merged.Enabled = true
	}

	if override.Endpoint != "" {
		merged.Endpoint = override.Endpoint
	}

	if override.ResponseUUIDField != "" {
		merged.ResponseUUIDField = override.ResponseUUIDField
	}

	if override.TimeoutSeconds > 0 {
		merged.TimeoutSeconds = override.TimeoutSeconds
	}

	if override.RetryCount > 0 {
		merged.RetryCount = override.RetryCount
	}

	if override.ProxyType != "" {
		merged.ProxyType = override.ProxyType
	}

	if override.ProxyURLEnv != "" {
		merged.ProxyURLEnv = override.ProxyURLEnv
	}

	if override.ProxyURL != "" {
		merged.ProxyURL = override.ProxyURL
	}

	if override.ProxyAuth {
		merged.ProxyAuth = true
	}

	if override.ProxyUsernameEnv != "" {
		merged.ProxyUsernameEnv = override.ProxyUsernameEnv
	}

	if override.ProxyPassEnv != "" {
		merged.ProxyPassEnv = override.ProxyPassEnv
	}

	return merged
}
