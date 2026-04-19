/* ============================================================
   SOLDIER OF GOD — Security API
   ============================================================ */

import type { SecurityState } from '../types/api';
import { apiGet } from './client';

export async function getSecurity(): Promise<SecurityState | null> {
  return apiGet<SecurityState>('/security');
}
