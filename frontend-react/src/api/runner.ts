/* ============================================================
   SOLDIER OF GOD — Projects Runner API
   Start/stop local dev servers (frontend + backend) straight from
   the Projects page. The backend owns process lifecycle; this
   module just wraps the HTTP endpoints + the WS event shapes.
   ============================================================ */

import { apiGet, apiPost } from './client';

export type RunnerState = 'stopped' | 'starting' | 'running' | 'crashed' | 'stopping';

export interface RunnerComponent {
  role: 'backend' | 'frontend' | string;
  label: string;
  dir: string;
  command: string;
  args: string[];
  port: number;
  health_url: string;
}

export interface RunnerProcess {
  component: RunnerComponent;
  state: RunnerState;
  pid: number;
  started_at?: string;
  exited_at?: string;
  exit_code: number;
  error?: string;
}

export interface RunnerGroup {
  project_id: string;
  processes: RunnerProcess[];
}

export interface RunnerLogLine {
  time: string;
  stream: 'stdout' | 'stderr' | string;
  role: 'backend' | 'frontend' | string;
  line: string;
}

export async function startProject(id: string): Promise<RunnerGroup | null> {
  return apiPost<RunnerGroup>(`/projects/${encodeURIComponent(id)}/run`, {});
}

export async function stopProject(id: string): Promise<boolean> {
  return (await apiPost(`/projects/${encodeURIComponent(id)}/stop`, {})) !== null;
}

export async function getRunnerStatus(id: string): Promise<RunnerGroup | null> {
  return apiGet<RunnerGroup>(`/projects/${encodeURIComponent(id)}/runner`);
}

export async function listRunners(): Promise<RunnerGroup[]> {
  const data = await apiGet<RunnerGroup[]>('/runner');
  return data ?? [];
}

export async function getRunnerLogs(
  id: string,
  component: number,
  tail = 200,
): Promise<RunnerLogLine[]> {
  const data = await apiGet<RunnerLogLine[]>(
    `/projects/${encodeURIComponent(id)}/runner/logs?component=${component}&tail=${tail}`,
  );
  return data ?? [];
}

/** Highest-priority state across a group's processes — the state to
 *  show on the project's LED. "crashed" wins over all others so issues
 *  surface immediately; "running" wins only when every component is up. */
export function groupState(g: RunnerGroup | null | undefined): RunnerState {
  if (!g || g.processes.length === 0) return 'stopped';
  const states = g.processes.map((p) => p.state);
  if (states.includes('crashed')) return 'crashed';
  if (states.includes('stopping')) return 'stopping';
  if (states.includes('starting')) return 'starting';
  if (states.every((s) => s === 'running')) return 'running';
  if (states.every((s) => s === 'stopped')) return 'stopped';
  return 'starting';
}

export const RUNNER_STATE_COLOR: Record<RunnerState, string> = {
  stopped:  '#7cc6ff',
  starting: '#ffaa00',
  running:  '#6ff2a0',
  crashed:  '#ff3355',
  stopping: '#ff7de0',
};
