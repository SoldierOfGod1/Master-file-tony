/* ============================================================
   SOLDIER OF GOD — Cross-user pattern aggregate (D2 follow-up)
   Privacy-safe ops telemetry. Counts only, k-anonymity ≥3.
   Backend gate: RAIN_SUPPORT_L2.
   ============================================================ */

import { apiGet } from './client';

export interface DayCount {
  readonly day: string;
  readonly count: number;
}

export interface KindCount {
  readonly kind: string;
  readonly count: number;
}

export interface KeywordCount {
  readonly stem: string;
  readonly occurrences: number;
  readonly user_buckets: number;
}

export interface PatternsAggregate {
  readonly conversations_by_day: DayCount[];
  readonly memory_by_kind: KindCount[];
  readonly active_users_7d: number;
  readonly active_users_7d_suppressed: boolean;
  readonly top_keyword_stems: KeywordCount[];
  readonly generated_at: string;
}

export async function getPatternsAggregate(): Promise<PatternsAggregate | null> {
  return apiGet<PatternsAggregate>('/patterns/aggregate');
}
