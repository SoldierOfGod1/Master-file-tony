/* ============================================================
   rain Dark NOC — read-only client for the network-ops HUD.
   Backend: /api/v1/darknoc/* (gated by DARK_NOC_ENABLED).
   Empty/null responses are normalised here so the page never
   has to null-check on .length (the Cost Analytics crash that
   ate two hours yesterday).
   ============================================================ */

import { apiGet } from './client';

export interface DarkNocConfig {
  readonly enabled: boolean;
  readonly grafana_dashboard_uid: string;
}

export interface DarkNocOverview {
  readonly generated_at: string;
  readonly faults_last_24h: number;
  readonly critical_faults_24h: number;
  readonly active_slices: number;
  readonly slices_breaching_sla: number;
  readonly network_trust_score: number;
  readonly source: 'clickhouse' | 'stub' | 'unavailable';
  readonly source_latency_ms: number;
  readonly note?: string;
}

export interface DarkNocFault {
  readonly id: string;
  readonly occurred_at: string;
  readonly severity: 'critical' | 'warning' | 'info' | string;
  readonly source: string;
  readonly region?: string;
  readonly technology?: string;
  readonly title: string;
  readonly detail?: string;
}

export interface DarkNocRegistryAgent {
  readonly name: string;
  readonly domain: string;
  readonly category: string;
  readonly summary: string;
  readonly protocol: string;
  readonly use_case: string;
}

export async function getDarkNocConfig(): Promise<DarkNocConfig> {
  return (await apiGet<DarkNocConfig>('/darknoc/config')) ?? {
    enabled: false,
    grafana_dashboard_uid: '',
  };
}

export async function getDarkNocOverview(): Promise<DarkNocOverview | null> {
  return apiGet<DarkNocOverview>('/darknoc/overview');
}

export async function listDarkNocFaults(): Promise<DarkNocFault[]> {
  return (await apiGet<DarkNocFault[]>('/darknoc/faults')) ?? [];
}

export async function listDarkNocRegistry(): Promise<DarkNocRegistryAgent[]> {
  return (await apiGet<DarkNocRegistryAgent[]>('/darknoc/registry')) ?? [];
}
