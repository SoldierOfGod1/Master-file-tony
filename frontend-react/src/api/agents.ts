/* ============================================================
   SOLDIER OF GOD — Agents API
   ============================================================ */

import type { Agent } from '../types/api';
import { apiGet, apiPut } from './client';

export async function listAgents(): Promise<Agent[]> {
  const data = await apiGet<Agent[]>('/agents');
  return data ?? [];
}

export async function getAgent(id: string): Promise<Agent | null> {
  return apiGet<Agent>(`/agents/${encodeURIComponent(id)}`);
}

export async function updateAgent(
  id: string,
  updates: Partial<Agent>,
): Promise<Agent | null> {
  return apiPut<Agent>(`/agents/${encodeURIComponent(id)}`, updates);
}
