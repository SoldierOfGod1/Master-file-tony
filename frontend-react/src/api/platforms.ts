/* Platform Monitor + rain Service tab — API client for /platforms/*. */

import { apiGet, apiPost } from './client';

export type PlatformState = 'up' | 'degraded' | 'down' | 'unknown';
export type Criticality = 'top' | 'standard';
export type Severity = 'info' | 'warning' | 'critical' | 'p1';

export interface DNSCheck {
  resolved: boolean;
  ips?: string[];
  latency_ms: number;
  error?: string;
}

export interface TLSCheck {
  valid: boolean;
  issuer?: string;
  subject?: string;
  expires_at?: string;
  days_to_expiry: number;
  error?: string;
}

export interface ContentCheck {
  checked: boolean;
  title_ok: boolean;
  body_ok: boolean;
  error?: string;
}

export interface PlatformStatus {
  id: string;
  name: string;
  group: string;
  url: string;
  docs_url?: string;
  criticality: Criticality;
  environment: string;
  owner?: string;
  state: PlatformState;
  http_code: number;
  latency_ms: number;
  checked_at: string;
  error?: string;
  dns: DNSCheck;
  tls: TLSCheck;
  content: ContentCheck;
  failure_streak: number;
  last_success?: string;
  last_failure?: string;
  uptime_24h: number;
  uptime_7d: number;
  uptime_30d: number;
}

export interface DatabaseHealth {
  id: string;
  label: string;
  driver: string;
  host: string;
  database: string;
  priority: 'p1' | 'standard';
  reachable: boolean;
  ping_ms: number;
  query_ms: number;
  active_sessions: number;
  error?: string;
  checked_at: string;
  last_success?: string;
  last_failure?: string;
  failure_streak: number;
  is_axiom: boolean;
}

export interface StoredAlert {
  id: number;
  service_id: string;
  kind: string;
  severity: Severity;
  message: string;
  cause?: string;
  next_step?: string;
  state: string;
  created_at: string;
  resolved_at?: string;
}

export interface IncidentEvent {
  id: number;
  kind: string;
  message: string;
  at: string;
}

export interface Incident {
  id: number;
  service_id: string;
  kind: string;
  severity: Severity;
  title: string;
  summary?: string;
  state: string;
  opened_at: string;
  mitigated_at?: string;
  resolved_at?: string;
  timeline?: IncidentEvent[];
}

export async function listPlatformHealth(): Promise<PlatformStatus[]> {
  return (await apiGet<PlatformStatus[]>('/platforms/health')) ?? [];
}

export async function listDatabaseHealth(): Promise<DatabaseHealth[]> {
  return (await apiGet<DatabaseHealth[]>('/platforms/databases')) ?? [];
}

export async function listPlatformAlerts(state?: string, limit = 100): Promise<StoredAlert[]> {
  const qs = new URLSearchParams();
  if (state) qs.set('state', state);
  qs.set('limit', String(limit));
  return (await apiGet<StoredAlert[]>(`/platforms/alerts?${qs}`)) ?? [];
}

export async function listPlatformIncidents(limit = 50): Promise<Incident[]> {
  return (await apiGet<Incident[]>(`/platforms/incidents?limit=${limit}`)) ?? [];
}

export async function ackIncident(id: number, note?: string): Promise<void> {
  const qs = note ? `?note=${encodeURIComponent(note)}` : '';
  await apiPost(`/platforms/incidents/${id}/ack${qs}`, {});
}

export async function resolveIncident(id: number, note?: string): Promise<void> {
  const qs = note ? `?note=${encodeURIComponent(note)}` : '';
  await apiPost(`/platforms/incidents/${id}/resolve${qs}`, {});
}

export async function resolveAlert(id: number): Promise<void> {
  await apiPost(`/platforms/alerts/${id}/resolve`, {});
}

/* Phase D2 — incident correlation rollup. Returns everything
   tagged with this incident_id (or numeric id, treated as a
   string) across conversations / approvals / IMSI audits / spend.
   Each list is independent — render only the sections you care
   about. total_zar is the sum of agent spend during the incident. */
export interface IncidentTimeline {
  readonly incident_id: string;
  readonly conversations: ReadonlyArray<Record<string, unknown>>;
  readonly approvals: ReadonlyArray<Record<string, unknown>>;
  readonly imsi_audits: ReadonlyArray<Record<string, unknown>>;
  readonly cost_records: ReadonlyArray<Record<string, unknown>>;
  readonly total_zar: number;
}

export async function getIncidentTimeline(id: string): Promise<IncidentTimeline | null> {
  return apiGet<IncidentTimeline>(
    `/platforms/incidents/${encodeURIComponent(id)}/timeline`,
  );
}
