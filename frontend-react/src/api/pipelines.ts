/* ============================================================
   SOLDIER OF GOD — Pipelines API
   ============================================================ */

import type { Pipeline } from '../types/api';
import { apiGet, apiPost, apiPut } from './client';

export async function listPipelines(): Promise<Pipeline[]> {
  const data = await apiGet<Pipeline[]>('/pipelines');
  return data ?? [];
}

export async function createPipeline(
  pipeline: Omit<Pipeline, 'id'>,
): Promise<Pipeline | null> {
  return apiPost<Pipeline>('/pipelines', pipeline);
}

export async function getPipeline(id: string): Promise<Pipeline | null> {
  return apiGet<Pipeline>(`/pipelines/${encodeURIComponent(id)}`);
}

export async function updatePipeline(
  id: string,
  updates: Partial<Pipeline>,
): Promise<Pipeline | null> {
  return apiPut<Pipeline>(`/pipelines/${encodeURIComponent(id)}`, updates);
}
