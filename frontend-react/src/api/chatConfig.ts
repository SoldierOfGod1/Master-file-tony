/* ============================================================
   SOLDIER OF GOD — Chat Config API
   ============================================================ */

import type { ChatConfig } from '../types/api';
import { apiGet, apiPut } from './client';

export async function getChatConfig(): Promise<ChatConfig | null> {
  return apiGet<ChatConfig>('/chat/config');
}

export async function updateChatConfig(
  config: Partial<ChatConfig>,
): Promise<ChatConfig | null> {
  return apiPut<ChatConfig>('/chat/config', config);
}
