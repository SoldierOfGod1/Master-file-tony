/* App-level settings (ClickUp workspace swap lives here).
   The backend masks the ClickUp token when reading; writes respecting the
   mask (i.e. re-saving the masked value leaves the stored token alone). */

import { apiGet, apiPut } from './client';

export interface AppSettings {
  'clickup.api_token': string;       // masked value like "••••ABCD" when set
  'clickup.workspace_id': string;
  'clickup.list_id': string;
  'clickup.configured': boolean;
}

export async function getSettings(): Promise<AppSettings | null> {
  return apiGet<AppSettings>('/settings');
}

/** Partial update. Pass only the keys the user changed. */
export async function updateSettings(
  patch: Partial<Pick<AppSettings, 'clickup.api_token' | 'clickup.workspace_id' | 'clickup.list_id'>>,
): Promise<AppSettings | null> {
  return apiPut<AppSettings>('/settings', patch);
}
