/* ============================================================
   SOLDIER OF GOD — Projects API
   ============================================================ */

import type { Project } from '../types/api';
import { apiGet, apiPost, apiPut } from './client';

export async function listProjects(): Promise<Project[]> {
  const data = await apiGet<Project[]>('/projects');
  return data ?? [];
}

export async function createProject(
  project: Omit<Project, 'id'>,
): Promise<Project | null> {
  return apiPost<Project>('/projects', project);
}

export async function getProject(id: string): Promise<Project | null> {
  return apiGet<Project>(`/projects/${encodeURIComponent(id)}`);
}

export async function updateProject(
  id: string,
  updates: Partial<Project>,
): Promise<Project | null> {
  return apiPut<Project>(`/projects/${encodeURIComponent(id)}`, updates);
}

/** Triggers a push of every local project to ClickUp. Returns the counts
 *  reported by the backend (`pushed`, `skipped`). */
export async function syncProjects(): Promise<{ pushed: number; skipped: number } | null> {
  return apiPost<{ pushed: number; skipped: number }>('/projects/sync', {});
}
