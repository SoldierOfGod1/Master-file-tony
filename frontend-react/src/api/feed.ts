/* ============================================================
   SOLDIER OF GOD — Live Feed API
   ============================================================ */

import type { FeedEvent } from '../types/api';
import { apiGet } from './client';

export async function listFeed(type?: string): Promise<FeedEvent[]> {
  const query = type ? `?type=${encodeURIComponent(type)}` : '';
  const data = await apiGet<FeedEvent[]>(`/feed${query}`);
  return data ?? [];
}
