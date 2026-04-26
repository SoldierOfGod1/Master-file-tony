/* ============================================================
   ChatPage — Soldier of God Primary Chat Interface
   Real-time streaming conversation with the orchestrator.
   ============================================================ */

import {
  useState,
  useEffect,
  useRef,
  useCallback,
  type ChangeEvent,
  type KeyboardEvent,
  type FormEvent,
} from 'react';
import {
  Plus,
  SendHorizonal,
  Download,
  Lock,
  Unlock,
  MessageSquare,
} from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import Modal from '../components/shared/Modal';
import type { Conversation, Message } from '../types/api';
import {
  listConversations,
  createConversation,
  getConversation,
  exportConversation,
} from '../api/conversations';
import { sendMessage, getConversationUsage } from '../api/chat';
import LoopOperatorPanel from './LoopOperatorPanel';
import ContextGauge, { CompactBanner, estimateMessagesPct } from './ContextGauge';
import styles from './ChatPage.module.css';

/* ----- Common project dirs ----- */

const PROJECT_DIRS: readonly string[] = [
  '~/projects',
  '~/projects/backend',
  '~/projects/frontend',
  '~/projects/infra',
  '~/projects/ml',
] as const;

/* ----- Phase C3 onboarding cards ----------------------------
   Shown when the conversation is empty. Lowers the barrier for new
   ops users staring at a blank textarea — clicking a card prefills
   the prompt so the user can refine and send. Each card is one of
   the four high-frequency rain ops asks. The intent classifier in
   Phase A1 will route the resulting prompt; the dispatcher in
   Phase A3 picks API or CLI path. */
const ONBOARDING_PROMPTS: ReadonlyArray<{
  readonly label: string;
  readonly hint: string;
  readonly prompt: string;
}> = [
  {
    label: 'Look up a customer',
    hint: 'Customer 360 + cascade + audit trail',
    prompt: 'Look up baptista.manuel@rain.co.za',
  },
  {
    label: 'Why was a payment declined?',
    hint: 'Cross-references payment, cdr, communications',
    prompt: 'Why did the last payment for ',
  },
  {
    label: 'Check platform health',
    hint: 'Axiom, GaussDB, satellite apps + open incidents',
    prompt: 'Is everything up right now?',
  },
  {
    label: 'Find a SIM by IMSI',
    hint: 'Walks the cascade, surfaces override + swap state',
    prompt: 'Trace IMSI 655',
  },
];

function OnboardingCards({ onPick }: { readonly onPick: (prompt: string) => void }) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
        gap: 10,
        padding: 16,
        marginTop: 24,
      }}
    >
      <div style={{ gridColumn: '1 / -1', textAlign: 'center', marginBottom: 8 }}>
        <div style={{ fontSize: 13, opacity: 0.85, fontFamily: 'var(--font-mono, monospace)' }}>
          Soldier of God · ops agent
        </div>
        <div style={{ fontSize: 11, opacity: 0.55, marginTop: 2 }}>
          Pick a starter or ask anything
        </div>
      </div>
      {ONBOARDING_PROMPTS.map((c) => (
        <button
          key={c.label}
          type="button"
          onClick={() => onPick(c.prompt)}
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'flex-start',
            gap: 4,
            padding: '12px 14px',
            background: 'rgba(0, 240, 255, 0.04)',
            border: '1px solid rgba(0, 240, 255, 0.22)',
            borderRadius: 6,
            color: 'inherit',
            cursor: 'pointer',
            textAlign: 'left',
            fontFamily: 'var(--font-mono, monospace)',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.background = 'rgba(0, 240, 255, 0.12)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.background = 'rgba(0, 240, 255, 0.04)';
          }}
        >
          <div style={{ fontSize: 12, color: '#00f0ff', letterSpacing: '0.04em' }}>
            {c.label}
          </div>
          <div style={{ fontSize: 10, opacity: 0.7 }}>{c.hint}</div>
        </button>
      ))}
    </div>
  );
}

/* ----- WebSocket chat event shapes ----- */

interface ChatStreamPayload {
  conversationId: string;
  type: 'stream' | 'complete' | 'error';
  content: string;
  metadata?: {
    duration_ms?: number;
    files_changed?: string[];
  };
}

/* ----- Display message: local extension of Message ----- */

interface DisplayMessage {
  readonly id: number;
  readonly conversationId: string;
  readonly role: string;
  readonly content: string;
  readonly source: string;
  readonly metadata: Record<string, unknown>;
  readonly createdAt: string;
  readonly isStreaming?: boolean;
  readonly isError?: boolean;
}

/* ----- Helpers ----- */

function formatTime(iso: string): string {
  try {
    const d = new Date(iso);
    const now = new Date();
    const isToday =
      d.getFullYear() === now.getFullYear() &&
      d.getMonth() === now.getMonth() &&
      d.getDate() === now.getDate();
    if (isToday) {
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  } catch {
    return '';
  }
}

function truncateDir(dir: string, maxLen: number = 28): string {
  if (dir.length <= maxLen) return dir;
  return '...' + dir.slice(dir.length - maxLen + 3);
}

function toDisplayMessage(m: Message): DisplayMessage {
  return {
    id: m.id,
    conversationId: m.conversationId,
    role: m.role,
    content: m.content,
    source: m.source,
    metadata: m.metadata,
    createdAt: m.createdAt,
    isStreaming: false,
    isError: false,
  };
}

/* ----- Source Badge ----- */

function SourceBadge({ source }: { readonly source: string }) {
  const isDiscord =
    source.toLowerCase() === 'discord';
  return (
    <span
      className={`${styles.sourceBadge} ${
        isDiscord ? styles.sourceBadgeDiscord : styles.sourceBadgeUI
      }`}
    >
      {isDiscord ? 'Discord' : 'UI'}
    </span>
  );
}

/* ----- Message Bubble ----- */

function MessageBubble({ msg }: { readonly msg: DisplayMessage }) {
  if (msg.role === 'system') {
    return (
      <div className={`${styles.messageRow} ${styles.messageRowSystem}`}>
        <div className={`${styles.messageBubble} ${styles.messageBubbleSystem}`}>
          {msg.content}
        </div>
      </div>
    );
  }

  const isUser = msg.role === 'user';
  const isError = msg.isError === true;

  return (
    <div
      className={`${styles.messageRow} ${
        isUser ? styles.messageRowUser : styles.messageRowAssistant
      }`}
    >
      {!isUser && (
        <div className={styles.messageLabel}>
          Soldier of God
          <SourceBadge source={msg.source} />
        </div>
      )}

      <div
        className={`${styles.messageBubble} ${
          isUser
            ? styles.messageBubbleUser
            : isError
              ? styles.errorBubble
              : styles.messageBubbleAssistant
        }`}
      >
        {msg.content}
        {msg.isStreaming && <span className={styles.streamingCursor} />}
      </div>

      {isUser && (
        <div className={styles.messageSourceBadge}>
          <SourceBadge source={msg.source} />
        </div>
      )}

      {!isUser && !msg.isStreaming && msg.metadata && (
        <div className={styles.messageMeta}>
          {typeof msg.metadata.duration_ms === 'number' && (
            <span>{(msg.metadata.duration_ms as number / 1000).toFixed(1)}s</span>
          )}
          {Array.isArray(msg.metadata.files_changed) &&
            (msg.metadata.files_changed as string[]).length > 0 && (
              <span>
                {(msg.metadata.files_changed as string[]).length} file(s) changed
              </span>
            )}
        </div>
      )}
    </div>
  );
}

/* ----- Typing Indicator ----- */

function TypingIndicator() {
  return (
    <div className={styles.typingIndicator}>
      <div className={styles.typingDot} />
      <div className={styles.typingDot} />
      <div className={styles.typingDot} />
    </div>
  );
}

/* ================================================================
   Main Page Component
   ================================================================ */

export default function ChatPage() {
  const { state } = useCommandCentre();

  // Conversations
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeConvId, setActiveConvId] = useState<string | null>(null);
  const [messages, setMessages] = useState<DisplayMessage[]>([]);

  // Real token total for the active conversation. Re-fetched when the
  // active convo changes and after every assistant response completes.
  const [realTokens, setRealTokens] = useState<number | undefined>(undefined);
  const [modelHint, setModelHint] = useState<string | undefined>(undefined);

  // Input
  const [inputText, setInputText] = useState('');
  const [projectDir, setProjectDir] = useState(PROJECT_DIRS[0]);
  const [customDir, setCustomDir] = useState('');

  // PIN
  const [pin, setPin] = useState<string | null>(null);
  const [pinModalOpen, setPinModalOpen] = useState(false);
  const [pinInput, setPinInput] = useState('');

  // New chat modal
  const [newChatOpen, setNewChatOpen] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [newDir, setNewDir] = useState(PROJECT_DIRS[0]);

  // Streaming state
  const [isWaiting, setIsWaiting] = useState(false);
  const [isStreaming, setIsStreaming] = useState(false);
  const streamingIdRef = useRef<number>(-1);

  // Refs
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const activeConvIdRef = useRef<string | null>(null);
  useEffect(() => {
    activeConvIdRef.current = activeConvId;
  }, [activeConvId]);

  /* ----- Auto-scroll ----- */

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, isWaiting, scrollToBottom]);

  /* ----- Load conversations ----- */

  useEffect(() => {
    let cancelled = false;
    async function load() {
      const data = await listConversations();
      if (!cancelled) {
        setConversations(data);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  /* ----- Select conversation ----- */

  const selectConversation = useCallback(async (id: string) => {
    setActiveConvId(id);
    setMessages([]);
    setIsWaiting(false);
    setIsStreaming(false);
    setRealTokens(undefined);
    setModelHint(undefined);

    const data = await getConversation(id);
    if (data) {
      setMessages(data.messages.map(toDisplayMessage));
    }

    // Pull the real token total from cost_records for this convo.
    const usage = await getConversationUsage(id);
    if (usage) {
      setRealTokens(usage.total_tokens);
      setModelHint(usage.model);
    }
  }, []);

  // Refresh usage after every assistant response completes so the gauge
  // stays accurate without needing a manual reload.
  const refreshUsage = useCallback(async () => {
    const id = activeConvIdRef.current;
    if (!id) return;
    const usage = await getConversationUsage(id);
    if (!usage) return;
    setRealTokens(usage.total_tokens);
    if (usage.model) setModelHint(usage.model);
  }, []);

  /* ----- WebSocket listener for chat events ----- */

  useEffect(() => {
    // We rely on the global lastMessage from context being dispatched.
    // However, the chat events are specific types not in the main switch.
    // We need a direct approach here.
  }, [state.gatewayStatus]);

  // Listen to raw WebSocket messages for chat events
  // We tap into the shared WS via the context's last message
  useEffect(() => {
    // The context handles WS and dispatches last message.
    // Chat events come as type "chat.stream", "chat.complete", "chat.error"
    // These fall into the default case of the context switch, which triggers refreshAll.
    // We need to listen for them separately.
    // For now we set up a secondary listener on the same endpoint.

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;
    let ws: WebSocket | null = null;
    let mounted = true;

    function connect() {
      try {
        ws = new WebSocket(wsUrl);

        ws.onmessage = (event: MessageEvent) => {
          if (!mounted) return;
          try {
            const envelope = JSON.parse(event.data as string) as {
              type: string;
              payload: unknown;
            };

            if (
              envelope.type !== 'chat.stream' &&
              envelope.type !== 'chat.complete' &&
              envelope.type !== 'chat.error'
            ) {
              return;
            }

            const payload = envelope.payload as ChatStreamPayload;

            // Read active conv id from ref — do NOT nest state updates inside
            // setState's updater function. React StrictMode invokes updaters
            // twice to detect impure side effects; nested setMessages / ref
            // mutations would then fire twice, causing duplicated content
            // and spurious extra bubbles.
            if (payload.conversationId !== activeConvIdRef.current) return;

            if (envelope.type === 'chat.stream') {
              setIsWaiting(false);
              setIsStreaming(true);

              // Pre-compute a new id if we might need it. Assign to the ref
              // BEFORE calling setMessages so React's StrictMode double-invoke
              // of the updater is idempotent (ref mutation happens exactly
              // once, deterministically).
              let newId = streamingIdRef.current;
              if (newId < 0) {
                newId = Date.now();
                streamingIdRef.current = newId;
              }
              const createdAt = new Date().toISOString();

              setMessages((prev) => {
                const idx = prev.findIndex(
                  (m) => m.id === newId && m.isStreaming,
                );
                if (idx >= 0) {
                  // Guard: StrictMode may invoke the updater twice with the
                  // same prev; on the second invocation we'd re-append the
                  // same chunk. Detect by checking if the tail already ends
                  // with this exact chunk.
                  const existing = prev[idx].content;
                  if (
                    payload.content !== '' &&
                    existing.endsWith(payload.content)
                  ) {
                    return prev;
                  }
                  const updated = [...prev];
                  updated[idx] = {
                    ...updated[idx],
                    content: existing + payload.content,
                  };
                  return updated;
                }
                const streamMsg: DisplayMessage = {
                  id: newId,
                  conversationId: payload.conversationId,
                  role: 'assistant',
                  content: payload.content,
                  source: 'ui',
                  metadata: {},
                  createdAt,
                  isStreaming: true,
                  isError: false,
                };
                return [...prev, streamMsg];
              });
            } else if (envelope.type === 'chat.complete') {
              setIsWaiting(false);
              setIsStreaming(false);
              // Pull the updated token total from cost_records. A small
              // delay lets the backend finish writing the row before we
              // query it.
              setTimeout(() => { void refreshUsage(); }, 400);

              const streamingId = streamingIdRef.current;
              streamingIdRef.current = -1;

              setMessages((prev) => {
                const idx = prev.findIndex((m) => m.id === streamingId);
                if (idx >= 0) {
                  const updated = [...prev];
                  updated[idx] = {
                    ...updated[idx],
                    content: payload.content,
                    metadata:
                      (payload.metadata as Record<string, unknown>) ?? {},
                    isStreaming: false,
                  };
                  return updated;
                }
                // Complete without prior stream — but guard against duplicates
                // from StrictMode double-invocation: if last message matches,
                // don't add a duplicate.
                const last = prev[prev.length - 1];
                if (
                  last &&
                  last.role === 'assistant' &&
                  last.content === payload.content &&
                  !last.isStreaming
                ) {
                  return prev;
                }
                return [
                  ...prev,
                  {
                    id: Date.now(),
                    conversationId: payload.conversationId,
                    role: 'assistant',
                    content: payload.content,
                    source: 'ui',
                    metadata:
                      (payload.metadata as Record<string, unknown>) ?? {},
                    createdAt: new Date().toISOString(),
                    isStreaming: false,
                    isError: false,
                  },
                ];
              });
            } else if (envelope.type === 'chat.error') {
              setIsWaiting(false);
              setIsStreaming(false);

              const streamingId = streamingIdRef.current;
              streamingIdRef.current = -1;

              setMessages((prev) => {
                // Remove any pending streaming message
                const cleaned = prev.filter(
                  (m) => m.id !== streamingId || !m.isStreaming,
                );
                return [
                  ...cleaned,
                  {
                    id: Date.now(),
                    conversationId: payload.conversationId,
                    role: 'assistant',
                    content: payload.content,
                    source: 'ui',
                    metadata: {},
                    createdAt: new Date().toISOString(),
                    isStreaming: false,
                    isError: true,
                  },
                ];
              });
            }
          } catch {
            // Ignore parse errors from non-chat messages
          }
        };

        ws.onclose = () => {
          if (mounted) {
            setTimeout(connect, 3000);
          }
        };
      } catch {
        // Retry after delay
        if (mounted) {
          setTimeout(connect, 5000);
        }
      }
    }

    connect();

    return () => {
      mounted = false;
      if (ws) {
        ws.onmessage = null;
        ws.onclose = null;
        ws.close();
      }
    };
  }, []);

  /* ----- Send message ----- */

  const handleSend = useCallback(async () => {
    const text = inputText.trim();
    if (!text || !activeConvId) return;

    // Optimistic user message
    const userMsg: DisplayMessage = {
      id: Date.now(),
      conversationId: activeConvId,
      role: 'user',
      content: text,
      source: 'ui',
      metadata: {},
      createdAt: new Date().toISOString(),
      isStreaming: false,
      isError: false,
    };

    setMessages((prev) => [...prev, userMsg]);
    setInputText('');
    setIsWaiting(true);

    // Reset textarea height
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }

    await sendMessage(activeConvId, text, pin ?? undefined);
  }, [inputText, activeConvId, pin]);

  /* ----- Textarea auto-grow + key handler ----- */

  const handleTextareaChange = useCallback(
    (e: ChangeEvent<HTMLTextAreaElement>) => {
      setInputText(e.target.value);
      const el = e.target;
      el.style.height = 'auto';
      el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
    },
    [],
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        void handleSend();
      }
    },
    [handleSend],
  );

  /* ----- Create conversation ----- */

  const handleCreateConversation = useCallback(
    async (e: FormEvent) => {
      e.preventDefault();
      const title = newTitle.trim();
      if (!title) return;

      const conv = await createConversation(title, newDir);
      if (conv) {
        setConversations((prev) => [conv, ...prev]);
        setActiveConvId(conv.id);
        setMessages([]);
        setNewChatOpen(false);
        setNewTitle('');
      }
    },
    [newTitle, newDir],
  );

  /* ----- Export conversation ----- */

  const handleExport = useCallback(async () => {
    if (!activeConvId) return;
    const md = await exportConversation(activeConvId);
    if (md) {
      const blob = new Blob([md], { type: 'text/markdown' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `conversation-${activeConvId}.md`;
      a.click();
      URL.revokeObjectURL(url);
    }
  }, [activeConvId]);

  /* ----- PIN handling ----- */

  const handlePinSubmit = useCallback(
    (e: FormEvent) => {
      e.preventDefault();
      if (pinInput.trim()) {
        setPin(pinInput.trim());
        setPinModalOpen(false);
        setPinInput('');
      }
    },
    [pinInput],
  );

  const handlePinToggle = useCallback(() => {
    if (pin) {
      setPin(null);
    } else {
      setPinModalOpen(true);
    }
  }, [pin]);

  /* ----- Active conversation data ----- */

  const activeConv = conversations.find((c) => c.id === activeConvId) ?? null;

  /* ================================================================
     RENDER
     ================================================================ */

  // Empty state: no conversations at all
  if (conversations.length === 0) {
    return (
      <div className={styles.page}>
        <div className={styles.emptyState}>
          <div className={styles.emptyIcon}>
            <MessageSquare size={36} />
          </div>
          <h2 className={styles.emptyTitle}>
            Start a conversation with Soldier of God
          </h2>
          <p className={styles.emptySubtitle}>
            Interact with the orchestrator through a real-time streaming chat
            interface. Your conversations are saved and can be exported.
          </p>
          <button
            type="button"
            className={styles.emptyBtn}
            onClick={() => setNewChatOpen(true)}
          >
            <Plus size={16} />
            New Chat
          </button>

          {/* New chat modal */}
          <Modal
            isOpen={newChatOpen}
            onClose={() => setNewChatOpen(false)}
            title="New Conversation"
          >
            <form onSubmit={handleCreateConversation}>
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>Title</label>
                <input
                  type="text"
                  className={styles.formInput}
                  placeholder="What are you working on?"
                  value={newTitle}
                  onChange={(e) => setNewTitle(e.target.value)}
                  autoFocus
                />
              </div>
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>Project Directory</label>
                <select
                  className={`${styles.formInput}`}
                  value={newDir}
                  onChange={(e) => setNewDir(e.target.value)}
                >
                  {PROJECT_DIRS.map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                </select>
              </div>
              <div className={styles.formActions}>
                <button
                  type="button"
                  className={styles.formBtnSecondary}
                  onClick={() => setNewChatOpen(false)}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className={styles.formBtnPrimary}
                  disabled={!newTitle.trim()}
                >
                  Create
                </button>
              </div>
            </form>
          </Modal>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.page}>
      {/* ---- Left Panel: Conversations ---- */}
      <div className={styles.sidebar}>
        <div className={styles.sidebarHeader}>
          <span className={styles.sidebarTitle}>Conversations</span>
          <button
            type="button"
            className={styles.newChatBtn}
            onClick={() => setNewChatOpen(true)}
          >
            <Plus size={14} />
            New
          </button>
        </div>

        <div className={styles.conversationList}>
          {conversations.map((conv) => (
            <div
              key={conv.id}
              className={`${styles.conversationItem} ${
                conv.id === activeConvId ? styles.conversationItemActive : ''
              }`}
              onClick={() => void selectConversation(conv.id)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  void selectConversation(conv.id);
                }
              }}
            >
              <span className={styles.convTitle}>{conv.title}</span>
              <div className={styles.convMeta}>
                <span className={styles.convDir}>
                  {truncateDir(conv.projectDir)}
                </span>
                <SourceBadge source={conv.source} />
                <span className={styles.convTime}>
                  {formatTime(conv.updatedAt)}
                </span>
              </div>
            </div>
          ))}
        </div>

        {/* Loop Operator — pause/kill active queue workers */}
        <div style={{ padding: '8px', borderTop: '1px solid rgba(124,198,255,0.15)' }}>
          <LoopOperatorPanel />
        </div>
      </div>

      {/* ---- Right Panel: Chat ---- */}
      <div className={styles.chatArea}>
        {activeConv ? (
          <>
            {/* Header */}
            <div className={styles.chatHeader}>
              <span className={styles.chatHeaderTitle}>
                {activeConv.title}
              </span>
              <div className={styles.chatHeaderActions}>
                <ContextGauge
                  messages={messages}
                  realTokens={realTokens}
                  modelHint={modelHint}
                />
                <button
                  type="button"
                  className={styles.iconBtn}
                  onClick={handleExport}
                  title="Export as Markdown"
                >
                  <Download size={16} />
                </button>
              </div>
            </div>
            <CompactBanner
              pct={
                typeof realTokens === 'number' && realTokens > 0
                  ? Math.min(100, Math.round((realTokens / 200_000) * 100))
                  : estimateMessagesPct(messages, modelHint)
              }
            />

            {/* Messages */}
            <div className={styles.messagesContainer}>
              {messages.length === 0 && !isWaiting && !isStreaming && (
                <OnboardingCards onPick={(p) => setInputText(p)} />
              )}
              {messages.map((msg) => (
                <MessageBubble key={msg.id} msg={msg} />
              ))}
              {isWaiting && !isStreaming && <TypingIndicator />}
              <div ref={messagesEndRef} />
            </div>

            {/* Input Area */}
            <div className={styles.inputArea}>
              <div className={styles.inputRow}>
                <div className={styles.inputWrapper}>
                  <textarea
                    ref={textareaRef}
                    className={styles.textarea}
                    placeholder="Message Soldier of God..."
                    value={inputText}
                    onChange={handleTextareaChange}
                    onKeyDown={handleKeyDown}
                    rows={1}
                  />
                </div>
                <button
                  type="button"
                  className={styles.sendBtn}
                  onClick={() => void handleSend()}
                  disabled={!inputText.trim() || isWaiting || isStreaming}
                  title="Send message"
                >
                  <SendHorizonal size={18} />
                </button>
              </div>

              <div className={styles.inputControls}>
                <select
                  className={styles.projectSelect}
                  value={projectDir}
                  onChange={(e) => setProjectDir(e.target.value)}
                  title="Project directory"
                >
                  {PROJECT_DIRS.map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                  <option value="__custom__">Custom...</option>
                </select>
                {projectDir === '__custom__' && (
                  <input
                    type="text"
                    className={styles.projectSelect}
                    placeholder="/path/to/project"
                    value={customDir}
                    onChange={(e) => setCustomDir(e.target.value)}
                    style={{ minWidth: 180 }}
                  />
                )}

                <button
                  type="button"
                  className={`${styles.pinBtn} ${pin ? styles.pinBtnActive : ''}`}
                  onClick={handlePinToggle}
                  title={pin ? 'PIN active (click to clear)' : 'Enter PIN'}
                >
                  {pin ? <Unlock size={14} /> : <Lock size={14} />}
                </button>
              </div>
            </div>
          </>
        ) : (
          <div className={styles.noConversation}>
            <MessageSquare size={28} strokeWidth={1.5} />
            <span>Select a conversation to begin</span>
          </div>
        )}
      </div>

      {/* ---- New Chat Modal ---- */}
      <Modal
        isOpen={newChatOpen}
        onClose={() => setNewChatOpen(false)}
        title="New Conversation"
      >
        <form onSubmit={handleCreateConversation}>
          <div className={styles.formGroup}>
            <label className={styles.formLabel}>Title</label>
            <input
              type="text"
              className={styles.formInput}
              placeholder="What are you working on?"
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              autoFocus
            />
          </div>
          <div className={styles.formGroup}>
            <label className={styles.formLabel}>Project Directory</label>
            <select
              className={styles.formInput}
              value={newDir}
              onChange={(e) => setNewDir(e.target.value)}
            >
              {PROJECT_DIRS.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </div>
          <div className={styles.formActions}>
            <button
              type="button"
              className={styles.formBtnSecondary}
              onClick={() => setNewChatOpen(false)}
            >
              Cancel
            </button>
            <button
              type="submit"
              className={styles.formBtnPrimary}
              disabled={!newTitle.trim()}
            >
              Create
            </button>
          </div>
        </form>
      </Modal>

      {/* ---- PIN Modal ---- */}
      <Modal
        isOpen={pinModalOpen}
        onClose={() => {
          setPinModalOpen(false);
          setPinInput('');
        }}
        title="Enter Security PIN"
      >
        <form onSubmit={handlePinSubmit}>
          <div className={styles.formGroup}>
            <label className={styles.formLabel}>PIN</label>
            <input
              type="password"
              className={`${styles.formInput} ${styles.pinInput}`}
              placeholder="****"
              value={pinInput}
              onChange={(e) => setPinInput(e.target.value)}
              autoFocus
              maxLength={8}
            />
          </div>
          <div className={styles.formActions}>
            <button
              type="button"
              className={styles.formBtnSecondary}
              onClick={() => {
                setPinModalOpen(false);
                setPinInput('');
              }}
            >
              Cancel
            </button>
            <button
              type="submit"
              className={styles.formBtnPrimary}
              disabled={!pinInput.trim()}
            >
              Unlock
            </button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
