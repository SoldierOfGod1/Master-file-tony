/* Database connections registry API. Each entry represents one postgres
   or clickhouse cluster the dashboard can query. Passwords are masked on
   read — writing a masked value leaves the stored password untouched. */

import { apiDelete, apiGet, apiPost, apiPut } from './client';

export type DBDriver = 'postgres' | 'clickhouse';

export interface DBConnection {
  id: string;
  label: string;
  driver: DBDriver;
  host: string;
  port: string;
  database: string;
  user: string;
  password: string; // masked on GET; real value on POST/PUT when user edits it
  ssl_mode: string;
  is_primary: boolean;
  filled: boolean;
}

export async function listConnections(): Promise<DBConnection[]> {
  return (await apiGet<DBConnection[]>('/connections')) ?? [];
}

export async function upsertConnection(
  c: Partial<DBConnection> & Pick<DBConnection, 'host' | 'port' | 'user' | 'database'>,
): Promise<DBConnection | null> {
  return apiPost<DBConnection>('/connections', c);
}

export async function updateConnection(
  id: string,
  patch: Partial<DBConnection>,
): Promise<DBConnection | null> {
  return apiPut<DBConnection>(`/connections/${encodeURIComponent(id)}`, patch);
}

export async function deleteConnection(id: string): Promise<boolean> {
  return apiDelete(`/connections/${encodeURIComponent(id)}`);
}

export async function setPrimary(id: string): Promise<{ primary: string } | null> {
  return apiPost<{ primary: string }>(`/connections/${encodeURIComponent(id)}/primary`, {});
}

/** Returns ok=true on success or an error string from the server on failure. */
export async function testConnection(id: string): Promise<{ ok: true } | { ok: false; error: string }> {
  const result = await apiPost<{ status: string }>(`/connections/${encodeURIComponent(id)}/test`, {});
  if (result && result.status === 'ok') return { ok: true };
  return { ok: false, error: 'test failed — see backend log' };
}
