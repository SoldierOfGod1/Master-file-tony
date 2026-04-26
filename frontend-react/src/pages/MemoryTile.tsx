/* ============================================================
   MemoryTile — Phase D1 + follow-up
   Operator panel for inspecting / adding / deleting agent_memory
   entries. Without this the memory layer is invisible — the
   agent reads it on every prompt but the human can't audit or
   correct what it learned.
   ============================================================ */

import { useCallback, useEffect, useState } from 'react';
import { Trash2, Plus } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import { HudChip } from '../components/shared/HudChip';
import {
  listMemory,
  createMemory,
  deleteMemory,
  type MemoryEntry,
  type MemoryKind,
} from '../api/memory';

const KIND_COLOR: Record<MemoryKind, string> = {
  preference:      '#6ff2a0',
  incident_context: '#ffaa00',
  pattern:         '#00f0ff',
  note:            '#7cc6ff',
};

const KINDS: ReadonlyArray<MemoryKind> = ['preference', 'incident_context', 'pattern', 'note'];

function formatRelative(iso: string): string {
  try {
    const ms = Date.now() - new Date(iso).getTime();
    const m = Math.round(ms / 60_000);
    if (m < 1) return 'just now';
    if (m < 60) return `${m}m ago`;
    const h = Math.round(m / 60);
    if (h < 24) return `${h}h ago`;
    const d = Math.round(h / 24);
    return `${d}d ago`;
  } catch {
    return iso;
  }
}

function MemoryRow({ entry, onDeleted }: {
  readonly entry: MemoryEntry;
  readonly onDeleted: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const onDelete = useCallback(async () => {
    if (!confirm(`Delete this ${entry.kind} memory for ${entry.user_id}?`)) return;
    setBusy(true);
    setErr(null);
    const ok = await deleteMemory(entry.id);
    setBusy(false);
    if (!ok) {
      setErr('delete failed — RAIN_SUPPORT_L2 likely off');
      return;
    }
    onDeleted();
  }, [entry, onDeleted]);

  const colour = KIND_COLOR[entry.kind] ?? '#7cc6ff';

  return (
    <div
      style={{
        padding: '6px 10px',
        marginBottom: 4,
        border: '1px solid rgba(124, 198, 255, 0.18)',
        borderLeft: `3px solid ${colour}`,
        borderRadius: 3,
        background: 'rgba(0, 240, 255, 0.02)',
        fontFamily: 'var(--font-mono, monospace)',
        fontSize: 11,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
        <HudChip color={colour}>{entry.kind}</HudChip>
        <span style={{ flex: 1, opacity: 0.65 }}>{entry.user_id}</span>
        <span style={{ fontSize: 9, opacity: 0.55 }}>{formatRelative(entry.created_at)}</span>
        <button
          type="button"
          onClick={onDelete}
          disabled={busy}
          title="Delete"
          style={{
            padding: '2px 6px',
            background: 'transparent',
            color: '#ff7b7b',
            border: '1px solid rgba(255, 123, 123, 0.4)',
            borderRadius: 3,
            cursor: 'pointer',
          }}
        >
          <Trash2 size={10} />
        </button>
      </div>
      <div style={{ marginTop: 4, opacity: 0.85, wordBreak: 'break-word' }}>
        {entry.body}
      </div>
      {err && <div style={{ marginTop: 3, color: '#ff7b7b', fontSize: 10 }}>{err}</div>}
    </div>
  );
}

function NewMemoryForm({ onCreated }: { readonly onCreated: () => void }) {
  const [open, setOpen] = useState(false);
  const [userID, setUserID] = useState('');
  const [kind, setKind] = useState<MemoryKind>('note');
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = useCallback(async () => {
    if (!userID.trim() || !body.trim()) {
      setErr('user_id and body are both required');
      return;
    }
    setBusy(true);
    setErr(null);
    const res = await createMemory(userID.trim(), kind, body.trim());
    setBusy(false);
    if (!res) {
      setErr('save failed — RAIN_SUPPORT_L2 likely off');
      return;
    }
    setBody('');
    setOpen(false);
    onCreated();
  }, [userID, kind, body, onCreated]);

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
          padding: '4px 10px',
          fontSize: 10,
          letterSpacing: '0.06em',
          textTransform: 'uppercase',
          color: '#00f0ff',
          background: 'rgba(0, 240, 255, 0.06)',
          border: '1px solid rgba(0, 240, 255, 0.4)',
          borderRadius: 3,
          cursor: 'pointer',
          fontFamily: 'var(--font-mono, monospace)',
        }}
      >
        <Plus size={10} /> add memory
      </button>
    );
  }

  return (
    <div
      style={{
        padding: 8,
        marginBottom: 8,
        border: '1px solid rgba(0, 240, 255, 0.3)',
        borderRadius: 4,
        background: 'rgba(0, 240, 255, 0.04)',
        fontFamily: 'var(--font-mono, monospace)',
      }}
    >
      <div style={{ display: 'flex', gap: 6, marginBottom: 6 }}>
        <input
          type="text"
          placeholder="user_id"
          value={userID}
          onChange={(e) => setUserID(e.target.value)}
          style={inputStyle()}
        />
        <select value={kind} onChange={(e) => setKind(e.target.value as MemoryKind)} style={inputStyle()}>
          {KINDS.map((k) => <option key={k} value={k}>{k}</option>)}
        </select>
      </div>
      <textarea
        placeholder="body — one short observation, max 2KB"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        rows={3}
        style={{ ...inputStyle(), width: '100%' }}
      />
      <div style={{ marginTop: 6, display: 'flex', gap: 6 }}>
        <button type="button" onClick={submit} disabled={busy} style={btnStyle('confirm')}>
          {busy ? 'saving…' : 'save'}
        </button>
        <button type="button" onClick={() => { setOpen(false); setErr(null); }} style={btnStyle('cancel')}>
          cancel
        </button>
      </div>
      {err && <div style={{ marginTop: 4, color: '#ff7b7b', fontSize: 10 }}>{err}</div>}
    </div>
  );
}

function inputStyle(): React.CSSProperties {
  return {
    padding: '3px 6px',
    fontSize: 11,
    color: '#cce6ff',
    background: 'rgba(0, 0, 0, 0.4)',
    border: '1px solid rgba(0, 240, 255, 0.3)',
    borderRadius: 3,
    fontFamily: 'inherit',
  };
}

function btnStyle(kind: 'confirm' | 'cancel' = 'confirm'): React.CSSProperties {
  const c = kind === 'confirm' ? '#6ff2a0' : '#7cc6ff';
  return {
    padding: '3px 10px',
    fontSize: 10,
    letterSpacing: '0.06em',
    textTransform: 'uppercase',
    color: c,
    background: 'transparent',
    border: `1px solid ${c}66`,
    borderRadius: 3,
    cursor: 'pointer',
    fontFamily: 'var(--font-mono, monospace)',
  };
}

export default function MemoryTile() {
  const [entries, setEntries] = useState<MemoryEntry[] | null>(null);
  const [filter, setFilter] = useState('');
  const [err, setErr] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await listMemory(filter.trim() || undefined);
      setEntries(data);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'unknown');
    }
  }, [filter]);

  useEffect(() => { void refresh(); }, [refresh]);

  if (err) {
    return (
      <HudPanel title="Agent Memory" accent="#ff7b7b">
        <div style={{ padding: 8, fontSize: 11, color: '#ff7b7b' }}>{err}</div>
      </HudPanel>
    );
  }

  return (
    <HudPanel
      title={`Agent Memory${entries ? ` · ${entries.length}` : ''}`}
      accent="#b980ff"
    >
      <div style={{ padding: 6, fontFamily: 'var(--font-mono, monospace)' }}>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 8 }}>
          <input
            type="text"
            placeholder="filter by user_id (blank = all users)"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            style={{ ...inputStyle(), flex: 1 }}
          />
          <NewMemoryForm onCreated={refresh} />
        </div>

        {!entries && <div style={{ fontSize: 11, opacity: 0.6 }}>loading…</div>}
        {entries && entries.length === 0 && (
          <div style={{ fontSize: 11, opacity: 0.6 }}>
            // no memory entries yet. The agent writes here when it learns
            // something memorable; you can also pin entries manually.
          </div>
        )}
        {entries && entries.map((e) => (
          <MemoryRow key={e.id} entry={e} onDeleted={refresh} />
        ))}

        <div style={{ marginTop: 6, fontSize: 9, opacity: 0.55 }}>
          Phase D1 — what the agent recalls between sessions. Inspecting / pruning here directly
          influences every subsequent agent run for the user. RAIN_SUPPORT_L2 required for writes.
        </div>
      </div>
    </HudPanel>
  );
}
