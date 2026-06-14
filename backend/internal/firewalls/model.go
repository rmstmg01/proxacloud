package firewalls

import "time"

const (
	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"

	ActionAllow = "allow"
	ActionDeny  = "deny"

	ProtocolTCP  = "tcp"
	ProtocolUDP  = "udp"
	ProtocolICMP = "icmp"
	ProtocolAny  = "any"

	PolicyAllow = "allow"
	PolicyDeny  = "deny"

	TargetVPS          = "vps"
	TargetContainer    = "container"
	TargetClusterNode  = "cluster_node"
	TargetDatabaseNode = "database_node"
)

type Group struct {
	ID                    int64        `json:"id"`
	Name                  string       `json:"name"`
	Description           string       `json:"description"`
	DefaultInboundPolicy  string       `json:"default_inbound_policy"`
	DefaultOutboundPolicy string       `json:"default_outbound_policy"`
	Enabled               bool         `json:"enabled"`
	Status                string       `json:"status"`
	LastAppliedAt         *time.Time   `json:"last_applied_at,omitempty"`
	Rules                 []Rule       `json:"rules,omitempty"`
	Attachments           []Attachment `json:"attachments,omitempty"`
	CreatedAt             time.Time    `json:"created_at"`
	UpdatedAt             time.Time    `json:"updated_at"`
}

type Rule struct {
	ID              int64     `json:"id"`
	FirewallGroupID int64     `json:"firewall_group_id"`
	Direction       string    `json:"direction"`
	Action          string    `json:"action"`
	Protocol        string    `json:"protocol"`
	SourceCIDR      string    `json:"source_cidr"`
	DestinationCIDR string    `json:"destination_cidr"`
	PortStart       *int      `json:"port_start,omitempty"`
	PortEnd         *int      `json:"port_end,omitempty"`
	Priority        int       `json:"priority"`
	Description     string    `json:"description"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Attachment struct {
	ID              int64     `json:"id"`
	FirewallGroupID int64     `json:"firewall_group_id"`
	TargetType      string    `json:"target_type"`
	TargetID        int64     `json:"target_id"`
	TargetName      string    `json:"target_name,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type Event struct {
	ID              int64     `json:"id"`
	FirewallGroupID int64     `json:"firewall_group_id"`
	Action          string    `json:"action"`
	Level           string    `json:"level"`
	Message         string    `json:"message"`
	Output          string    `json:"output"`
	CreatedAt       time.Time `json:"created_at"`
}

type CreateGroupRequest struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	DefaultInboundPolicy  string `json:"default_inbound_policy"`
	DefaultOutboundPolicy string `json:"default_outbound_policy"`
	Enabled               bool   `json:"enabled"`
}

type UpdateGroupRequest struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	DefaultInboundPolicy  string `json:"default_inbound_policy"`
	DefaultOutboundPolicy string `json:"default_outbound_policy"`
	Enabled               bool   `json:"enabled"`
}

type CreateRuleRequest struct {
	Direction       string `json:"direction"`
	Action          string `json:"action"`
	Protocol        string `json:"protocol"`
	SourceCIDR      string `json:"source_cidr"`
	DestinationCIDR string `json:"destination_cidr"`
	PortStart       *int   `json:"port_start"`
	PortEnd         *int   `json:"port_end"`
	Priority        int    `json:"priority"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
}

type UpdateRuleRequest = CreateRuleRequest

type CreateAttachmentRequest struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
}

type ApplyResponse struct {
	FirewallGroupID int64    `json:"firewall_group_id"`
	Status          string   `json:"status"`
	Message         string   `json:"message"`
	Warnings        []string `json:"warnings"`
}
