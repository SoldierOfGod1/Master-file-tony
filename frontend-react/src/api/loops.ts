/* Loop Operator — live state of the chat queue workers. */

import { apiGet, apiPost } from './client';

export interface LoopState {
  project_dir: string;
  conversation_id?: string;
  started_at?: string;
  paused: boolean;
  pending: number;
  running: boolean;
}

export async function listLoops(): Promise<LoopState[]> {
  return (await apiGet<LoopState[]>('/loops')) ?? [];
}

export async function pauseLoop(projectDir: string, paused: boolean): Promise<void> {
  await apiPost('/loops/pause', { project_dir: projectDir, paused });
}

export async function killLoop(projectDir: string): Promise<boolean> {
  const res = await apiPost<{ killed: boolean }>('/loops/kill', { project_dir: projectDir });
  return res?.killed ?? false;
}
