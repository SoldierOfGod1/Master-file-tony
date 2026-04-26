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

/* Phase C1 — streaming-status probe.
   Frontend polls this on conversation switch / page reload to know
   whether the agent is mid-stream and the WebSocket should stay
   attached. */
export interface ConversationActiveState {
  readonly streaming: boolean;
  readonly info: {
    readonly conversation_id: string;
    readonly path: 'cli' | 'agent';
    readonly user_id?: string;
    readonly started_at: string;
  } | null;
}

export async function getConversationActive(
  id: string,
): Promise<ConversationActiveState | null> {
  return apiGet<ConversationActiveState>(
    `/conversations/${encodeURIComponent(id)}/active`,
  );
}

/* Phase C1 follow-up — stream replay.
   Returns the buffered chat.stream / chat.complete / chat.error
   payloads (oldest first) the server kept in memory while the
   client was offline. Each entry has its own `type` field so the
   caller can route it through the same handler used for live
   WebSocket events. Empty array if the buffer is empty or the
   conversation has been Forgotten (which happens on completion). */
export interface ReplayChatEvent {
  readonly conversationId: string;
  readonly type: 'stream' | 'complete' | 'error';
  readonly content: string;
  readonly metadata?: Record<string, unknown>;
}

export async function getConversationReplay(
  id: string,
): Promise<ReplayChatEvent[]> {
  const data = await apiGet<ReplayChatEvent[]>(
    `/conversations/${encodeURIComponent(id)}/replay`,
  );
  return data ?? [];
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
