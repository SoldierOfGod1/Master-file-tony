/* ============================================================
   QualityGatePanel — Dashboard module.
   Runs `go vet` + `tsc --noEmit` + secret scan via /api/v1/quality
   and renders a 3-LED summary. Click the button to re-run.
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Shield, CheckCircle2, XCircle, Clock, PlayCircle } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import { HudStatusLed } from '../components/shared/HudChip';
import { getLastQuality, runQuality, type QualityReport, type QualityGate } from '../api/quality';

type GateKey = 'go_vet' | 'typescript' | 'secrets';

const GATE_LABEL: Record<GateKey, string> = {
  go_vet: 'Backend Check',
  typescript: 'Frontend Check',
  secrets: 'Secret Scan',
};

const GATE_TOOLTIP: Record<GateKey, string> = {
  go_vet: 'go vet — static analysis of the Go backend',
  typescript: 'tsc --noEmit — TypeScript type-check of the frontend',
  secrets: 'regex scan for hardcoded API keys and passwords',
};

function ledColor(gate?: QualityGate): string {
  if (!gate) return '#7cc6ff';
  if (gate.skipped) return '#ffb86b';
  return gate.ok ? '#6ff2a0' : '#ff7b7b';
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export default function QualityGatePanel() {
  const [report, setReport] = useState<QualityReport | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // One cheap GET on mount so the panel shows the last cached run. No
  // polling — quality gates are expensive, the user triggers reruns.
  useEffect(() => {
    getLastQuality()
      .then((r) => setReport(r))
      .catch(() => setError('quality endpoint unreachable'));
  }, []);

  const onRun = useCallback(async () => {
    setRunning(true);
    setError(null);
    try {
      const r = await runQuality();
      if (r) setReport(r);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'run failed');
    } finally {
      setRunning(false);
    }
  }, []);

  const overall = useMemo<'pass' | 'fail' | 'unknown'>(() => {
    if (!report) return 'unknown';
    const gates: QualityGate[] = [report.go_vet, report.typescript, report.secrets];
    if (gates.some((g) => !g.skipped && !g.ok)) return 'fail';
    return 'pass';
  }, [report]);

  const overallColor = overall === 'pass' ? '#6ff2a0' : overall === 'fail' ? '#ff7b7b' : '#7cc6ff';
  const overallLabel = overall === 'pass' ? 'PASS' : overall === 'fail' ? 'FAIL' : '—';

  const ranAt = report?.ran_at ? new Date(report.ran_at).toLocaleTimeString() : 'never';
  const hitCount = report?.hits?.length ?? 0;

  return (
    <HudPanel
      icon={<Shield size={12} />}
      title="Quality Gates"
      subtitle={`go vet · tsc · secret scan · last: ${ranAt}`}
      leading={<HudStatusLed color={overallColor} animate={overall === 'fail'} />}
      meta={
        <button
          type="button"
          onClick={onRun}
          disabled={running}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 4,
            padding: '2px 10px',
            fontFamily: 'inherit',
            fontSize: 10,
            textTransform: 'uppercase',
            letterSpacing: '0.08em',
            color: '#0a0c12',
            background: running ? '#7cc6ff' : overallColor,
            border: 0,
            borderRadius: 4,
            cursor: running ? 'wait' : 'pointer',
          }}
        >
          {running ? <Clock size={10} /> : <PlayCircle size={10} />}
          {running ? 'running…' : 'run'}
        </button>
      }
    >
      {error && (
        <div style={{ color: '#ff7b7b', fontSize: 11, marginBottom: 6 }}>{error}</div>
      )}

      <div style={{ display: 'grid', gap: 6 }}>
        {(Object.keys(GATE_LABEL) as GateKey[]).map((key) => {
          const gate = report?.[key];
          const color = ledColor(gate);
          const Icon = gate?.ok ? CheckCircle2 : gate?.skipped ? Clock : XCircle;
          return (
            <div
              key={key}
              title={GATE_TOOLTIP[key]}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                padding: '4px 6px',
                borderLeft: `2px solid ${color}55`,
                fontSize: 11,
              }}
            >
              <HudStatusLed color={color} />
              <Icon size={12} color={color} />
              <span style={{ flex: 1, fontFamily: 'var(--font-mono, monospace)' }}>
                {GATE_LABEL[key]}
                {gate?.skipped ? ' (skipped)' : ''}
              </span>
              <span style={{ color: 'var(--ink-dim, #7cc6ff)', fontSize: 10 }}>
                {gate ? formatDuration(gate.duration_ms) : '—'}
              </span>
            </div>
          );
        })}
      </div>

      {hitCount > 0 && (
        <div style={{ marginTop: 8, fontSize: 10, color: '#ff7b7b' }}>
          {hitCount} secret hit{hitCount === 1 ? '' : 's'} — see /quality endpoint for file list
        </div>
      )}

      <div style={{ marginTop: 6, fontSize: 10, opacity: 0.7 }}>
        Status: <strong style={{ color: overallColor }}>{overallLabel}</strong>
      </div>
    </HudPanel>
  );
}
