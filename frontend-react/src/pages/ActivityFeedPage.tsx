/* ============================================================
   ActivityFeedPage — HUD panel wrapping a live event timeline.
   ============================================================ */

import { useState, useEffect, useRef, useMemo } from 'react';
import { Activity, Zap, AlertTriangle, Shield, Cog } from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import type { FeedEvent } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './ActivityFeedPage.module.css';

const FILTERS = ['All', 'Spawns', 'Errors', 'Gates', 'System'] as const;
type Filter = (typeof FILTERS)[number];

const FILTER_TYPE: Record<Filter, string | null> = {
  All:    null,
  Spawns: 'spawn',
  Errors: 'error',
  Gates:  'gate',
  System: 'system',
};

/* Map event.type → colour + icon so the timeline reads visually. */
interface TypeStyle {
  readonly color: string;
  readonly icon: typeof Zap;
  readonly label: string;
}
const TYPES: Record<string, TypeStyle> = {
  spawn:  { color: '#6ff2a0', icon: Zap,            label: 'Spawn' },
  error:  { color: '#ff3355', icon: AlertTriangle,  label: 'Error' },
  gate:   { color: '#ffaa00', icon: Shield,         label: 'Gate' },
  system: { color: '#7cc6ff', icon: Cog,            label: 'System' },
};
const styleFor = (t: string): TypeStyle =>
  TYPES[t.toLowerCase()] ?? { color: '#7cc6ff', icon: Activity, label: t };

function FeedRow({ event }: { readonly event: FeedEvent }) {
  const ts = styleFor(event.type);
  const Icon = ts.icon;
  return (
    <div className={styles.row}>
      <span className={styles.time}>{event.time}</span>
      <span className={styles.dot} style={{ background: ts.color, boxShadow: `0 0 6px ${ts.color}` }} />
      <Icon size={11} style={{ color: ts.color, flexShrink: 0 }} />
      <HudChip color={ts.color} className={styles.agentChip}>{event.agent}</HudChip>
      <span className={styles.message}>{event.message}</span>
    </div>
  );
}

export default function ActivityFeedPage() {
  const { state } = useCommandCentre();
  const [activeFilter, setActiveFilter] = useState<Filter>('All');
  const listRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo<FeedEvent[]>(() => {
    const matchType = FILTER_TYPE[activeFilter];
    if (!matchType) return state.feed;
    return state.feed.filter((e) => e.type.toLowerCase() === matchType);
  }, [state.feed, activeFilter]);

  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [filtered]);

  // Per-type counts for the summary bar.
  const counts = useMemo(() => {
    const m: Record<string, number> = { spawn: 0, error: 0, gate: 0, system: 0 };
    for (const e of state.feed) {
      const k = e.type.toLowerCase();
      m[k] = (m[k] ?? 0) + 1;
    }
    return m;
  }, [state.feed]);

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Activity Feed · Live"
        subtitle={`${state.feed.length} events · streaming via WebSocket`}
        gaugeValue={state.feed.length === 0 ? 0 : Math.min(state.feed.length / 100, 1)}
        gaugeReadout={`${state.feed.length}`}
        gaugeLabel="EVENTS"
        gaugeColor="#00f0ff"
        segments={Object.entries(TYPES).map(([k, v]) => ({
          label: v.label,
          value: counts[k] ?? 0,
          color: v.color,
        }))}
      />

      <div className={styles.filterRow}>
        {FILTERS.map((f) => (
          <button
            key={f}
            type="button"
            className={`${styles.filterBtn} ${activeFilter === f ? styles.filterBtnActive : ''}`}
            onClick={() => setActiveFilter(f)}
          >
            {f}
            {f !== 'All' && (
              <span className={styles.filterCount}>
                {counts[FILTER_TYPE[f] ?? ''] ?? 0}
              </span>
            )}
          </button>
        ))}
      </div>

      <HudPanel
        title={activeFilter === 'All' ? 'All Events' : activeFilter}
        leading={<HudStatusLed color="#6ff2a0" />}
        meta={<>{filtered.length}</>}
        accent="#00f0ff"
      >
        <div className={styles.feedList} ref={listRef}>
          {filtered.length === 0 ? (
            <div className={styles.empty}>// no events match this filter</div>
          ) : (
            filtered.map((event, idx) => (
              <FeedRow key={`${event.time}-${event.agent}-${idx}`} event={event} />
            ))
          )}
        </div>
      </HudPanel>
    </div>
  );
}
