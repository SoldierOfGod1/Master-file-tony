/* ============================================================
   SOLDIER OF GOD — Tasks API
   ============================================================ */

import type { Task } from '../types/api';
import { apiGet, apiPost, apiPut, apiDelete } from './client';

export async function listTasks(): Promise<Task[]> {
  const data = await apiGet<Task[]>('/tasks');
  return data ?? [];
}

export async function createTask(
  task: Omit<Task, 'id'>,
): Promise<Task | null> {
  return apiPost<Task>('/tasks', task);
}

export async function getTask(id: string): Promise<Task | null> {
  return apiGet<Task>(`/tasks/${encodeURIComponent(id)}`);
}

export async function updateTask(
  id: string,
  updates: Partial<Task>,
): Promise<Task | null> {
  return apiPut<Task>(`/tasks/${encodeURIComponent(id)}`, updates);
}

export async function deleteTask(id: string): Promise<boolean> {
  return apiDelete(`/tasks/${encodeURIComponent(id)}`);
}
