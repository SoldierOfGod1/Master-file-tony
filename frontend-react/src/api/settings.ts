/* App-level settings — ClickUp, Axiom Postgres, Athena CDR usage.
   Secret-like fields (api tokens, passwords, AWS secret keys) come
   back masked from the backend; writes that echo the masked value
   are ignored server-side so a re-save never clobbers the real
   stored secret. */

import { apiGet, apiPut } from './client';

export interface AppSettings {
  // ---- ClickUp ----
  'clickup.api_token': string;       // masked "••••ABCD"
  'clickup.workspace_id': string;
  'clickup.list_id': string;
  'clickup.configured': boolean;

  // ---- Axiom Postgres (connection strings stored elsewhere; legacy) ----
  'axiom.configured'?: boolean;

  // ---- Athena — Customer 360 CDR usage ----
  'athena.enabled'?: string;                 // "true" | "false"
  'athena.region'?: string;                  // eu-west-1
  'athena.database'?: string;                // usage
  'athena.workgroup'?: string;               // optional
  'athena.output_s3'?: string;               // s3://bucket/prefix/
  'athena.aws_access_key_id'?: string;
  'athena.aws_secret_access_key'?: string;   // masked on read
  'athena.configured'?: boolean;
}

export async function getSettings(): Promise<AppSettings | null> {
  return apiGet<AppSettings>('/settings');
}

/** Partial update. Pass only the keys the user changed. */
export async function updateSettings(
  patch: Record<string, string>,
): Promise<AppSettings | null> {
  return apiPut<AppSettings>('/settings', patch);
}
