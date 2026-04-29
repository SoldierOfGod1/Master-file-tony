/* Customer 360 API — Axiom-backed client lookup.
   The backend handles query parameterisation; we just pass the raw
   phone/email the user typed. An optional `connectionID` argument picks a
   non-primary DB connection when the user has multiple clusters set up. */

import { apiGet, apiPost, apiPut } from './client';
import type { Customer360, CustomerConfig } from '../types/api';

export async function getCustomerConfig(): Promise<CustomerConfig | null> {
  return apiGet<CustomerConfig>('/customer/config');
}

/** `&connection=<id>` query segment; empty when the primary should be used. */
function connParam(connectionID?: string): string {
  return connectionID ? `&connection=${encodeURIComponent(connectionID)}` : '';
}

export async function lookupByPhone(phone: string, connectionID?: string): Promise<Customer360 | null> {
  return apiGet<Customer360>(
    `/customer?phone=${encodeURIComponent(phone)}${connParam(connectionID)}`,
  );
}

export async function lookupByEmail(email: string, connectionID?: string): Promise<Customer360 | null> {
  return apiGet<Customer360>(
    `/customer?email=${encodeURIComponent(email)}${connParam(connectionID)}`,
  );
}

export async function getCustomerByID(id: string, connectionID?: string): Promise<Customer360 | null> {
  const qs = connectionID ? `?connection=${encodeURIComponent(connectionID)}` : '';
  return apiGet<Customer360>(`/customer/${encodeURIComponent(id)}${qs}`);
}

/** When a lookup returns multiple candidates, re-query with the chosen
 *  individual id. Routed through /customer/{id} which the backend
 *  already exposes for deep-link neighbours. */
export async function lookupByID(individualID: string, connectionID?: string): Promise<Customer360 | null> {
  const qs = connectionID ? `?connection=${encodeURIComponent(connectionID)}` : '';
  return apiGet<Customer360>(`/customer/${encodeURIComponent(individualID)}${qs}`);
}

/** Record an NBA outcome — accept / dismiss / snooze. The backend
 *  flips the rec's status (so the 7-day cooldown applies) and
 *  inserts an audit row into customer_recommendation_actions. */
export async function recordRecommendationAction(
  customerID: string,
  recID: string,
  body: { action: 'accept' | 'dismiss' | 'snooze'; channel?: string; agent_id?: string; note?: string },
): Promise<{ ok: boolean; at: string } | null> {
  return apiPost<{ ok: boolean; at: string }>(
    `/customer/${encodeURIComponent(customerID)}/recommendation/${encodeURIComponent(recID)}/action`,
    body,
  );
}

// ---- IMSI overrides --------------------------------------------
// When the 3-pivot IMSI resolver can't find a customer's SIMs,
// the operator pastes known IMSIs here. Usage + CDR Usage panels
// use them directly on subsequent lookups.

export async function getIMSIOverride(customerID: string): Promise<{ imsis: string[] } | null> {
  return apiGet<{ imsis: string[] }>(
    `/customer/${encodeURIComponent(customerID)}/imsi-override`,
  );
}

export async function setIMSIOverride(customerID: string, imsis: string[]): Promise<{ imsis: string[]; count: number } | null> {
  return apiPut<{ imsis: string[]; count: number }>(
    `/customer/${encodeURIComponent(customerID)}/imsi-override`,
    { imsis },
  );
}

// ---- Usage summary --------------------------------------------
// Headline rollup of the last 30 days from the rain Axiom HTTP API
// (api.sit.rain.co.za/axiom/usage-online/fact-cdr-analytics/daily-usage),
// pre-computed server-side. Replaces the old Athena CDR tile.

export interface UsageDay {
  readonly date: string;
  readonly bytes: number;
  readonly up?: number;
  readonly down?: number;
}

export interface UsageSummary {
  readonly msisdn: string;
  /** "axiom-api" | "gaussdb" — which upstream the numbers came from.
   *  Display in the tile chip so the operator knows the provenance.
   *  Backend chooses based on the USAGE_SOURCE env; frontend doesn't
   *  override per-call (kept central so two operators don't see
   *  divergent numbers for the same MSISDN). */
  readonly source?: 'axiom-api' | 'gaussdb' | string;
  readonly window_days: number;
  readonly first_day?: string;
  readonly last_day?: string;
  readonly total_bytes: number;
  readonly avg_daily_bytes: number;
  readonly peak_daily_bytes: number;
  readonly peak_day?: string;
  readonly active_days: number;
  readonly series: UsageDay[];
}

export async function getUsageSummary(msisdn: string): Promise<UsageSummary | null> {
  return apiGet<UsageSummary>(`/customer/usage/summary?msisdn=${encodeURIComponent(msisdn)}`);
}
