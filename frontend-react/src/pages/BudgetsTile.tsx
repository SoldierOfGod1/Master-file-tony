/* ============================================================
   BudgetsTile — Phase B3 + D-series follow-up
   Shows weekly Anthropic-API spend per user vs cap. Inline edit
   on the cap when RAIN_SUPPORT_L2=true on the server (PUT 403s
   otherwise; we handle both cases gracefully).
   ============================================================ */

import { useCallback, useEffect, useState } from 'react';
import HudPanel from '../components/shared/HudPanel';
import { HudChip } from '../components/shared/HudChip';
import { listBudgets, setBudgetCap, type BudgetState } from '../api/budgets';

const VERDICT_COLOR: Record<string, string> = {
  ok:      '#6ff2a0',
  warn:    '#ffaa00',
  blocked: '#ff3355',
};

function formatRand(value: number): string {
  return `R${value.toLocaleString('en-ZA', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function BudgetRow({ b, onUpdated }: {
  readonly b: BudgetState;
  readonly onUpdated: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(String(b.cap_zar));
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const colour = VERDICT_COLOR[b.verdict] ?? '#7cc6ff';
  const pct = Math.min(100, Math.max(0, b.pct_spent));

  const submit = useCallback(async () => {
    const n = parseFloat(draft);
    if (!Number.isFinite(n) || n < 0) {
      setErr('cap must be a non-negative number');
      return;
    }
    setSaving(true);
    setErr(null);
    try {
      const res = await setBudgetCap(b.user_id, n);
      if (!res) {
        setErr('save failed — RAIN_SUPPORT_L2 likely off on the server');
      } else {
        setEditing(false);
        onUpdated();
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'unknown error');
    } finally {
      setSaving(false);
    }
  }, [b.user_id, draft, onUpdated]);

  return (
    <div
      style={{
        padding: '8px 10px',
        marginBottom: 6,
        border: '1px solid rgba(124, 198, 255, 0.18)',
        borderLeft: `3px solid ${colour}`,
        borderRadius: 4,
        background: 'rgba(0, 240, 255, 0.02)',
        fontFamily: 'var(--font-mono, monospace)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
        <span style={{ flex: 1, fontSize: 11 }}>{b.user_id}</span>
        <HudChip color={colour}>{b.verdict.toUpperCase()}</HudChip>
        <span style={{ fontSize: 11, opacity: 0.85 }}>
          {formatRand(b.spent_zar)} / {formatRand(b.cap_zar)}
        </span>
        <span style={{ fontSize: 10, opacity: 0.65 }}>
          {b.pct_spent.toFixed(0)}%
        </span>
      </div>

      {/* Progress bar */}
      <div
        style={{
          marginTop: 6,
          height: 4,
          background: 'rgba(124, 198, 255, 0.08)',
          borderRadius: 2,
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            width: `${pct}%`,
            height: '100%',
            background: colour,
            transition: 'width 200ms ease',
          }}
        />
      </div>

      {/* Edit row */}
      <div style={{ marginTop: 6, display: 'flex', gap: 6, alignItems: 'center' }}>
        {!editing ? (
          <button
            type="button"
            onClick={() => { setDraft(String(b.cap_zar)); setEditing(true); }}
            style={btnStyle()}
          >
            edit cap
          </button>
        ) : (
          <>
            <input
              type="number"
              min="0"
              step="0.01"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              style={{
                width: 90,
                padding: '2px 6px',
                fontSize: 11,
                background: 'rgba(0, 0, 0, 0.4)',
                color: '#cce6ff',
                border: '1px solid rgba(0, 240, 255, 0.3)',
                borderRadius: 3,
                fontFamily: 'inherit',
              }}
            />
            <button type="button" onClick={submit} disabled={saving} style={btnStyle('confirm')}>
              {saving ? 'saving…' : 'save'}
            </button>
            <button type="button" onClick={() => setEditing(false)} style={btnStyle('cancel')}>
              cancel
            </button>
          </>
        )}
        {err && (
          <span style={{ fontSize: 10, color: '#ff7b7b', marginLeft: 6 }}>{err}</span>
        )}
      </div>
    </div>
  );
}

function btnStyle(kind: 'edit' | 'confirm' | 'cancel' = 'edit'): React.CSSProperties {
  const colour = kind === 'confirm' ? '#6ff2a0' : kind === 'cancel' ? '#7cc6ff' : '#00f0ff';
  return {
    padding: '2px 8px',
    fontSize: 9,
    letterSpacing: '0.06em',
    textTransform: 'uppercase',
    color: colour,
    background: 'transparent',
    border: `1px solid ${colour}66`,
    borderRadius: 3,
    cursor: 'pointer',
    fontFamily: 'inherit',
  };
}

export default function BudgetsTile() {
  const [budgets, setBudgets] = useState<BudgetState[] | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await listBudgets();
      // Sort: blocked first, then warn, then ok. Within each tier
      // by spend desc so the busy users surface together.
      const order: Record<string, number> = { blocked: 0, warn: 1, ok: 2 };
      const sorted = [...data].sort((a, b) => {
        const ao = order[a.verdict] ?? 3;
        const bo = order[b.verdict] ?? 3;
        if (ao !== bo) return ao - bo;
        return b.spent_zar - a.spent_zar;
      });
      setBudgets(sorted);
      setLoadErr(null);
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : 'unknown error');
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  if (loadErr) {
    return (
      <HudPanel title="Agent Budgets" accent="#ff7b7b">
        <div style={{ padding: 8, fontSize: 11, color: '#ff7b7b' }}>
          {loadErr}
        </div>
      </HudPanel>
    );
  }

  if (!budgets) {
    return (
      <HudPanel title="Agent Budgets" accent="#7cc6ff">
        <div style={{ padding: 8, fontSize: 11, opacity: 0.7 }}>loading…</div>
      </HudPanel>
    );
  }

  if (budgets.length === 0) {
    return (
      <HudPanel title="Agent Budgets" accent="#7cc6ff">
        <div style={{ padding: 8, fontSize: 11, opacity: 0.7 }}>
          No spend recorded yet this week.
        </div>
      </HudPanel>
    );
  }

  return (
    <HudPanel
      title={`Agent Budgets · ${budgets.length}`}
      accent="#b980ff"
    >
      <div style={{ padding: 4 }}>
        {budgets.map((b) => (
          <BudgetRow key={b.user_id} b={b} onUpdated={refresh} />
        ))}
      </div>
      <div style={{ padding: '0 8px 6px', fontSize: 9, opacity: 0.55, fontFamily: 'var(--font-mono, monospace)' }}>
        Phase B3 — weekly Anthropic API spend per user. Cap edits require RAIN_SUPPORT_L2 server-side.
      </div>
    </HudPanel>
  );
}
