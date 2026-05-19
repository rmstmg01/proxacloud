package clusters

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Provision(w http.ResponseWriter, r *http.Request) {
	var req ProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.Provision(req)
	if err != nil {
		if errors.Is(err, ErrClusterExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	status := http.StatusCreated
	if resp.Status == "provisioning" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, resp)
}

func (h *Handler) ProvisionAndBootstrap(w http.ResponseWriter, r *http.Request) {
	var req ProvisionAndBootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.ProvisionAndBootstrap(req)
	if err != nil {
		if errors.Is(err, ErrClusterExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, resp)
}

func (h *Handler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req BootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.Bootstrap(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	status := http.StatusOK
	if resp.Status == "bootstrapping" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, resp)
}

func (h *Handler) ListClusters(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.ListClusters()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CheckName(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	exists, err := h.service.ClusterNameExists(name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":   name,
		"exists": exists,
	})
}

func (h *Handler) GetCluster(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.GetCluster(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.ListRuns(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetLatestOperation(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.GetLatestOperation(clusterID)
	if err != nil {
		if errors.Is(err, ErrOperationNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListOperationEvents(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	operationID := int64(0)
	if raw := r.URL.Query().Get("operation_id"); raw != "" {
		operationID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid operation_id"})
			return
		}
	}

	sinceID := int64(0)
	if raw := r.URL.Query().Get("since_id"); raw != "" {
		sinceID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since_id"})
			return
		}
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}

	resp, err := h.service.ListOperationEvents(clusterID, operationID, sinceID, limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListClusterNodes(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.ListClusterNodes(clusterID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetBootstrapConfig(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.GetBootstrapConfig(clusterID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	out := map[string]any{
		"cluster_id":      resp.ClusterID,
		"ssh_user":        resp.SSHUser,
		"updated_at":      resp.UpdatedAt,
		"last_ssh_key_id": nil,
	}
	if resp.LastSSHKeyID.Valid {
		out["last_ssh_key_id"] = resp.LastSSHKeyID.Int64
	}

	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) ListBootstrapSSHKeys(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.ListBootstrapSSHKeys(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteCluster(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	if err := h.service.DeleteCluster(clusterID); err != nil {
		if errors.Is(err, ErrClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": clusterID, "deleted": true})
}

func (h *Handler) RestartCluster(w http.ResponseWriter, r *http.Request) {
	h.powerActionCluster(w, r, "restart")
}

func (h *Handler) StartCluster(w http.ResponseWriter, r *http.Request) {
	h.powerActionCluster(w, r, "start")
}

func (h *Handler) ShutdownCluster(w http.ResponseWriter, r *http.Request) {
	h.powerActionCluster(w, r, "shutdown")
}

func (h *Handler) RedeployCluster(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.RedeployCluster(clusterID)
	if err != nil {
		if errors.Is(err, ErrClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, resp)
}

func (h *Handler) powerActionCluster(w http.ResponseWriter, r *http.Request, action string) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.PowerActionCluster(clusterID, action)
	if err != nil {
		if errors.Is(err, ErrClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, resp)
}

func (h *Handler) Scale(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req ScaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.Scale(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Upgrade(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req UpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.Upgrade(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ReplaceNode(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req ReplaceNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.ReplaceNode(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RotateKubeconfig(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req RotateKubeconfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.RotateKubeconfig(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeployObservability(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req DeployObservabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.DeployObservability(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetObservability(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.GetObservability(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeployMetalLB(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req DeployMetalLBRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.DeployMetalLB(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeployIngressController(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req DeployIngressControllerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.DeployIngressController(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeployCertificateManager(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req DeployCertificateManagerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.DeployCertificateManager(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeployApp(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req DeployAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.DeployApp(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListApplicationManifests(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.ListApplicationManifests(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListApplicationManifestRevisions(w http.ResponseWriter, r *http.Request) {
	clusterID, appID, ok := parseClusterAndAppIDs(w, r)
	if !ok {
		return
	}

	resp, err := h.service.ListApplicationManifestRevisions(clusterID, appID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetApplicationManifestRevision(w http.ResponseWriter, r *http.Request) {
	clusterID, appID, ok := parseClusterAndAppIDs(w, r)
	if !ok {
		return
	}
	revisionID, err := strconv.ParseInt(chi.URLParam(r, "revisionID"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid revision id"})
		return
	}

	resp, err := h.service.GetApplicationManifestRevision(clusterID, appID, revisionID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RedeployApplicationManifest(w http.ResponseWriter, r *http.Request) {
	clusterID, appID, ok := parseClusterAndAppIDs(w, r)
	if !ok {
		return
	}
	var req RedeployAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.RedeployApplicationManifest(clusterID, appID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) StartKubectlCommand(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req KubectlCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.StartKubectlCommand(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (h *Handler) GetKubectlCommandRun(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	runID := chi.URLParam(r, "runID")
	resp, err := h.service.GetKubectlCommandRun(clusterID, runID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetMetalLB(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.GetMetalLB(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func parseClusterAndAppIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return 0, 0, false
	}
	appID, err := strconv.ParseInt(chi.URLParam(r, "appID"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid application id"})
		return 0, 0, false
	}
	return clusterID, appID, true
}

func (h *Handler) CreateEtcdSnapshot(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req CreateEtcdSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.CreateEtcdSnapshot(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListEtcdSnapshots(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.ListEtcdSnapshots(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RestoreEtcdSnapshot(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req RestoreEtcdSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	resp, err := h.service.RestoreEtcdSnapshot(clusterID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetClusterHealth(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	resp, err := h.service.GetClusterHealth(clusterID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetRecoveryConfig(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req ClusterRecoveryConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	req.ClusterID = clusterID
	if err := h.service.SetRecoveryConfig(clusterID, req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recovery config updated"})
}

func (h *Handler) TriggerNodeRecovery(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster id"})
		return
	}

	var req TriggerRecoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	// Use ReplaceNode to trigger recovery
	// For now, we'll use automatic Proxmox node selection
	replaceReq := ReplaceNodeRequest{
		FailedVMName:       req.VMName,
		NewProxmoxNodeName: "", // Will be auto-selected
	}

	resp, err := h.service.ReplaceNode(clusterID, replaceReq)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
