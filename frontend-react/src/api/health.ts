/* ============================================================
   SOLDIER OF GOD — Health Metrics API
   ============================================================ */

import type { HealthMetrics } from '../types/api';
import { apiGet } from './client';

export async function getHealthMetrics(): Promise<HealthMetrics | null> {
  return apiGet<HealthMetrics>('/health-metrics');
}
