/* ============================================================
   rain Sales — dashboard snapshot client.
   The backend polls Axiom every few minutes and stores a snapshot
   in memory. This client just reads the snapshot — user activity
   never triggers a DB query.
   ============================================================ */

import { apiGet, apiPost } from './client';

export interface ChannelStats {
  total: number;
  web: number;
  call_centre: number;
  retail: number;
}

export interface RevenueByChannel {
  total: number;
  web: number;
  call_centre: number;
  retail: number;
}

export interface TrendPoint {
  hour: string;
  today: number;
  yesterday: number;
  last_week: number;
}

export interface MTDProgress {
  actual: number;
  budget: number;
  pct: number;
}

export interface SourceError {
  source: string;
  error: string;
}

export interface FulfilmentStats {
  manufactured: number;
  in_transit: number;
  delivered: number;
  failed: number;
  pct_delivered: number;
}

export interface PaymentStatusBucket {
  count: number;
  pct: number;
}

export interface PaymentHealthStats {
  total_payments: number;
  total_value: number;
  successful: PaymentStatusBucket;
  failed: PaymentStatusBucket;
  retry: PaymentStatusBucket;
  pending: PaymentStatusBucket;
}

export interface CallCentreKPIs {
  calls_today: number;
  answer_rate_pct: number;
  avg_wait_sec: number;
  abandoned: number;
  service_level_pct: number;
}

export interface CallCentreTrendPoint {
  hour: string;
  today: number;
  yesterday: number;
}

export interface BillRunErrorBucket {
  label: string;
  count: number;
}

export interface ProductSnapshot {
  sales_count: ChannelStats;
  yesterday_sales_count: ChannelStats;
  written_revenue: RevenueByChannel;
  mtd_sales_count: MTDProgress;
  mtd_revenue: MTDProgress;
  trend: TrendPoint[];
  fulfilment: FulfilmentStats;
  payment_health: PaymentHealthStats;
  call_centre_kpis: CallCentreKPIs;
  call_centre_trend: CallCentreTrendPoint[];
  bill_run_errors: BillRunErrorBucket[];
  errors?: SourceError[];
  latency_ms?: Record<string, number>;
}

export interface SalesSnapshot {
  as_of: string;
  window: string;
  timezone: string;
  rainone: ProductSnapshot;
  loop: ProductSnapshot;
  poll_latency_ms: number;
  poll_errors: number;
}

export async function getSalesSnapshot(): Promise<SalesSnapshot | null> {
  return apiGet<SalesSnapshot>('/sales/snapshot');
}

/** Trigger an on-demand poll. Backend collapses concurrent calls so
 *  rapid clicks can't stack load on the primary. */
export async function refreshSalesSnapshot(): Promise<SalesSnapshot | null> {
  return apiPost<SalesSnapshot>('/sales/refresh', {});
}

export const CHANNEL_COLOURS: Record<'total' | 'web' | 'call_centre' | 'retail', string> = {
  total:      '#7cc6ff',  // rain soft blue — the neutral channel
  web:        '#6ff2a0',  // green — digital path
  call_centre:'#ffaa00',  // amber — human / agent path
  retail:     '#ff7de0',  // pink — physical store
};
