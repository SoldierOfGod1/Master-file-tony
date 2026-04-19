/* ============================================================
   SOLDIER OF GOD — Agent Office API
   ============================================================ */

import type { OfficeData, AgentOfficeState } from '../types/api';
import { apiGet } from './client';

export async function getOffice(): Promise<OfficeData | null> {
  return apiGet<OfficeData>('/office');
}

export async function getOfficeAgent(
  id: string,
): Promise<AgentOfficeState | null> {
  return apiGet<AgentOfficeState>(`/office/agents/${encodeURIComponent(id)}`);
}
