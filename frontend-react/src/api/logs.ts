/* ============================================================
   SOLDIER OF GOD — Logs API
   ============================================================ */

import type { LogEntry } from '../types/api';
import { apiGet } from './client';

export async function listLogs(level?: string): Promise<LogEntry[]> {
  const query = level ? `?level=${encodeURIComponent(level)}` : '';
  const data = await apiGet<LogEntry[]>(`/logs${query}`);
  return data ?? [];
}
