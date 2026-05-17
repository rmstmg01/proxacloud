package proxmox

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	timeout time.Duration
}

type qemuVMRef struct {
	VMID   int
	Name   string
	Status string
}

func NewClient() *Client {
	return &Client{timeout: 15 * time.Second}
}

func (c *Client) DiscoverNodes(cred Credential) ([]Node, error) {
	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	httpClient := c.httpClient(cred)

	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes")

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request proxmox nodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("proxmox nodes api returned status %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			Node    string  `json:"node"`
			Status  string  `json:"status"`
			MaxCPU  int     `json:"maxcpu"`
			CPU     float64 `json:"cpu"`
			MaxMem  int64   `json:"maxmem"`
			Mem     int64   `json:"mem"`
			MaxDisk int64   `json:"maxdisk"`
			Disk    int64   `json:"disk"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode proxmox response: %w", err)
	}

	nodes := make([]Node, 0, len(payload.Data))
	for _, item := range payload.Data {
		nodes = append(nodes, Node{
			NodeName:         item.Node,
			Status:           item.Status,
			CPUCount:         item.MaxCPU,
			CPUUsage:         item.CPU,
			MemoryTotalBytes: item.MaxMem,
			MemoryUsedBytes:  item.Mem,
			DiskTotalBytes:   item.MaxDisk,
			DiskUsedBytes:    item.Disk,
			Enabled:          true,
		})
	}

	return nodes, nil
}

func (c *Client) ControlVMByName(cred Credential, proxmoxNodeName string, vmName string, action string) (string, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	vmName = strings.TrimSpace(vmName)
	action = strings.TrimSpace(strings.ToLower(action))
	if proxmoxNodeName == "" {
		return "", errors.New("proxmox node name is required")
	}
	if vmName == "" {
		return "", errors.New("vm name is required")
	}
	if action != "shutdown" && action != "reboot" && action != "start" {
		return "", fmt.Errorf("unsupported vm power action %q", action)
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	vm, err := c.findQEMUVM(httpClient, base, cred, proxmoxNodeName, vmName)
	if err != nil {
		return "", err
	}

	actualAction := action
	vmStatus := strings.TrimSpace(strings.ToLower(vm.Status))
	if action == "reboot" && vmStatus != "running" {
		actualAction = "start"
	}
	if action == "shutdown" && vmStatus != "running" {
		return "already-stopped", nil
	}
	if action == "start" && vmStatus == "running" {
		return "already-running", nil
	}

	if err := c.runVMAction(httpClient, base, cred, proxmoxNodeName, vm.VMID, actualAction, false); err != nil {
		fallbackAction, fallbackSkipLock, fallbackOK := fallbackVMAction(actualAction)
		if !fallbackOK {
			return "", err
		}
		if fallbackErr := c.runVMAction(httpClient, base, cred, proxmoxNodeName, vm.VMID, fallbackAction, fallbackSkipLock); fallbackErr != nil {
			return "", fmt.Errorf("%s failed (%v); fallback %s failed: %w", actualAction, err, fallbackAction, fallbackErr)
		}
		return fallbackAction, nil
	}

	return actualAction, nil
}

func fallbackVMAction(action string) (string, bool, bool) {
	switch action {
	case "shutdown":
		return "stop", false, true
	case "reboot":
		return "reset", false, true
	case "start":
		return "start", true, true
	default:
		return "", false, false
	}
}

func (c *Client) runVMAction(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vmid int, action string, skipLock bool) error {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "qemu", strconv.Itoa(vmid), "status", action)

	var body io.Reader
	if skipLock {
		form := url.Values{}
		form.Set("skiplock", "1")
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequest(http.MethodPost, reqURL.String(), body)
	if err != nil {
		return fmt.Errorf("create vm %s request: %w", action, err)
	}
	if skipLock {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request proxmox vm %s: %w", action, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox vm %s api returned status %d: %s", action, resp.StatusCode, responseErrorMessage(resp))
	}

	var taskResp struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return fmt.Errorf("decode proxmox vm %s task response: %w", action, err)
	}
	if strings.TrimSpace(taskResp.Data) == "" {
		return fmt.Errorf("proxmox vm %s api returned an empty task id", action)
	}
	if err := c.waitForTask(httpClient, base, cred, proxmoxNodeName, taskResp.Data, action); err != nil {
		return err
	}

	return nil
}

func (c *Client) findQEMUVM(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vmName string) (qemuVMRef, error) {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "qemu")

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return qemuVMRef{}, fmt.Errorf("create qemu list request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return qemuVMRef{}, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return qemuVMRef{}, fmt.Errorf("request proxmox qemu list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return qemuVMRef{}, fmt.Errorf("proxmox qemu list api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data []struct {
			VMID   int    `json:"vmid"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return qemuVMRef{}, fmt.Errorf("decode qemu list response: %w", err)
	}

	for _, item := range payload.Data {
		if strings.EqualFold(strings.TrimSpace(item.Name), vmName) {
			return qemuVMRef{
				VMID:   item.VMID,
				Name:   item.Name,
				Status: item.Status,
			}, nil
		}
	}

	return qemuVMRef{}, fmt.Errorf("vm %q not found on proxmox node %q", vmName, proxmoxNodeName)
}

func responseErrorMessage(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "unable to read response body"
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return "empty response body"
	}
	return msg
}

func (c *Client) waitForTask(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, upid string, action string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		status, exitStatus, err := c.getTaskStatus(httpClient, base, cred, proxmoxNodeName, upid)
		if err != nil {
			return err
		}

		if strings.EqualFold(status, "stopped") {
			if exitStatus == "" || strings.EqualFold(exitStatus, "OK") {
				return nil
			}
			return fmt.Errorf("proxmox vm %s task failed: %s", action, exitStatus)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for proxmox vm %s task %s to complete", action, upid)
		}
		time.Sleep(2 * time.Second)
	}
}

func (c *Client) getTaskStatus(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, upid string) (string, string, error) {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "tasks", upid, "status")

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", "", fmt.Errorf("create task status request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return "", "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request proxmox task status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("proxmox task status api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("decode task status response: %w", err)
	}

	return strings.TrimSpace(payload.Data.Status), strings.TrimSpace(payload.Data.ExitStatus), nil
}

func (c *Client) httpClient(cred Credential) *http.Client {
	return &http.Client{
		Timeout: c.timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: cred.InsecureTLS},
		},
	}
}

func (c *Client) authorizeRequest(httpClient *http.Client, base *url.URL, req *http.Request, cred Credential) error {
	if cred.APITokenID != "" && cred.APITokenSecret != "" {
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", cred.APITokenID, cred.APITokenSecret))
		return nil
	}
	if cred.Username != "" && cred.Password != "" {
		ticket, csrfToken, err := c.getTicket(httpClient, base, cred.Username, cred.Password)
		if err != nil {
			return fmt.Errorf("authenticate with proxmox: %w", err)
		}
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
		req.Header.Set("CSRFPreventionToken", csrfToken)
		return nil
	}
	return errors.New("missing proxmox authentication data")
}

// getTicket authenticates with the Proxmox API using username/password and
// returns the PVEAuthCookie ticket and CSRFPreventionToken.
func (c *Client) getTicket(httpClient *http.Client, base *url.URL, username, password string) (string, string, error) {
	ticketURL := *base
	ticketURL.Path = path.Join(ticketURL.Path, "/api2/json/access/ticket")

	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)

	req, err := http.NewRequest(http.MethodPost, ticketURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("create ticket request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("ticket request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", "", errors.New("invalid username or password")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("ticket endpoint returned status %d", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			Ticket              string `json:"ticket"`
			CSRFPreventionToken string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("decode ticket response: %w", err)
	}
	if payload.Data.Ticket == "" {
		return "", "", errors.New("proxmox returned an empty ticket")
	}
	return payload.Data.Ticket, payload.Data.CSRFPreventionToken, nil
}
