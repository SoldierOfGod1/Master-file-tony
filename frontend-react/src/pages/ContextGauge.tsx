/* ============================================================
   ContextGauge — chat-header context-window usage meter.
   Estimates tokens from message char count (÷ 4) and warns when
   the running total approaches the model's context ceiling.
   ============================================================ */

import { useMemo } from 'react';
import { Gauge, AlertTriangle } from 'lucide-react';

interface Message {
  content: string;
  role: string;
}

interface Props {
  readonly messages: readonly Message[];
  readonly modelHint?: string;
}

// Approximate context windows for the Claude CLI's default models.
// Override by passing modelHint — we pick the first match substring.
const MODEL_WINDOWS: Array<{ match: string; tokens: number; label: string }> = [
  { match: 'opus-4-7', tokens: 1_000_000, label: 'Opus 4.7 (1M)' },
  { match: 'opus',     tokens: 200_000,   label: 'Opus (200K)' },
  { match: 'sonnet',   tokens: 200_000,   label: 'Sonnet (200K)' },
  { match: 'haiku',    tokens: 200_000,   label: 'Haiku (200K)' },
];

const DEFAULT_WINDOW = { tokens: 200_000, label: 'Sonnet (200K est)' };
const SYSTEM_PROMPT_OVERHEAD = 4_000;

function estimateTokens(text: string): number {
  // Rough approximation — works well enough for a chat-header gauge.
  return Math.ceil(text.length / 4);
}

export default function ContextGauge({ messages, modelHint }: Props) {
  const window = useMemo(() => {
    const hint = (modelHint ?? '').toLowerCase();
    for (const m of MODEL_WINDOWS) {
      if (hint.includes(m.match)) return { tokens: m.tokens, label: m.label };
    }
    return DEFAULT_WINDOW;
  }, [modelHint]);

  const used = useMemo(() => {
    let total = SYSTEM_PROMPT_OVERHEAD;
    for (const m of messages) total += estimateTokens(m.content ?? '');
    return total;
  }, [messages]);

  const pct = Math.min(100, Math.round((used / window.tokens) * 100));

  const color = pct >= 90 ? '#ff7b7b' : pct >= 70 ? '#ffb86b' : '#6ff2a0';

  return (
    <div
      title={`~${used.toLocaleString()} / ${window.tokens.toLocaleString()} tokens (${window.label})`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        padding: '4px 8px',
        fontFamily: 'var(--font-mono, monospace)',
        fontSize: 11,
        border: `1px solid ${color}55`,
        borderRadius: 4,
        color: 'var(--ink, #e6f6ff)',
      }}
    >
      <Gauge size={12} color={color} />
      <div
        style={{
          width: 80,
          height: 6,
          borderRadius: 3,
          background: 'rgba(124,198,255,0.18)',
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            width: `${pct}%`,
            height: '100%',
            background: color,
            transition: 'width 180ms ease',
          }}
        />
      </div>
      <span style={{ color, minWidth: 28, textAlign: 'right' }}>{pct}%</span>
    </div>
  );
}

/** Standalone banner for when context is getting full. */
export function CompactBanner({ pct }: { readonly pct: number }) {
  if (pct < 70) return null;
  const critical = pct >= 90;
  const color = critical ? '#ff7b7b' : '#ffb86b';
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '6px 12px',
        background: `${color}18`,
        borderLeft: `3px solid ${color}`,
        color,
        fontSize: 11,
        fontFamily: 'var(--font-mono, monospace)',
      }}
    >
      <AlertTriangle size={12} />
      <span>
        Context ~{pct}% full — {critical ? 'run /compact now' : 'consider running /compact soon'} to free room.
      </span>
    </div>
  );
}

export function estimateMessagesPct(messages: readonly Message[], modelHint?: string): number {
  let total = SYSTEM_PROMPT_OVERHEAD;
  for (const m of messages) total += estimateTokens(m.content ?? '');
  const hint = (modelHint ?? '').toLowerCase();
  let limit = DEFAULT_WINDOW.tokens;
  for (const w of MODEL_WINDOWS) {
    if (hint.includes(w.match)) {
      limit = w.tokens;
      break;
    }
  }
  return Math.min(100, Math.round((total / limit) * 100));
}
