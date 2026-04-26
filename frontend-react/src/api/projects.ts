/* ============================================================
   SOLDIER OF GOD — Projects API
   ============================================================ */

import type { Project } from '../types/api';
import { apiDelete, apiGet, apiPost, apiPut } from './client';

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

/** Deletes a project both locally AND its mirrored ClickUp task (+ any
 *  cached subtasks). Returns true on success, false on 404 / error. */
export async function deleteProject(id: string): Promise<boolean> {
  return apiDelete(`/projects/${encodeURIComponent(id)}`);
}

/** Kicks off an async push of every local project to ClickUp. Returns
 *  immediately; caller should poll `getSyncStatus` to watch progress. */
export async function syncProjects(): Promise<SyncStatus | null> {
  const res = await apiPost<{ started: boolean; already_running?: boolean; status: SyncStatus }>(
    '/projects/sync', {},
  );
  return res?.status ?? null;
}

export interface SyncStatus {
  in_progress: boolean;
  started_at?: string;
  finished_at?: string;
  total: number;
  pushed: number;
  skipped: number;
  current_id?: string;
  last_error?: string;
  last_duration?: string;
}

/** Poll for the latest sync progress. Cheap — no external calls. */
export async function getSyncStatus(): Promise<SyncStatus | null> {
  return apiGet<SyncStatus>('/projects/sync/status');
}
