/* Agent Fleet — filesystem scan for agents / hooks / rules and per-agent
   append-only memory. Backed by /api/v1/agent-fleet/*. */

import { apiGet, apiPost } from './client';

export type Source = 'global' | 'project' | 'plugin';

export interface FleetAgent {
  id: string;
  name: string;
  file_name: string;
  path: string;
  description: string;
  category: string;
  source: Source;
  model?: string;
  version?: string;
  thinking?: string;
  overrides: boolean;
  has_memory: boolean;
  plugin?: string;
}

export interface FleetHook {
  name: string;
  path: string;
  kind: string;     // "script" | "docs" | "config" | "other"
  language: string; // "bash" | "powershell" | "markdown" | ...
  size_bytes: number;
  executable: boolean;
}

export interface FleetRule {
  name: string;
  path: string;
  group: string;     // "common" | "python" | "golang" | "root" | ...
  source: Source;
}

export async function listFleetAgents(): Promise<FleetAgent[]> {
  return (await apiGet<FleetAgent[]>('/agent-fleet/agents')) ?? [];
}

export async function listFleetHooks(): Promise<FleetHook[]> {
  return (await apiGet<FleetHook[]>('/agent-fleet/hooks')) ?? [];
}

export async function listFleetRules(): Promise<FleetRule[]> {
  return (await apiGet<FleetRule[]>('/agent-fleet/rules')) ?? [];
}

/** Fetch the raw contents of any agent/hook/rule file (sandboxed server-side). */
export async function readFleetFile(path: string): Promise<string> {
  const res = await apiGet<{ path: string; content: string }>(
    `/agent-fleet/file?path=${encodeURIComponent(path)}`,
  );
  return res?.content ?? '';
}

/** Read the sibling .memory.md file for an agent. Returns "" when not yet created. */
export async function readAgentMemory(agentPath: string): Promise<string> {
  const res = await apiGet<{ path: string; content: string }>(
    `/agent-fleet/memory?path=${encodeURIComponent(agentPath)}`,
  );
  return res?.content ?? '';
}

/** Append a dated lesson to the agent's memory file. Returns the full memory. */
export async function appendAgentMemory(agentPath: string, note: string): Promise<string> {
  const res = await apiPost<{ path: string; content: string }>(
    '/agent-fleet/memory',
    { path: agentPath, note },
  );
  return res?.content ?? '';
}
