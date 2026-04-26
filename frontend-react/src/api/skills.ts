/* Skills + MCP catalogue API */

import { apiGet, apiPost } from './client';

export type SkillSource = 'global' | 'project' | 'plugin';

export interface Skill {
  name: string;
  description: string;
  category: string;
  source: SkillSource;
  plugin?: string;
  path: string;
}

export interface MCPServer {
  name: string;
  group?: string;
  comment?: string;
  transport: string;
  url?: string;
  command?: string;
  enabled: boolean;
  source: SkillSource;
}

export type MCPHealthState = 'up' | 'down' | 'local' | 'unknown';

export interface MCPHealth {
  name: string;
  status: MCPHealthState;
  latency_ms: number;
  checked_at: string;
  error?: string;
}

export async function listSkills(): Promise<Skill[]> {
  return (await apiGet<Skill[]>('/skills')) ?? [];
}

export async function listMCPServers(): Promise<MCPServer[]> {
  return (await apiGet<MCPServer[]>('/mcp')) ?? [];
}

export async function listMCPHealth(): Promise<MCPHealth[]> {
  return (await apiGet<MCPHealth[]>('/mcp/health')) ?? [];
}

/** Read a SKILL.md file (sandboxed to ~/.claude/skills + project
 *  .claude/skills + the agents/hooks/rules roots the scanner knows). */
export async function readSkillFile(path: string): Promise<string> {
  const res = await apiGet<{ path: string; content: string }>(
    `/skills/file?path=${encodeURIComponent(path)}`,
  );
  return res?.content ?? '';
}

/** Atomically overwrite a user-owned SKILL.md. Plugin-owned paths
 *  return 403 — caller should clone first. */
export async function writeSkillFile(path: string, content: string): Promise<string> {
  const res = await apiPost<{ path: string; content: string }>(
    '/skills/file', { path, content },
  );
  return res?.content ?? content;
}
