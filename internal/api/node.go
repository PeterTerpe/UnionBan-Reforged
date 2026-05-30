package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/identity"
)

// NodeIdentityResponse exposes a node's public identity and certificate so
// other nodes can verify signatures and assign a trust level.
type NodeIdentityResponse struct {
	NodeID      string `json:"node_id"`
	DisplayName string `json:"display_name"`
	Certificate string `json:"certificate"`
	CreatedAt   int64  `json:"created_at"`
}

// BanlistQueryResponse is the payload returned when another node queries the
// local banlist for a particular player UUID.
type BanlistQueryResponse struct {
	PlayerUUID string              `json:"player_uuid"`
	Entries    []database.BanEntry `json:"entries"`
	Count      int                 `json:"count"`
	Timestamp  int64               `json:"timestamp"`
}

// BanlistVersionResponse returns the current local banlist cache version so
// remote nodes can decide whether they need to re-query.
type BanlistVersionResponse struct {
	Version   string `json:"version"`
	Timestamp int64  `json:"timestamp"`
}

// --- Handlers ---------------------------------------------------------------

// handleNodeIdentity returns the local node's public identity and certificate.
//
//	GET /api/v1/node/identity
func (s *Server) handleNodeIdentity(w http.ResponseWriter, r *http.Request) {
	current := s.identityService.Current()

	cert := identity.Certificate{}
	if err := json.Unmarshal([]byte(current.Certificate), &cert); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to parse local certificate",
		})
		return
	}

	writeJSON(w, http.StatusOK, NodeIdentityResponse{
		NodeID:      current.NodeID,
		DisplayName: cert.DisplayName,
		Certificate: current.Certificate,
		CreatedAt:   current.CreatedAt,
	})
}

// handleNodeBanlistQuery returns all local banlist entries matching the given
// player UUID.  This is the primary inter-node query endpoint.
//
//	GET /api/v1/node/banlist?player_uuid=<uuid>
//
// The player_uuid query parameter is required.
func (s *Server) handlePlayerQuery(w http.ResponseWriter, r *http.Request) {
	playerUUID := strings.TrimSpace(r.URL.Query().Get("player_uuid"))
	if playerUUID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "player_uuid query parameter is required",
		})
		return
	}

	entries, err := s.database.ListBanEntriesByPlayerUUID(r.Context(), playerUUID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query banlist",
		})
		s.logger.Error("node banlist query failed", "player_uuid", playerUUID, "error", err)
		return
	}

	writeJSON(w, http.StatusOK, BanlistQueryResponse{
		PlayerUUID: database.NormalizePlayerUUID(playerUUID),
		Entries:    entries,
		Count:      len(entries),
		Timestamp:  time.Now().Unix(),
	})
}
