/* ============================================================
   ProjectRunnerDrawer — live log tail for one running project.
   Opens when the user clicks the Run indicator on a project row.
   Polls /runner/logs every second per component (backend +
   frontend) and mirrors the output into a terminal-style tail.
   ============================================================ */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { X, Square, Play, RefreshCw, ExternalLink } from 'lucide-react';
import {
  getRunnerLogs,
  getRunnerStatus,
  startProject,
  stopProject,
  RUNNER_STATE_COLOR,
  type RunnerGroup,
  type RunnerLogLine,
  type RunnerProcess,
} from '../api/runner';

interface Props {
  readonly projectId: string;
  readonly projectName: string;
  readonly hasLocalPath: boolean;
  readonly onClose: () => void;
}

export default function ProjectRunnerDrawer({
  projectId,
  projectName,
  hasLocalPath,
  onClose,
}: Props) {
  const [group, setGroup] = useState<RunnerGroup | null>(null);
  const [logs, setLogs] = useState<Record<number, RunnerLogLine[]>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    const g = await getRunnerStatus(projectId);
    setGroup(g);
    if (!g) return;
    const next: Record<number, RunnerLogLine[]> = {};
    await Promise.all(
      g.processes.map(async (_, idx) => {
        next[idx] = await getRunnerLogs(projectId, idx, 300);
      }),
    );
    setLogs(next);
  }, [projectId]);

  useEffect(() => {
    void refresh();
    const t = window.setInterval(() => {
      void refresh();
    }, 1500);
    return () => window.clearInterval(t);
  }, [refresh]);

  const onStart = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const g = await startProject(projectId);
      if (g) setGroup(g);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'start failed');
    } finally {
      setBusy(false);
    }
  }, [projectId, refresh]);

  const onStop = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      await stopProject(projectId);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'stop failed');
    } finally {
      setBusy(false);
    }
  }, [projectId, refresh]);

  const anyRunning = useMemo(
    () => (group?.processes ?? []).some((p) => p.state === 'running' || p.state === 'starting'),
    [group],
  );

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(5, 8, 16, 0.6)',
        zIndex: 1000,
        display: 'flex',
        justifyContent: 'flex-end',
      }}
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(680px, 100vw)',
          height: '100vh',
          background: '#0a0c12',
          borderLeft: '1px solid #00f0ff44',
          display: 'flex',
          flexDirection: 'column',
          color: '#c7e9ff',
          fontFamily: 'var(--font-mono, monospace)',
          boxShadow: '-20px 0 40px rgba(0, 240, 255, 0.15)',
        }}
      >
        <header style={headerStyle}>
          <div>
            <div style={{ fontSize: 10, opacity: 0.6, textTransform: 'uppercase', letterSpacing: '0.1em' }}>
              Runner
            </div>
            <div style={{ fontSize: 14, color: '#00f0ff' }}>{projectName}</div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            {!anyRunning ? (
              <button
                type="button"
                onClick={() => void onStart()}
                disabled={busy || !hasLocalPath}
                style={btnStyle('#6ff2a0', busy || !hasLocalPath)}
                title={hasLocalPath ? 'Start frontend + backend' : 'Set a local path on this project first'}
              >
                <Play size={12} /> Start
              </button>
            ) : (
              <button
                type="button"
                onClick={() => void onStop()}
                disabled={busy}
                style={btnStyle('#ff7b7b', busy)}
              >
                <Square size={12} /> Stop
              </button>
            )}
            <button
              type="button"
              onClick={() => void refresh()}
              disabled={busy}
              style={btnStyle('#7cc6ff', busy)}
              title="Refresh"
            >
              <RefreshCw size={12} />
            </button>
            <button type="button" onClick={onClose} style={btnStyle('#7cc6ff', false)}>
              <X size={12} />
            </button>
          </div>
        </header>

        {!hasLocalPath && (
          <div style={warnStyle}>
            This project has no <code>local_path</code>. Edit the project and set a path first.
          </div>
        )}
        {error && <div style={errorStyle}>{error}</div>}

        {(group?.processes ?? []).length === 0 ? (
          <div style={{ padding: 20, fontSize: 11, opacity: 0.7 }}>
            // no processes started. Click <b>Start</b> to spawn the dev servers detected
            in the project folder.
          </div>
        ) : (
          <div style={{ flex: 1, overflow: 'auto', padding: 12, display: 'flex', flexDirection: 'column', gap: 12 }}>
            {(group?.processes ?? []).map((p, idx) => (
              <ProcessSection
                key={idx}
                p={p}
                lines={logs[idx] ?? []}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function ProcessSection({
  p, lines,
}: {
  readonly p: RunnerProcess;
  readonly lines: RunnerLogLine[];
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines, autoScroll]);

  const accent = RUNNER_STATE_COLOR[p.state];
  return (
    <div style={{ border: `1px solid ${accent}33`, borderRadius: 4 }}>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '6px 10px',
        borderBottom: `1px solid ${accent}33`,
        background: `${accent}11`,
      }}>
        <span style={{
          display: 'inline-block',
          width: 6, height: 6, borderRadius: '50%',
          background: accent,
          boxShadow: `0 0 6px ${accent}`,
        }} />
        <span style={{ fontSize: 11, color: accent, textTransform: 'uppercase', letterSpacing: '0.08em' }}>
          {p.component.role}
        </span>
        <span style={{ fontSize: 10, opacity: 0.7 }}>{p.component.label}</span>
        <span style={{ marginLeft: 'auto', fontSize: 10, opacity: 0.6 }}>
          {p.state}{p.pid ? ` · pid ${p.pid}` : ''}{p.component.port ? ` · :${p.component.port}` : ''}
        </span>
        {p.state === 'running' && p.component.health_url && (
          <a
            href={p.component.health_url}
            target="_blank"
            rel="noreferrer"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 3,
              padding: '2px 6px',
              fontSize: 9,
              color: '#6ff2a0',
              border: '1px solid #6ff2a055',
              borderRadius: 3,
              textDecoration: 'none',
            }}
          >
            <ExternalLink size={10} /> open
          </a>
        )}
      </div>
      {p.error && (
        <div style={{ padding: '4px 10px', fontSize: 10, color: '#ff7b7b', background: '#ff3355' + '11' }}>
          {p.error}
        </div>
      )}
      <div
        ref={scrollRef}
        onScroll={(e) => {
          const el = e.currentTarget;
          const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 20;
          setAutoScroll(atBottom);
        }}
        style={{
          maxHeight: 260,
          overflow: 'auto',
          padding: '6px 10px',
          fontSize: 10.5,
          lineHeight: 1.45,
          background: '#05070d',
        }}
      >
        {lines.length === 0 ? (
          <div style={{ opacity: 0.4 }}>// waiting for output…</div>
        ) : (
          lines.map((l, i) => (
            <div
              key={i}
              style={{
                color: l.stream === 'stderr' ? '#ff9b9b' : '#c7e9ff',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {l.line}
            </div>
          ))
        )}
      </div>
    </div>
  );
}

const headerStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '12px 16px',
  borderBottom: '1px solid #00f0ff22',
};

const warnStyle: React.CSSProperties = {
  padding: '8px 16px',
  fontSize: 11,
  color: '#ffaa00',
  background: '#ffaa0011',
  borderBottom: '1px solid #ffaa0033',
};

const errorStyle: React.CSSProperties = {
  padding: '8px 16px',
  fontSize: 11,
  color: '#ff7b7b',
  background: '#ff335511',
  borderBottom: '1px solid #ff335533',
};

function btnStyle(color: string, disabled: boolean): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 5,
    padding: '4px 10px',
    fontSize: 10,
    fontFamily: 'inherit',
    textTransform: 'uppercase',
    letterSpacing: '0.08em',
    color,
    background: 'transparent',
    border: `1px solid ${color}55`,
    borderRadius: 3,
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.5 : 1,
  };
}
