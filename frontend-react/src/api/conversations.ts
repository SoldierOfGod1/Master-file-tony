/* ============================================================
   SOLDIER OF GOD — Conversations API
   ============================================================ */

import type { Conversation, Message } from '../types/api';
import { apiGet, apiPost, apiPut } from './client';

export interface ConversationWithMessages extends Conversation {
  messages: Message[];
}

export async function listConversations(): Promise<Conversation[]> {
  const data = await apiGet<Conversation[]>('/conversations');
  return data ?? [];
}

export async function createConversation(
  title: string,
  projectDir: string,
): Promise<Conversation | null> {
  return apiPost<Conversation>('/conversations', { title, projectDir });
}

export async function getConversation(
  id: string,
): Promise<ConversationWithMessages | null> {
  return apiGet<ConversationWithMessages>(
    `/conversations/${encodeURIComponent(id)}`,
  );
}

export async function updateConversation(
  id: string,
  updates: Partial<Pick<Conversation, 'title' | 'status'>>,
): Promise<Conversation | null> {
  return apiPut<Conversation>(
    `/conversations/${encodeURIComponent(id)}`,
    updates,
  );
}

export async function exportConversation(id: string): Promise<string | null> {
  const url = `/api/v1/conversations/${encodeURIComponent(id)}/export?format=md`;

  try {
    const res = await fetch(url, {
      method: 'GET',
      headers: { Accept: 'text/markdown' },
    });

    if (!res.ok) return null;
    return res.text();
  } catch {
    return null;
  }
}
