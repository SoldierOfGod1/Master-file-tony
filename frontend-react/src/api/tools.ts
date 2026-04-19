/* ============================================================
   SOLDIER OF GOD — Tools API
   ============================================================ */

import type { Tool } from '../types/api';
import { apiGet, apiPut } from './client';

export async function listTools(): Promise<Tool[]> {
  const data = await apiGet<Tool[]>('/tools');
  return data ?? [];
}

export async function getTool(id: string): Promise<Tool | null> {
  return apiGet<Tool>(`/tools/${encodeURIComponent(id)}`);
}

export async function updateTool(
  id: string,
  updates: Partial<Tool>,
): Promise<Tool | null> {
  return apiPut<Tool>(`/tools/${encodeURIComponent(id)}`, updates);
}
