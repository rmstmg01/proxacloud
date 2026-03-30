package proxmox

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	timeout time.Duration
}

func NewClient() *Client {
	return &Client{timeout: 15 * time.Second}
}

func (c *Client) DiscoverNodes(cred Credential) ([]Node, error) {
	base, err := url.Parse(strings.TrimSpace(cred.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	httpClient := &http.Client{
		Timeout: c.timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: cred.InsecureTLS},
		},
	}

	reqURL := *base
	reqURL.Path = path.Join(reqURL.Path, "/api2/json/nodes")

	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if cred.APITokenID != "" && cred.APITokenSecret != "" {
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", cred.APITokenID, cred.APITokenSecret))
	} else if cred.Username != "" && cred.Password != "" {
		return nil, errors.New("username/password discovery is not yet implemented; use API token")
	} else {
		return nil, errors.New("missing proxmox authentication data")
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
			Node   string `json:"node"`
			Status string `json:"status"`
			MaxCPU int    `json:"maxcpu"`
			MaxMem int64  `json:"maxmem"`
			Mem    int64  `json:"mem"`
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
			MemoryTotalBytes: item.MaxMem,
			MemoryUsedBytes:  item.Mem,
			Enabled:          true,
		})
	}

	return nodes, nil
}
