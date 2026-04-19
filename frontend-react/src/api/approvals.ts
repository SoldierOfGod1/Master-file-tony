/* ============================================================
   SOLDIER OF GOD — Approvals API
   ============================================================ */

import type { Approval } from '../types/api';
import { apiGet, apiPost, apiPut } from './client';

export async function listApprovals(): Promise<Approval[]> {
  const data = await apiGet<Approval[]>('/approvals');
  return data ?? [];
}

export async function createApproval(
  approval: Omit<Approval, 'id'>,
): Promise<Approval | null> {
  return apiPost<Approval>('/approvals', approval);
}

export async function getApproval(id: string): Promise<Approval | null> {
  return apiGet<Approval>(`/approvals/${encodeURIComponent(id)}`);
}

export async function updateApproval(
  id: string,
  updates: Partial<Approval>,
): Promise<Approval | null> {
  return apiPut<Approval>(`/approvals/${encodeURIComponent(id)}`, updates);
}
