/* ============================================================
   SOLDIER OF GOD — KPIs API
   ============================================================ */

import type { KPIs } from '../types/api';
import { apiGet } from './client';

export async function getKPIs(): Promise<KPIs | null> {
  return apiGet<KPIs>('/kpis');
}
