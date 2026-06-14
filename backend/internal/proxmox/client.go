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

	"github.com/gorilla/websocket"
)

type Client struct {
	timeout time.Duration
}

type qemuVMRef struct {
	VMID   int
	Name   string
	Status string
}

type containerRef struct {
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

func (c *Client) ListStorageContent(cred Credential, proxmoxNodeName string, storageID string, content string) ([]StorageContent, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	storageID = strings.TrimSpace(storageID)
	content = strings.TrimSpace(content)
	if proxmoxNodeName == "" {
		return nil, errors.New("proxmox node name is required")
	}
	if storageID == "" {
		return nil, errors.New("storage id is required")
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	httpClient := c.httpClient(cred)
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "storage", storageID, "content")
	if content != "" {
		query := reqURL.Query()
		query.Set("content", content)
		reqURL.RawQuery = query.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create storage content request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request proxmox storage content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("proxmox storage content api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data []StorageContent `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode proxmox storage content response: %w", err)
	}
	return payload.Data, nil
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

	if err := c.runVMAction(httpClient, base, cred, proxmoxNodeName, vm.VMID, actualAction, false, normalActionWait(actualAction)); err != nil {
		fallbackAction, fallbackSkipLock, fallbackOK := fallbackVMAction(actualAction)
		if !fallbackOK {
			if c.vmReachedDesiredState(httpClient, base, cred, proxmoxNodeName, vm.VMID, vmName, actualAction, finalStateWait(actualAction)) {
				return actualAction + "-verified", nil
			}
			return "", err
		}
		if c.vmReachedDesiredState(httpClient, base, cred, proxmoxNodeName, vm.VMID, vmName, actualAction, preFallbackStateWait(actualAction)) {
			return actualAction + "-verified", nil
		}
		if fallbackErr := c.runVMAction(httpClient, base, cred, proxmoxNodeName, vm.VMID, fallbackAction, fallbackSkipLock, fallbackActionWait(fallbackAction)); fallbackErr != nil {
			if c.vmReachedDesiredState(httpClient, base, cred, proxmoxNodeName, vm.VMID, vmName, fallbackAction, finalStateWait(fallbackAction)) {
				return fallbackAction + "-verified", nil
			}
			return "", fmt.Errorf("%s failed (%v); fallback %s failed: %w", actualAction, err, fallbackAction, fallbackErr)
		}
		return fallbackAction, nil
	}

	return actualAction, nil
}

func (c *Client) GetVMStatusByName(cred Credential, proxmoxNodeName string, vmName string) (string, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	vmName = strings.TrimSpace(vmName)
	if proxmoxNodeName == "" {
		return "", errors.New("proxmox node name is required")
	}
	if vmName == "" {
		return "", errors.New("vm name is required")
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

	status, err := c.getQEMUVMStatus(httpClient, base, cred, proxmoxNodeName, vm.VMID)
	if err != nil {
		return strings.TrimSpace(strings.ToLower(vm.Status)), nil
	}

	return strings.TrimSpace(strings.ToLower(status)), nil
}

func (c *Client) VMConsoleURLByName(cred Credential, proxmoxNodeName string, vmName string) (VMConsole, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	vmName = strings.TrimSpace(vmName)
	if proxmoxNodeName == "" {
		return VMConsole{}, errors.New("proxmox node name is required")
	}
	if vmName == "" {
		return VMConsole{}, errors.New("vm name is required")
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return VMConsole{}, fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	vm, err := c.findQEMUVM(httpClient, base, cred, proxmoxNodeName, vmName)
	if err != nil {
		return VMConsole{}, err
	}

	consoleURL := *base
	consoleURL.Path = "/"
	consoleURL.RawQuery = ""
	consoleURL.Fragment = ""
	query := consoleURL.Query()
	query.Set("console", "kvm")
	query.Set("novnc", "1")
	query.Set("vmid", strconv.Itoa(vm.VMID))
	query.Set("vmname", vm.Name)
	query.Set("node", proxmoxNodeName)
	query.Set("resize", "scale")
	query.Set("cmd", "")
	consoleURL.RawQuery = query.Encode()

	return VMConsole{
		VMID:     vm.VMID,
		NodeName: proxmoxNodeName,
		URL:      consoleURL.String(),
	}, nil
}

func (c *Client) ProxyVMConsoleByName(cred Credential, proxmoxNodeName string, vmName string, w http.ResponseWriter, r *http.Request) error {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	vmName = strings.TrimSpace(vmName)
	if proxmoxNodeName == "" {
		return errors.New("proxmox node name is required")
	}
	if vmName == "" {
		return errors.New("vm name is required")
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	vm, err := c.findQEMUVM(httpClient, base, cred, proxmoxNodeName, vmName)
	if err != nil {
		return err
	}
	session, err := c.createVMConsoleSession(httpClient, base, cred, proxmoxNodeName, vm)
	if err != nil {
		return err
	}
	upstream, err := c.dialVMConsoleWebSocket(httpClient, base, cred, session)
	if err != nil {
		return err
	}
	defer upstream.Close()

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"},
		CheckOrigin:  func(_ *http.Request) bool { return true },
	}
	downstream, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("upgrade project console websocket: %w", err)
	}
	defer downstream.Close()

	return proxyWebSockets(downstream, upstream)
}

func (c *Client) CreateVMConsoleSessionByName(cred Credential, proxmoxNodeName string, vmName string) (VMConsoleSession, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	vmName = strings.TrimSpace(vmName)
	if proxmoxNodeName == "" {
		return VMConsoleSession{}, errors.New("proxmox node name is required")
	}
	if vmName == "" {
		return VMConsoleSession{}, errors.New("vm name is required")
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return VMConsoleSession{}, fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	vm, err := c.findQEMUVM(httpClient, base, cred, proxmoxNodeName, vmName)
	if err != nil {
		return VMConsoleSession{}, err
	}
	return c.createVMConsoleSession(httpClient, base, cred, proxmoxNodeName, vm)
}

func (c *Client) ProxyVMConsoleSession(cred Credential, session VMConsoleSession, w http.ResponseWriter, r *http.Request) error {
	if session.VMID <= 0 {
		return errors.New("vmid is required")
	}
	if strings.TrimSpace(session.NodeName) == "" {
		return errors.New("proxmox node name is required")
	}
	if strings.TrimSpace(session.Port) == "" || strings.TrimSpace(session.Ticket) == "" {
		return errors.New("proxmox console session is incomplete")
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	upstream, err := c.dialVMConsoleWebSocket(httpClient, base, cred, session)
	if err != nil {
		return err
	}
	defer upstream.Close()

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"},
		CheckOrigin:  func(_ *http.Request) bool { return true },
	}
	downstream, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("upgrade project console websocket: %w", err)
	}
	defer downstream.Close()

	return proxyWebSockets(downstream, upstream)
}

func (c *Client) ControlContainerByName(cred Credential, proxmoxNodeName string, containerName string, action string) (string, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	containerName = strings.TrimSpace(containerName)
	action = strings.TrimSpace(strings.ToLower(action))
	if proxmoxNodeName == "" {
		return "", errors.New("proxmox node name is required")
	}
	if containerName == "" {
		return "", errors.New("container name is required")
	}
	if action != "shutdown" && action != "reboot" && action != "start" {
		return "", fmt.Errorf("unsupported container power action %q", action)
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	container, err := c.findLXCContainer(httpClient, base, cred, proxmoxNodeName, containerName)
	if err != nil {
		return "", err
	}

	actualAction := action
	status := strings.TrimSpace(strings.ToLower(container.Status))
	if action == "reboot" && status != "running" {
		actualAction = "start"
	}
	if action == "shutdown" && status != "running" {
		return "already-stopped", nil
	}
	if action == "start" && status == "running" {
		return "already-running", nil
	}

	if err := c.runContainerAction(httpClient, base, cred, proxmoxNodeName, container.VMID, actualAction, normalActionWait(actualAction)); err != nil {
		return "", err
	}
	return actualAction, nil
}

func (c *Client) GetContainerStatusByName(cred Credential, proxmoxNodeName string, containerName string) (string, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	containerName = strings.TrimSpace(containerName)
	if proxmoxNodeName == "" {
		return "", errors.New("proxmox node name is required")
	}
	if containerName == "" {
		return "", errors.New("container name is required")
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	container, err := c.findLXCContainer(httpClient, base, cred, proxmoxNodeName, containerName)
	if err != nil {
		return "", err
	}
	status, err := c.getLXCContainerStatus(httpClient, base, cred, proxmoxNodeName, container.VMID)
	if err != nil {
		return strings.TrimSpace(strings.ToLower(container.Status)), nil
	}
	return strings.TrimSpace(strings.ToLower(status)), nil
}

func (c *Client) ApplyVMFirewallByName(cred Credential, proxmoxNodeName string, vmName string, rules []FirewallRuleSpec, managedPrefix string) (string, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	vmName = strings.TrimSpace(vmName)
	managedPrefix = strings.TrimSpace(managedPrefix)
	if proxmoxNodeName == "" {
		return "", errors.New("proxmox node name is required")
	}
	if vmName == "" {
		return "", errors.New("vm name is required")
	}
	if managedPrefix == "" {
		return "", errors.New("managed firewall prefix is required")
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
	if err := c.applyGuestFirewall(httpClient, base, cred, proxmoxNodeName, "qemu", vm.VMID, rules, managedPrefix); err != nil {
		return "", err
	}
	return fmt.Sprintf("applied %d firewall rule(s) to VM %s (%d)", len(rules), vmName, vm.VMID), nil
}

func (c *Client) ApplyContainerFirewallByName(cred Credential, proxmoxNodeName string, containerName string, rules []FirewallRuleSpec, managedPrefix string) (string, error) {
	proxmoxNodeName = strings.TrimSpace(proxmoxNodeName)
	containerName = strings.TrimSpace(containerName)
	managedPrefix = strings.TrimSpace(managedPrefix)
	if proxmoxNodeName == "" {
		return "", errors.New("proxmox node name is required")
	}
	if containerName == "" {
		return "", errors.New("container name is required")
	}
	if managedPrefix == "" {
		return "", errors.New("managed firewall prefix is required")
	}

	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	httpClient := c.httpClient(cred)

	container, err := c.findLXCContainer(httpClient, base, cred, proxmoxNodeName, containerName)
	if err != nil {
		return "", err
	}
	if err := c.applyGuestFirewall(httpClient, base, cred, proxmoxNodeName, "lxc", container.VMID, rules, managedPrefix); err != nil {
		return "", err
	}
	return fmt.Sprintf("applied %d firewall rule(s) to container %s (%d)", len(rules), containerName, container.VMID), nil
}

func (c *Client) vmReachedDesiredState(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vmid int, vmName string, action string, maxWait time.Duration) bool {
	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}
	deadline := time.Now().Add(maxWait)
	for {
		status, err := c.getQEMUVMStatus(httpClient, base, cred, proxmoxNodeName, vmid)
		if err != nil {
			vm, findErr := c.findQEMUVM(httpClient, base, cred, proxmoxNodeName, vmName)
			if findErr == nil {
				status = vm.Status
			}
		}
		if vmStatusMatchesAction(status, action) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Second)
	}
}

func (c *Client) runContainerAction(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vmid int, action string, maxWait time.Duration) error {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "lxc", strconv.Itoa(vmid), "status", action)

	req, err := http.NewRequest(http.MethodPost, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create container %s request: %w", action, err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request proxmox container %s: %w", action, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox container %s api returned status %d: %s", action, resp.StatusCode, responseErrorMessage(resp))
	}

	var taskResp struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return fmt.Errorf("decode proxmox container %s task response: %w", action, err)
	}
	if strings.TrimSpace(taskResp.Data) == "" {
		return fmt.Errorf("proxmox container %s api returned an empty task id", action)
	}
	return c.waitForTask(httpClient, base, cred, proxmoxNodeName, taskResp.Data, action, maxWait)
}

func (c *Client) findLXCContainer(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, containerName string) (containerRef, error) {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "lxc")

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return containerRef{}, fmt.Errorf("create lxc list request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return containerRef{}, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return containerRef{}, fmt.Errorf("request proxmox lxc list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return containerRef{}, fmt.Errorf("proxmox lxc list api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data []struct {
			VMID   int    `json:"vmid"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return containerRef{}, fmt.Errorf("decode lxc list response: %w", err)
	}
	for _, item := range payload.Data {
		if strings.EqualFold(strings.TrimSpace(item.Name), containerName) {
			return containerRef{VMID: item.VMID, Name: item.Name, Status: item.Status}, nil
		}
	}
	return containerRef{}, fmt.Errorf("container %q not found on proxmox node %q", containerName, proxmoxNodeName)
}

func (c *Client) getLXCContainerStatus(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vmid int) (string, error) {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "lxc", strconv.Itoa(vmid), "status", "current")

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create container status request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request proxmox container status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxmox container status api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode container status response: %w", err)
	}
	return payload.Data.Status, nil
}

func vmStatusMatchesAction(status string, action string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	switch action {
	case "shutdown", "stop":
		return status == "stopped"
	case "start", "reboot", "reset":
		return status == "running"
	default:
		return false
	}
}

func fallbackVMAction(action string) (string, bool, bool) {
	switch action {
	case "shutdown":
		return "stop", true, true
	case "reboot":
		return "reset", true, true
	case "start":
		return "start", true, true
	default:
		return "", false, false
	}
}

func normalActionWait(action string) time.Duration {
	switch action {
	case "shutdown", "reboot":
		return 30 * time.Second
	default:
		return 2 * time.Minute
	}
}

func fallbackActionWait(action string) time.Duration {
	switch action {
	case "stop", "reset":
		return 2 * time.Minute
	default:
		return 3 * time.Minute
	}
}

func preFallbackStateWait(action string) time.Duration {
	switch action {
	case "shutdown", "reboot":
		return 45 * time.Second
	default:
		return 15 * time.Second
	}
}

func finalStateWait(action string) time.Duration {
	switch action {
	case "shutdown", "stop":
		return 5 * time.Minute
	case "reboot", "reset", "start":
		return 3 * time.Minute
	default:
		return 1 * time.Minute
	}
}

func (c *Client) runVMAction(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vmid int, action string, skipLock bool, maxWait time.Duration) error {
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
	if err := c.waitForTask(httpClient, base, cred, proxmoxNodeName, taskResp.Data, action, maxWait); err != nil {
		return err
	}

	return nil
}

func (c *Client) createVMConsoleSession(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vm qemuVMRef) (VMConsoleSession, error) {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "qemu", strconv.Itoa(vm.VMID), "vncproxy")

	form := url.Values{}
	form.Set("websocket", "1")

	req, err := http.NewRequest(http.MethodPost, reqURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return VMConsoleSession{}, fmt.Errorf("create vm console session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return VMConsoleSession{}, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return VMConsoleSession{}, fmt.Errorf("request proxmox vm console session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return VMConsoleSession{}, fmt.Errorf("proxmox vm console session api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data struct {
			Port   any    `json:"port"`
			Ticket string `json:"ticket"`
			Cert   string `json:"cert"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return VMConsoleSession{}, fmt.Errorf("decode proxmox vm console session response: %w", err)
	}
	port := consolePortString(payload.Data.Port)
	if port == "" || strings.TrimSpace(payload.Data.Ticket) == "" {
		return VMConsoleSession{}, errors.New("proxmox returned an incomplete vm console session")
	}

	return VMConsoleSession{
		VMID:     vm.VMID,
		NodeName: proxmoxNodeName,
		Port:     port,
		Ticket:   strings.TrimSpace(payload.Data.Ticket),
		Cert:     payload.Data.Cert,
	}, nil
}

func (c *Client) dialVMConsoleWebSocket(httpClient *http.Client, base *url.URL, cred Credential, session VMConsoleSession) (*websocket.Conn, error) {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", session.NodeName, "qemu", strconv.Itoa(session.VMID), "vncwebsocket")
	reqURL.RawQuery = ""
	reqURL.Fragment = ""
	switch reqURL.Scheme {
	case "https":
		reqURL.Scheme = "wss"
	case "http":
		reqURL.Scheme = "ws"
	default:
		return nil, fmt.Errorf("unsupported proxmox endpoint scheme %q", reqURL.Scheme)
	}
	query := reqURL.Query()
	query.Set("port", session.Port)
	query.Set("vncticket", session.Ticket)
	reqURL.RawQuery = query.Encode()

	headers, err := c.authenticationHeaders(httpClient, base, cred)
	if err != nil {
		return nil, err
	}

	dialer := websocket.Dialer{
		HandshakeTimeout:  c.timeout,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: cred.InsecureTLS},
		EnableCompression: false,
		Subprotocols:      []string{"binary"},
	}
	conn, resp, err := dialer.Dial(reqURL.String(), headers)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			return nil, fmt.Errorf("dial proxmox vm console websocket: status %d: %s", resp.StatusCode, responseErrorMessage(resp))
		}
		return nil, fmt.Errorf("dial proxmox vm console websocket: %w", err)
	}
	return conn, nil
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

func (c *Client) getQEMUVMStatus(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, vmid int) (string, error) {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, "qemu", strconv.Itoa(vmid), "status", "current")

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create vm status request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request proxmox vm status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxmox vm status api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode vm status response: %w", err)
	}

	return payload.Data.Status, nil
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

func consolePortString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.Itoa(int(typed))
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func (c *Client) waitForTask(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, upid string, action string, maxWait time.Duration) error {
	if maxWait <= 0 {
		maxWait = 2 * time.Minute
	}
	deadline := time.Now().Add(maxWait)
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

func (c *Client) applyGuestFirewall(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, guestKind string, vmid int, rules []FirewallRuleSpec, managedPrefix string) error {
	if err := c.setGuestFirewallEnabled(httpClient, base, cred, proxmoxNodeName, guestKind, vmid); err != nil {
		return err
	}
	if err := c.deleteManagedFirewallRules(httpClient, base, cred, proxmoxNodeName, guestKind, vmid, managedPrefix); err != nil {
		return err
	}
	for _, rule := range rules {
		if err := c.createGuestFirewallRule(httpClient, base, cred, proxmoxNodeName, guestKind, vmid, rule); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) setGuestFirewallEnabled(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, guestKind string, vmid int) error {
	form := url.Values{}
	form.Set("enable", "1")

	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, guestKind, strconv.Itoa(vmid), "firewall", "options")
	req, err := http.NewRequest(http.MethodPut, reqURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create firewall options request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request proxmox firewall options: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox firewall options api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}
	return nil
}

func (c *Client) deleteManagedFirewallRules(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, guestKind string, vmid int, managedPrefix string) error {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, guestKind, strconv.Itoa(vmid), "firewall", "rules")
	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create firewall rules list request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request proxmox firewall rules: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox firewall rules api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}

	var payload struct {
		Data []struct {
			Pos     int    `json:"pos"`
			Comment string `json:"comment"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode proxmox firewall rules response: %w", err)
	}

	for i := len(payload.Data) - 1; i >= 0; i-- {
		rule := payload.Data[i]
		if !strings.HasPrefix(rule.Comment, managedPrefix) {
			continue
		}
		if err := c.deleteGuestFirewallRule(httpClient, base, cred, proxmoxNodeName, guestKind, vmid, rule.Pos); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) deleteGuestFirewallRule(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, guestKind string, vmid int, pos int) error {
	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, guestKind, strconv.Itoa(vmid), "firewall", "rules", strconv.Itoa(pos))
	req, err := http.NewRequest(http.MethodDelete, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create firewall rule delete request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete proxmox firewall rule: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox firewall rule delete api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}
	return nil
}

func (c *Client) createGuestFirewallRule(httpClient *http.Client, base *url.URL, cred Credential, proxmoxNodeName string, guestKind string, vmid int, rule FirewallRuleSpec) error {
	form := url.Values{}
	form.Set("type", proxmoxFirewallDirection(rule.Direction))
	form.Set("action", proxmoxFirewallAction(rule.Action))
	form.Set("enable", proxmoxBool(rule.Enabled))
	if proto := strings.TrimSpace(strings.ToLower(rule.Protocol)); proto != "" && proto != "any" {
		form.Set("proto", proto)
	}
	if source := strings.TrimSpace(rule.SourceCIDR); source != "" {
		form.Set("source", source)
	}
	if dest := strings.TrimSpace(rule.DestinationCIDR); dest != "" {
		form.Set("dest", dest)
	}
	if rule.PortStart != nil {
		form.Set("dport", proxmoxFirewallPortRange(rule.PortStart, rule.PortEnd))
	}
	if comment := strings.TrimSpace(rule.ManagedComment); comment != "" {
		form.Set("comment", comment)
	}

	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes", proxmoxNodeName, guestKind, strconv.Itoa(vmid), "firewall", "rules")
	req, err := http.NewRequest(http.MethodPost, reqURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create firewall rule request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("create proxmox firewall rule: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox firewall rule create api returned status %d: %s", resp.StatusCode, responseErrorMessage(resp))
	}
	return nil
}

func proxmoxFirewallDirection(direction string) string {
	if strings.TrimSpace(strings.ToLower(direction)) == "outbound" {
		return "out"
	}
	return "in"
}

func proxmoxFirewallAction(action string) string {
	if strings.TrimSpace(strings.ToLower(action)) == "deny" {
		return "DROP"
	}
	return "ACCEPT"
}

func proxmoxBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func proxmoxFirewallPortRange(start *int, end *int) string {
	if start == nil {
		return ""
	}
	if end == nil || *end == *start {
		return strconv.Itoa(*start)
	}
	return fmt.Sprintf("%d:%d", *start, *end)
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

func (c *Client) authenticationHeaders(httpClient *http.Client, base *url.URL, cred Credential) (http.Header, error) {
	req, err := http.NewRequest(http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create proxmox authentication header request: %w", err)
	}
	if err := c.authorizeRequest(httpClient, base, req, cred); err != nil {
		return nil, err
	}
	return req.Header.Clone(), nil
}

func proxyWebSockets(downstream *websocket.Conn, upstream *websocket.Conn) error {
	errCh := make(chan error, 2)
	copyMessages := func(dst *websocket.Conn, src *websocket.Conn) {
		for {
			messageType, payload, err := src.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if err := dst.WriteMessage(messageType, payload); err != nil {
				errCh <- err
				return
			}
		}
	}

	go copyMessages(upstream, downstream)
	go copyMessages(downstream, upstream)

	err := <-errCh
	_ = downstream.Close()
	_ = upstream.Close()
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return nil
	}
	return err
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
