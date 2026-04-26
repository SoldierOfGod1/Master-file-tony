/* Axiom Explorer — read-only schema discovery + Snowflake-middleware
   endpoint → Axiom table correlation map. */

import { apiGet } from './client';

export interface AxiomDatabase {
  name: string;
  owner: string;
  size_mb: number;
  connections: number;
}

export interface AxiomSchema {
  name: string;
  owner?: string;
  table_count: number;
}

export interface AxiomTable {
  schema: string;
  name: string;
  type: string;            // "BASE TABLE" | "VIEW" | ...
  row_estimate: number;
  likely_domain?: string;
}

export interface AxiomColumn {
  schema: string;
  table: string;
  name: string;
  data_type: string;
  nullable: boolean;
  default?: string | null;
  char_max_len?: number | null;
  ordinal_pos: number;
}

export interface AxiomPeek {
  schema: string;
  table: string;
  columns: string[];
  rows: string[][];
  note?: string;
}

export interface AxiomEndpointMap {
  method: string;
  path: string;
  summary: string;
  domain: string;
  reads?: string[];
  writes?: string[];
  notes?: string;
}

export async function listDatabases(conn?: string): Promise<AxiomDatabase[]> {
  const q = conn ? `?conn=${encodeURIComponent(conn)}` : '';
  return (await apiGet<AxiomDatabase[]>(`/axiom/databases${q}`)) ?? [];
}

function build(params: Record<string, string | undefined>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) p.set(k, v);
  return p.toString() ? `?${p.toString()}` : '';
}

export async function listSchemas(conn?: string, db?: string): Promise<AxiomSchema[]> {
  return (await apiGet<AxiomSchema[]>(`/axiom/schemas${build({ conn, db })}`)) ?? [];
}

export async function listTables(schema?: string, conn?: string, db?: string): Promise<AxiomTable[]> {
  return (await apiGet<AxiomTable[]>(`/axiom/tables${build({ conn, db, schema })}`)) ?? [];
}

export async function listColumns(schema: string, table: string, conn?: string, db?: string): Promise<AxiomColumn[]> {
  return (await apiGet<AxiomColumn[]>(`/axiom/columns${build({ conn, db, schema, table })}`)) ?? [];
}

export async function searchColumns(q: string, conn?: string, db?: string): Promise<AxiomColumn[]> {
  return (await apiGet<AxiomColumn[]>(`/axiom/search${build({ conn, db, q })}`)) ?? [];
}

export async function peekTable(schema: string, table: string, limit = 5, conn?: string, db?: string): Promise<AxiomPeek | null> {
  return await apiGet<AxiomPeek>(`/axiom/peek${build({ conn, db, schema, table, limit: String(limit) })}`);
}

export async function listEndpointMap(): Promise<AxiomEndpointMap[]> {
  return (await apiGet<AxiomEndpointMap[]>('/axiom/endpoint-map')) ?? [];
}
