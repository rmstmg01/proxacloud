package proxacloudtunnelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

type Config struct {
	BaseURL  string
	APIToken string
}

type Tunnel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type Route struct {
	ID        string `json:"id"`
	TunnelID  string `json:"tunnelId"`
	Hostname  string `json:"hostname"`
	TargetURL string `json:"targetUrl"`
	TLSMode   string `json:"tlsMode"`
	Enabled   bool   `json:"enabled"`
}

type TCPRoute struct {
	ID             string `json:"id"`
	TunnelID       string `json:"tunnelId"`
	Name           string `json:"name"`
	PublicHostname string `json:"publicHostname"`
	PublicPort     int    `json:"publicPort"`
	TargetHost     string `json:"targetHost"`
	TargetPort     int    `json:"targetPort"`
	Enabled        bool   `json:"enabled"`
}

type TokenSummary struct {
	ID          string `json:"id"`
	TunnelID    string `json:"tunnelId"`
	Name        string `json:"name"`
	TokenPrefix string `json:"tokenPrefix"`
	Scope       string `json:"scope"`
	RouteID     string `json:"routeId"`
	TCPRouteID  string `json:"tcpRouteId"`
}

type TokenResponse struct {
	Token string       `json:"token"`
	Item  TokenSummary `json:"item"`
}

type CreateTunnelRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

type CreateHTTPRouteRequest struct {
	Hostname  string `json:"hostname"`
	TargetURL string `json:"targetUrl"`
	TLSMode   string `json:"tlsMode"`
	Enabled   bool   `json:"enabled"`
}

type CreateTCPRouteRequest struct {
	Name           string `json:"name"`
	PublicHostname string `json:"publicHostname"`
	PublicPort     int    `json:"publicPort"`
	TargetHost     string `json:"targetHost"`
	TargetPort     int    `json:"targetPort"`
	Enabled        bool   `json:"enabled"`
}

type CreateTokenRequest struct {
	Name       string `json:"name"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Scope      string `json:"scope,omitempty"`
	RouteID    string `json:"routeId,omitempty"`
	TCPRouteID string `json:"tcpRouteId,omitempty"`
}

func New(cfg Config) *Client {
	return &Client{
		baseURL:  strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiToken: strings.TrimSpace(cfg.APIToken),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != "" && c.apiToken != ""
}

func (c *Client) CreateTunnel(ctx context.Context, req CreateTunnelRequest) (Tunnel, error) {
	var out Tunnel
	if err := c.do(ctx, http.MethodPost, "/api/v1/tunnels", req, &out); err != nil {
		return Tunnel{}, err
	}
	return out, nil
}

func (c *Client) CreateHTTPRoute(ctx context.Context, tunnelID string, req CreateHTTPRouteRequest) (Route, error) {
	var out Route
	if err := c.do(ctx, http.MethodPost, "/api/v1/tunnels/"+tunnelID+"/routes", req, &out); err != nil {
		return Route{}, err
	}
	return out, nil
}

func (c *Client) CreateTCPRoute(ctx context.Context, tunnelID string, req CreateTCPRouteRequest) (TCPRoute, error) {
	var out TCPRoute
	if err := c.do(ctx, http.MethodPost, "/api/v1/tunnels/"+tunnelID+"/tcp-routes", req, &out); err != nil {
		return TCPRoute{}, err
	}
	return out, nil
}

func (c *Client) CreateToken(ctx context.Context, tunnelID string, req CreateTokenRequest) (TokenResponse, error) {
	var out TokenResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/tunnels/"+tunnelID+"/tokens", req, &out); err != nil {
		return TokenResponse{}, err
	}
	return out, nil
}

func (c *Client) DeleteTunnel(ctx context.Context, tunnelID string) error {
	return c.delete(ctx, "/api/v1/tunnels/"+tunnelID)
}

func (c *Client) DeleteHTTPRoute(ctx context.Context, routeID string) error {
	return c.delete(ctx, "/api/v1/routes/"+routeID)
}

func (c *Client) DeleteTCPRoute(ctx context.Context, routeID string) error {
	return c.delete(ctx, "/api/v1/tcp-routes/"+routeID)
}

func (c *Client) DeleteToken(ctx context.Context, tokenID string) error {
	return c.delete(ctx, "/api/v1/tokens/"+tokenID)
}

func (c *Client) delete(ctx context.Context, path string) error {
	if !c.Enabled() {
		return fmt.Errorf("proxacloud tunnel API is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build proxacloud tunnel delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call proxacloud tunnel API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("proxacloud tunnel API returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if !c.Enabled() {
		return fmt.Errorf("proxacloud tunnel API is not configured")
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode proxacloud tunnel request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build proxacloud tunnel request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call proxacloud tunnel API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("proxacloud tunnel API returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode proxacloud tunnel response: %w", err)
	}
	return nil
}
