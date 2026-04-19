/* ClickUp integration API */

import { apiGet, apiPatch, apiPost } from './client';

export interface ClickUpTask {
  id: string;
  name: string;
  description: string;
  status: string;
  status_color: string;
  url: string;
  due_date?: string;
  priority?: string;
  tags?: string[];
  assignees?: string[];
}

export interface ClickUpConfig {
  configured: boolean;
  workspace_id: string;
  list_id: string;
}

export interface CreateTaskInput {
  name: string;
  description?: string;
  status?: string;
  priority?: number;
}

export async function getClickUpConfig(): Promise<ClickUpConfig | null> {
  return apiGet<ClickUpConfig>('/clickup/config');
}

export async function listClickUpTasks(): Promise<ClickUpTask[]> {
  return (await apiGet<ClickUpTask[]>('/clickup/tasks')) ?? [];
}

export async function createClickUpTask(
  input: CreateTaskInput,
): Promise<ClickUpTask | null> {
  return apiPost<ClickUpTask>('/clickup/tasks', input);
}

export async function updateClickUpTaskStatus(
  id: string,
  status: string,
): Promise<boolean> {
  const res = await apiPatch<{ id: string; status: string }>(
    `/clickup/tasks/${encodeURIComponent(id)}`,
    { status },
  );
  return res !== null;
}
