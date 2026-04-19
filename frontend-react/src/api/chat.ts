/* ============================================================
   SOLDIER OF GOD — Chat API (Send Messages)
   ============================================================ */

import type { Message } from '../types/api';
import { apiPost } from './client';

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
