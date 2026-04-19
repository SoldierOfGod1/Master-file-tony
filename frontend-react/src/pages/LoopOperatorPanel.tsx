/* ============================================================
   LoopOperatorPanel — live queue worker list with Kill/Pause.
   Poll /api/v1/loops every 2s; empty state renders a short hint.
   ============================================================ */

import { useCallback, useEffect, useState } from 'react';
import { Activity, Square, Pause, Play } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import { HudStatusLed, HudChip } from '../components/shared/HudChip';
import { listLoops, pauseLoop, killLoop, type LoopState } from '../api/loops';

function shortPath(p: string): string {
  if (!p) return '—';
  if (p.length <= 40) return p;
  return '…' + p.slice(-38);
}

function elapsed(startedAt?: string): string {
  if (!startedAt) return '—';
  const ms = Date.now() - new Date(startedAt).getTime();
  if (ms < 1000) return '0s';
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.floor((ms % 60_000) / 1000);
  return `${m}m${s}s`;
}

export default function LoopOperatorPanel() {
  const [loops, setLoops] = useState<LoopState[]>([]);
  const [busy, setBusy] = useState<Record<string, boolean>>({});

  const refresh = useCallback(async () => {
    const rows = await listLoops();
    setLoops(rows);
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 2_000);
    return () => clearInterval(t);
  }, [refresh]);

  const onPause = useCallback(async (dir: string, paused: boolean) => {
    setBusy((b) => ({ ...b, [dir]: true }));
    try {
      await pauseLoop(dir, paused);
      await refresh();
    } finally {
      setBusy((b) => ({ ...b, [dir]: false }));
    }
  }, [refresh]);

  const onKill = useCallback(async (dir: string) => {
    setBusy((b) => ({ ...b, [dir]: true }));
    try {
      await killLoop(dir);
      await refresh();
    } finally {
      setBusy((b) => ({ ...b, [dir]: false }));
    }
  }, [refresh]);

  const runningCount = loops.filter((l) => l.running).length;

  return (
    <HudPanel
      icon={<Activity size={12} />}
      title="Loop Operator"
      subtitle="Live chat queue workers — pause or kill"
      leading={<HudStatusLed color={runningCount > 0 ? '#6ff2a0' : '#7cc6ff'} animate={runningCount > 0} />}
      meta={<>{runningCount}/{loops.length} running</>}
    >
      {loops.length === 0 && (
        <div style={{ fontSize: 11, opacity: 0.7, padding: '6px 0' }}>
          No queue workers yet — fire a /chat request to spin one up.
        </div>
      )}

      <div style={{ display: 'grid', gap: 6 }}>
        {loops.map((l) => {
          const color = l.running ? '#6ff2a0' : l.paused ? '#ffb86b' : '#7cc6ff';
          const isBusy = !!busy[l.project_dir];
          return (
            <div
              key={l.project_dir}
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr auto',
                gap: 6,
                alignItems: 'center',
                padding: '6px 8px',
                borderLeft: `2px solid ${color}55`,
                fontSize: 11,
                fontFamily: 'var(--font-mono, monospace)',
              }}
            >
              <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                  <HudStatusLed color={color} animate={l.running} />
                  <span style={{ color: 'var(--ink-dim, #7cc6ff)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {shortPath(l.project_dir)}
                  </span>
                </div>
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', fontSize: 10, opacity: 0.8 }}>
                  <HudChip color={color}>{l.running ? 'RUN' : l.paused ? 'PAUSED' : 'IDLE'}</HudChip>
                  {l.running && <span>t+{elapsed(l.started_at)}</span>}
                  {l.pending > 0 && <span>queued: {l.pending}</span>}
                  {l.conversation_id && <span>conv: {l.conversation_id.slice(0, 8)}</span>}
                </div>
              </div>
              <div style={{ display: 'flex', gap: 4 }}>
                <button
                  type="button"
                  onClick={() => onPause(l.project_dir, !l.paused)}
                  disabled={isBusy}
                  title={l.paused ? 'Resume' : 'Pause'}
                  style={iconBtn(l.paused ? '#6ff2a0' : '#ffb86b')}
                >
                  {l.paused ? <Play size={12} /> : <Pause size={12} />}
                </button>
                <button
                  type="button"
                  onClick={() => onKill(l.project_dir)}
                  disabled={isBusy || !l.running}
                  title="Kill running CLI"
                  style={iconBtn('#ff7b7b')}
                >
                  <Square size={12} />
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </HudPanel>
  );
}

function iconBtn(color: string): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 22,
    height: 22,
    padding: 0,
    color,
    background: 'transparent',
    border: `1px solid ${color}66`,
    borderRadius: 4,
    cursor: 'pointer',
  };
}
