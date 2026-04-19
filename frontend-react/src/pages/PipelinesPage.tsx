/* ============================================================
   PipelinesPage — HUD panels for each CI/CD pipeline with a
   stage track (connected dots) in the body.
   ============================================================ */

import { useState, useMemo } from 'react';
import {
  Workflow,
  GitBranch,
  Timer,
  CircleOff,
  Zap,
} from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { useCommandCentre } from '../context/CommandCentreContext';
import type { PipelineStage } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './PipelinesPage.module.css';

const FILTERS = ['All', 'Running', 'Passed', 'Failed'] as const;
type Filter = (typeof FILTERS)[number];

/* status → colour. Drives the LED + accent + stage dots. */
const STATUS_COLOR: Record<string, string> = {
  running: '#00f0ff',
  passed:  '#6ff2a0',
  failed:  '#ff3355',
  idle:    '#7cc6ff',
};
const statusColor = (s: string): string => STATUS_COLOR[s.toLowerCase()] ?? STATUS_COLOR.idle;

const TYPE_COLOR: Record<string, string> = {
  build:  '#7cc6ff',
  deploy: '#ff7de0',
  test:   '#ffaa00',
};
const typeColor = (t: string): string => TYPE_COLOR[t.toLowerCase()] ?? '#7cc6ff';

function formatDuration(ms: number): string {
  if (ms <= 0) return '--';
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainSec = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remainSec}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

const DEFAULT_STAGES: readonly string[] = ['pre-flight', 'lint', 'build', 'test', 'deploy'];
const normaliseStages = (stages: PipelineStage[]): PipelineStage[] =>
  stages.length > 0 ? stages : DEFAULT_STAGES.map((name) => ({ name, status: 'pending' }));

function StageTrack({ stages }: { readonly stages: readonly PipelineStage[] }) {
  return (
    <div className={styles.track}>
      {stages.map((stage, idx) => {
        const isLast = idx === stages.length - 1;
        const color = statusColor(stage.status);
        const isRunning = stage.status.toLowerCase() === 'running';
        return (
          <div key={stage.name} className={styles.stageWrap}>
            <div
              className={`${styles.stageDot} ${isRunning ? styles.stageDotRunning : ''}`}
              style={{ background: color, boxShadow: `0 0 8px ${color}` }}
            />
            <span className={styles.stageName}>{stage.name}</span>
            {!isLast && (
              <div
                className={styles.connector}
                style={{
                  background: stage.status.toLowerCase() === 'passed'
                    ? `linear-gradient(90deg, ${color}, ${statusColor(stages[idx + 1].status)})`
                    : 'rgba(0, 240, 255, 0.12)',
                }}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}

export default function PipelinesPage() {
  const { state } = useCommandCentre();
  const [activeFilter, setActiveFilter] = useState<Filter>('All');

  const filtered = useMemo(() => {
    if (activeFilter === 'All') return state.pipelines;
    return state.pipelines.filter(
      (p) => p.status.toLowerCase() === activeFilter.toLowerCase(),
    );
  }, [state.pipelines, activeFilter]);

  const counts = useMemo(() => {
    const m: Record<string, number> = { running: 0, passed: 0, failed: 0 };
    for (const p of state.pipelines) {
      const k = p.status.toLowerCase();
      m[k] = (m[k] ?? 0) + 1;
    }
    return m;
  }, [state.pipelines]);

  const passRatio = state.pipelines.length === 0
    ? 0
    : (counts.passed ?? 0) / state.pipelines.length;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="CI/CD Pipelines"
        subtitle={`${state.pipelines.length} pipelines · ${counts.running ?? 0} running`}
        gaugeValue={passRatio}
        gaugeReadout={`${counts.passed ?? 0}/${state.pipelines.length}`}
        gaugeLabel="PASS"
        gaugeColor="#6ff2a0"
        segments={[
          { label: 'Running', value: counts.running ?? 0, color: '#00f0ff' },
          { label: 'Passed',  value: counts.passed ?? 0,  color: '#6ff2a0' },
          { label: 'Failed',  value: counts.failed ?? 0,  color: '#ff3355' },
        ]}
        extra={
          <div className={styles.hubIcon}>
            <Workflow size={22} style={{ color: '#00f0ff' }} />
          </div>
        }
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
              <span className={styles.filterCount}>{counts[f.toLowerCase()] ?? 0}</span>
            )}
          </button>
        ))}
      </div>

      {filtered.length === 0 ? (
        <HudPanel title="Pipeline Queue" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" animate={false} />}>
          <div className={styles.empty}>
            <CircleOff size={36} className={styles.emptyIcon} />
            <span>No {activeFilter.toLowerCase()} pipelines</span>
          </div>
        </HudPanel>
      ) : (
        <div className={hudStyles.gridWide}>
          {filtered.map((pipeline) => {
            const color = statusColor(pipeline.status);
            const tColor = typeColor(pipeline.type);
            const stages = normaliseStages(pipeline.stages);
            const isRunning = pipeline.status.toLowerCase() === 'running';
            return (
              <HudPanel
                key={pipeline.id}
                title={pipeline.name}
                accent={color}
                leading={<HudStatusLed color={color} animate={isRunning} />}
                meta={<HudChip color={tColor}>{pipeline.type}</HudChip>}
                footer={
                  <div className={styles.footerRow}>
                    <span className={styles.footerMeta}>
                      <GitBranch size={10} /> {pipeline.branch}
                    </span>
                    <span className={styles.footerMeta}>
                      <Zap size={10} /> {pipeline.trigger}
                    </span>
                    <span className={styles.footerMeta}>
                      <Timer size={10} /> {formatDuration(pipeline.durationMs)}
                    </span>
                  </div>
                }
              >
                <div className={styles.body}>
                  <div className={styles.metaRow}>
                    <span className={styles.projectRef}>{pipeline.projectId}</span>
                    <HudChip color={color}>{pipeline.status}</HudChip>
                  </div>
                  <StageTrack stages={stages} />
                </div>
              </HudPanel>
            );
          })}
        </div>
      )}
    </div>
  );
}
