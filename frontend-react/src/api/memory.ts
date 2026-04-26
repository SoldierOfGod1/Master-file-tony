/* ============================================================
   SOLDIER OF GOD — Agent memory API (D1 + follow-up)
   ============================================================ */

import { apiDelete, apiGet, apiPost } from './client';

export type MemoryKind = 'preference' | 'incident_context' | 'pattern' | 'note';

export interface MemoryEntry {
  readonly id: number;
  readonly user_id: string;
  readonly kind: MemoryKind;
  readonly body: string;
  readonly created_at: string;
}

export async function listMemory(userID?: string): Promise<MemoryEntry[]> {
  const qs = userID ? `?user_id=${encodeURIComponent(userID)}` : '';
  const data = await apiGet<MemoryEntry[]>(`/memory${qs}`);
  return data ?? [];
}

export async function createMemory(
  userID: string,
  kind: MemoryKind,
  body: string,
): Promise<{ id: number } | null> {
  return apiPost<{ id: number }>('/memory', { user_id: userID, kind, body });
}

export async function deleteMemory(
  id: number,
): Promise<{ id: number; deleted: boolean } | null> {
  return apiDelete<{ id: number; deleted: boolean }>(`/memory/${id}`);
}
