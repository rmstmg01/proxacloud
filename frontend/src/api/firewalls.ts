import client from './client';
import type {
  FirewallApplyResponse,
  FirewallAttachment,
  FirewallDirection,
  FirewallEvent,
  FirewallGroup,
  FirewallPolicy,
  FirewallProtocol,
  FirewallRule,
  FirewallTargetType,
} from '../types';

export const getFirewalls = () =>
  client.get<FirewallGroup[]>('/firewalls').then((r) => r.data);

export const getFirewall = (id: number) =>
  client.get<FirewallGroup>(`/firewalls/${id}`).then((r) => r.data);

export const createFirewall = (body: {
  name: string;
  description: string;
  default_inbound_policy: FirewallPolicy;
  default_outbound_policy: FirewallPolicy;
  enabled: boolean;
}) => client.post<FirewallGroup>('/firewalls', body).then((r) => r.data);

export const updateFirewall = (id: number, body: {
  name: string;
  description: string;
  default_inbound_policy: FirewallPolicy;
  default_outbound_policy: FirewallPolicy;
  enabled: boolean;
}) => client.put<FirewallGroup>(`/firewalls/${id}`, body).then((r) => r.data);

export const deleteFirewall = (id: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/firewalls/${id}`).then((r) => r.data);

export const createFirewallRule = (firewallID: number, body: {
  direction: FirewallDirection;
  action: 'allow' | 'deny';
  protocol: FirewallProtocol;
  source_cidr: string;
  destination_cidr: string;
  port_start?: number;
  port_end?: number;
  priority: number;
  description: string;
  enabled: boolean;
}) => client.post<FirewallRule>(`/firewalls/${firewallID}/rules`, body).then((r) => r.data);

export const deleteFirewallRule = (firewallID: number, ruleID: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/firewalls/${firewallID}/rules/${ruleID}`).then((r) => r.data);

export const createFirewallAttachment = (firewallID: number, body: {
  target_type: FirewallTargetType;
  target_id: number;
}) => client.post<FirewallAttachment>(`/firewalls/${firewallID}/attachments`, body).then((r) => r.data);

export const deleteFirewallAttachment = (firewallID: number, attachmentID: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/firewalls/${firewallID}/attachments/${attachmentID}`).then((r) => r.data);

export const applyFirewall = (firewallID: number) =>
  client.post<FirewallApplyResponse>(`/firewalls/${firewallID}/apply`).then((r) => r.data);

export const getFirewallEvents = (firewallID: number) =>
  client.get<FirewallEvent[]>(`/firewalls/${firewallID}/events`).then((r) => r.data);
