/* Quality-gate API — lint / type-check / secret scan results. */

import { apiGet, apiPost } from './client';

export interface QualityGate {
  ok: boolean;
  output: string;
  duration_ms: number;
  skipped?: boolean;
  reason?: string;
}

export interface QualitySecretHit {
  file: string;
  line: number;
  rule: string;
  text: string;
}

export interface QualityReport {
  go_vet: QualityGate;
  typescript: QualityGate;
  secrets: QualityGate;
  hits?: QualitySecretHit[];
  ran_at: string;
}

export async function getLastQuality(): Promise<QualityReport | null> {
  return await apiGet<QualityReport>('/quality');
}

export async function runQuality(): Promise<QualityReport | null> {
  return await apiPost<QualityReport>('/quality', {});
}
