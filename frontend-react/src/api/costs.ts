/* ============================================================
   SOLDIER OF GOD — Costs API
   ============================================================ */

import type { CostData } from '../types/api';
import { apiGet } from './client';

export async function getCosts(): Promise<CostData | null> {
  return apiGet<CostData>('/costs');
}
