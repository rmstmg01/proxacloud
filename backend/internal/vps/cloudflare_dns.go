package vps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const cloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"

type cloudflareDNSResult struct {
	RecordID   string
	RecordType string
	Content    string
	Proxied    bool
	Status     string
}

type cloudflareDNSRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type cloudflareDNSResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Proxied bool   `json:"proxied"`
	} `json:"result"`
	Errors []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (s *Service) ensureCloudflareDNSRecord(ctx context.Context, attachment TunnelAttachment) (cloudflareDNSResult, error) {
	cfg := s.tunnelConfig.CloudflareDNS
	if !cfg.Enabled {
		return cloudflareDNSResult{Status: "disabled"}, nil
	}
	if cfg.APIToken == "" || cfg.ZoneID == "" {
		return cloudflareDNSResult{}, errors.New("Cloudflare API token or Zone ID not configured")
	}
	if cfg.Target == "" {
		return cloudflareDNSResult{}, errors.New("CLOUDFLARE_DNS_TARGET is required when Cloudflare DNS is enabled")
	}

	recordType := strings.ToUpper(strings.TrimSpace(cfg.RecordType))
	if recordType == "" {
		recordType = cloudflareRecordTypeForTarget(cfg.Target)
	}
	content := strings.TrimSpace(cfg.Target)
	if recordType == "CNAME" {
		content = strings.TrimSuffix(content, ".")
	}

	if attachment.CloudflareDNSRecordID != "" &&
		strings.EqualFold(attachment.CloudflareDNSRecordType, recordType) &&
		attachment.CloudflareDNSRecordContent == content &&
		attachment.CloudflareDNSProxied == cfg.Proxied {
		return cloudflareDNSResult{
			RecordID:   attachment.CloudflareDNSRecordID,
			RecordType: recordType,
			Content:    content,
			Proxied:    cfg.Proxied,
			Status:     "active",
		}, nil
	}

	if attachment.CloudflareDNSRecordID != "" {
		if err := s.deleteCloudflareDNSRecord(ctx, attachment.CloudflareDNSRecordID); err != nil {
			return cloudflareDNSResult{}, fmt.Errorf("delete old Cloudflare DNS record: %w", err)
		}
	}

	recordID, err := s.createCloudflareDNSRecord(ctx, attachment.PublicHostname, recordType, content, cfg.Proxied)
	if err != nil {
		return cloudflareDNSResult{}, err
	}
	return cloudflareDNSResult{
		RecordID:   recordID,
		RecordType: recordType,
		Content:    content,
		Proxied:    cfg.Proxied,
		Status:     "active",
	}, nil
}

func (s *Service) createCloudflareDNSRecord(ctx context.Context, fqdn, recordType, content string, proxied bool) (string, error) {
	cfg := s.tunnelConfig.CloudflareDNS
	payload, err := json.Marshal(cloudflareDNSRequest{
		Type:    recordType,
		Name:    strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), "."),
		Content: content,
		TTL:     1,
		Proxied: proxied,
	})
	if err != nil {
		return "", fmt.Errorf("encode Cloudflare DNS request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudflareAPIBaseURL+"/zones/"+cfg.ZoneID+"/dns_records", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create Cloudflare request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Cloudflare API: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var cfResp cloudflareDNSResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return "", fmt.Errorf("failed to parse Cloudflare response: %w", err)
	}
	if !cfResp.Success {
		return "", fmt.Errorf("%s", cloudflareErrorMessage(cfResp))
	}
	return cfResp.Result.ID, nil
}

func (s *Service) deleteCloudflareDNSRecord(ctx context.Context, recordID string) error {
	cfg := s.tunnelConfig.CloudflareDNS
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, cloudflareAPIBaseURL+"/zones/"+cfg.ZoneID+"/dns_records/"+recordID, nil)
	if err != nil {
		return fmt.Errorf("failed to create Cloudflare delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Cloudflare API: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var cfResp cloudflareDNSResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return fmt.Errorf("failed to parse Cloudflare delete response: %w", err)
	}
	if !cfResp.Success {
		return fmt.Errorf("%s", cloudflareErrorMessage(cfResp))
	}
	return nil
}

func cloudflareRecordTypeForTarget(target string) string {
	if net.ParseIP(strings.TrimSpace(target)) != nil {
		return "A"
	}
	return "CNAME"
}

func cloudflareErrorMessage(resp cloudflareDNSResponse) string {
	if len(resp.Errors) > 0 && strings.TrimSpace(resp.Errors[0].Message) != "" {
		return "Cloudflare API error: " + resp.Errors[0].Message
	}
	return "Cloudflare API error"
}
