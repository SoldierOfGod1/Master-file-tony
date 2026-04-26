/* ============================================================
   SOLDIER OF GOD — Chat API (Send Messages)
   ============================================================ */

import type { Message } from '../types/api';
import { apiGet, apiPost } from './client';

export interface ConversationUsage {
  conversation_id: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  amount_zar: number;
  model: string;
}

export async function getConversationUsage(id: string): Promise<ConversationUsage | null> {
  return await apiGet<ConversationUsage>(`/conversations/${encodeURIComponent(id)}/usage`);
}

export interface SendMessagePayload {
  conversationId: string;
  message: string;
  pin?: string;
}

export async function sendMessage(
  conversationId: string,
  message: string,
  pin?: string,
): Promise<Message | null> {
  const body: SendMessagePayload = { conversationId, message };
  if (pin) {
    body.pin = pin;
  }
  return apiPost<Message>('/chat', body);
}
