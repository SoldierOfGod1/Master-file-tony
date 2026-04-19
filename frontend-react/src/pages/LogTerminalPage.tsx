/* ============================================================
   LogTerminalPage — single wide HUD panel wrapping a mono
   terminal-style log viewer.
   ============================================================ */

import { useState, useEffect, useRef, useMemo } from 'react';
import { Terminal } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudStatusLed } from '../components/shared/HudChip';
import { useCommandCentre } from '../context/CommandCentreContext';
import type { LogEntry } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './LogTerminalPage.module.css';

const FILTERS = ['ALL', 'ERR', 'WRN', 'INF', 'DBG'] as const;
type Filter = (typeof FILTERS)[number];
const MAX_LINES = 200;

const LEVEL_FILTER_MAP: Record<Filter, string | null> = {
  ALL: null,
  ERR: 'error',
  WRN: 'warn',
  INF: 'info',
  DBG: 'debug',
};

function levelClass(level: string): string {
  const l = level.toLowerCase();
  if (l === 'error' || l === 'err') return styles.levelError;
  if (l === 'warn' || l === 'wrn' || l === 'warning') return styles.levelWarn;
  if (l === 'info' || l === 'inf') return styles.levelInfo;
  if (l === 'debug' || l === 'dbg') return styles.levelDebug;
  return '';
}

function formatLevel(level: string): string {
  const l = level.toUpperCase();
  return l.length > 5 ? l.slice(0, 5) : l.padEnd(5, ' ');
}

function LogLine({ entry }: { readonly entry: LogEntry }) {
  return (
    <div className={styles.logLine}>
      <span className={styles.logTs}>{entry.ts}</span>
      <span className={`${styles.logLevel} ${levelClass(entry.level)}`}>
        [{formatLevel(entry.level)}]
      </span>
      <span className={styles.logAgent}>[{entry.agent}]</span>
      <span className={styles.logMsg}>{entry.msg}</span>
    </div>
  );
}

export default function LogTerminalPage() {
  const { state } = useCommandCentre();
  const [activeFilter, setActiveFilter] = useState<Filter>('ALL');
  const terminalRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo<LogEntry[]>(() => {
    let entries = state.logs;
    const matchLevel = LEVEL_FILTER_MAP[activeFilter];
    if (matchLevel) {
      entries = entries.filter((e) => e.level.toLowerCase() === matchLevel);
    }
    // Tail only the last MAX_LINES for scroll perf.
    return entries.length > MAX_LINES
      ? entries.slice(entries.length - MAX_LINES)
      : entries;
  }, [state.logs, activeFilter]);

  useEffect(() => {
    const el = terminalRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [filtered]);

  const counts = useMemo(() => {
    const m: Record<string, number> = { error: 0, warn: 0, info: 0, debug: 0 };
    for (const l of state.logs) {
      const k = l.level.toLowerCase();
      if (k === 'err' || k === 'error') m.error++;
      else if (k === 'wrn' || k === 'warn' || k === 'warning') m.warn++;
      else if (k === 'inf' || k === 'info') m.info++;
      else if (k === 'dbg' || k === 'debug') m.debug++;
    }
    return m;
  }, [state.logs]);

  const errorLED = counts.error > 0 ? '#ff3355' : '#6ff2a0';

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Log Terminal"
        subtitle={`${state.logs.length} lines · tail ${Math.min(MAX_LINES, state.logs.length)} · level filter: ${activeFilter}`}
        gaugeValue={state.logs.length === 0 ? 0 : Math.min(state.logs.length / 500, 1)}
        gaugeReadout={`${state.logs.length}`}
        gaugeLabel="LINES"
        gaugeColor={errorLED}
        segments={[
          { label: 'Errors',   value: counts.error, color: '#ff3355' },
          { label: 'Warnings', value: counts.warn,  color: '#ffaa00' },
          { label: 'Info',     value: counts.info,  color: '#7cc6ff' },
          { label: 'Debug',    value: counts.debug, color: '#6ff2a0' },
        ]}
        extra={
          <div className={styles.termIcon}>
            <Terminal size={22} style={{ color: '#00f0ff' }} />
          </div>
        }
      />

      <div className={styles.filterRow}>
        {FILTERS.map((f) => {
          const count = f === 'ALL'
            ? state.logs.length
            : counts[LEVEL_FILTER_MAP[f] ?? ''] ?? 0;
          return (
            <button
              key={f}
              type="button"
              className={`${styles.filterBtn} ${activeFilter === f ? styles.filterBtnActive : ''} ${styles[`filter${f}`] ?? ''}`}
              onClick={() => setActiveFilter(f)}
            >
              {f}
              <span className={styles.filterCount}>{count}</span>
            </button>
          );
        })}
      </div>

      <HudPanel
        title={`${activeFilter} · Live Tail`}
        accent={errorLED}
        leading={<HudStatusLed color={errorLED} />}
        meta={<>{filtered.length} / {state.logs.length}</>}
        footer={<>// WebSocket streaming · max {MAX_LINES} lines in view</>}
      >
        <div className={styles.terminal} ref={terminalRef}>
          {filtered.length === 0 ? (
            <div className={styles.empty}>// no log entries matching {activeFilter}</div>
          ) : (
            filtered.map((entry, idx) => (
              <LogLine key={`${entry.ts}-${entry.agent}-${idx}`} entry={entry} />
            ))
          )}
        </div>
      </HudPanel>
    </div>
  );
}
