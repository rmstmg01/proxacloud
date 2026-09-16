package regions

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
	"proxacloud/backend/internal/tenant"
)

type AccessPolicy struct {
	Mode      string  `json:"mode"`
	RegionIDs []int64 `json:"region_ids"`
}

func (p AccessPolicy) Allows(id int64) bool {
	if p.Mode == "all" {
		return true
	}
	if p.Mode == "selected" {
		for _, allowed := range p.RegionIDs {
			if allowed == id {
				return true
			}
		}
	}
	return false
}

// Visibility is independent of access: keep every region in the response.
func annotateAccess(regions []Region, policy AccessPolicy) {
	for i := range regions {
		region := &regions[i]
		region.CanDeploy = false
		region.DeploymentUnavailableReason = ""
		switch {
		case !region.Enabled:
			region.DeploymentUnavailableReason = "Region disabled"
		case !policy.Allows(region.ID):
			region.DeploymentUnavailableReason = "Not permitted"
		default:
			region.CanDeploy = true
		}
	}
}

func (s *Service) Policy(userID int64) (AccessPolicy, error) {
	p := AccessPolicy{RegionIDs: []int64{}}
	var ids pq.Int64Array
	err := s.db.QueryRow(`SELECT region_access_mode, ARRAY(SELECT region_id FROM user_allowed_regions WHERE user_id = u.id ORDER BY region_id) FROM users u WHERE id = $1`, userID).Scan(&p.Mode, &ids)
	if err != nil {
		return p, err
	}
	p.RegionIDs = append(p.RegionIDs, ids...)
	return p, nil
}

func (s *Service) SavePolicy(userID int64, p AccessPolicy) error {
	if p.Mode != "all" && p.Mode != "selected" && p.Mode != "none" {
		return errors.New("invalid region access mode")
	}
	if p.Mode != "selected" && len(p.RegionIDs) != 0 {
		return errors.New("region_ids require selected mode")
	}
	seen := map[int64]bool{}
	for _, id := range p.RegionIDs {
		if id <= 0 || seen[id] {
			return errors.New("region_ids must contain unique positive IDs")
		}
		seen[id] = true
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize edits to a user's policy; readers see either complete version.
	var existing int64
	if err = tx.QueryRow(`SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&existing); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM regions WHERE id = ANY($1)`, pq.Array(p.RegionIDs)).Scan(&count); err != nil {
		return err
	}
	if count != len(p.RegionIDs) {
		return errors.New("one or more regions do not exist")
	}
	if _, err = tx.Exec(`UPDATE users SET region_access_mode = $2 WHERE id = $1`, userID, p.Mode); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM user_allowed_regions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, id := range p.RegionIDs {
		if _, err = tx.Exec(`INSERT INTO user_allowed_regions (user_id, region_id) VALUES ($1, $2)`, userID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RequireDeployment checks the actor, never a client-supplied account or user ID.
// Call before allocating resources or submitting provisioning work.
func RequireDeployment(w http.ResponseWriter, db *sql.DB, scope tenant.Scope, regionID int64) bool {
	if scope.IsAdmin() {
		return true
	}
	if regionID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "region_id is required"})
		return false
	}
	var allowed bool
	err := db.QueryRow(`SELECT EXISTS (
 SELECT 1 FROM users u JOIN regions r ON r.id = $2
 WHERE u.username = $1 AND r.enabled = TRUE AND
 (u.region_access_mode = 'all' OR (u.region_access_mode = 'selected' AND EXISTS
 (SELECT 1 FROM user_allowed_regions a WHERE a.user_id = u.id AND a.region_id = r.id))))`, scope.Username, regionID).Scan(&allowed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to verify region permission"})
		return false
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "deployment is not permitted in this region", "code": "region_not_allowed"})
	}
	return allowed
}

func (h *Handler) GetUserAccess(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, 400, map[string]string{"error": "invalid user id"})
		return
	}
	policy, err := h.service.Policy(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load region permissions"})
		return
	}
	writeJSON(w, 200, policy)
}

func (h *Handler) SetUserAccess(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, 400, map[string]string{"error": "invalid user id"})
		return
	}
	var policy AccessPolicy
	if err = json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request payload"})
		return
	}
	if err = h.service.SavePolicy(id, policy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "user not found"})
			return
		}
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if policy.RegionIDs == nil {
		policy.RegionIDs = []int64{}
	}
	writeJSON(w, 200, policy)
}
