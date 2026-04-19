/* ============================================================
   ProjectsPage — pure workstream catalogue.
   One HudPanel per project showing path, components, description,
   progress, owner. NO ClickUp chrome here — the ClickUp kanban +
   sync lives on /clickup. Projects here are the user's local
   workstreams, similar to how Claude Code surfaces its known
   projects with paths + recent activity.
   ============================================================ */

import { useCallback, useMemo, useState } from 'react';
import {
  FolderKanban,
  FolderOpen,
  User,
  Calendar,
  Layers,
  ServerIcon,
  Laptop,
  Cloud,
  FileCode,
  Folder,
} from 'lucide-react';
import Modal from '../components/shared/Modal';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { useCommandCentre } from '../context/CommandCentreContext';
import type { Project, ProjectComponent } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './ProjectsPage.module.css';

/* Status → accent. Lower-cased normalisation so "To Do" and "to do"
   map to the same colour. */
const STATUS_COLOR: Record<string, string> = {
  'to do':        '#7cc6ff',
  'in progress':  '#00f0ff',
  'sit':          '#ffaa00',
  'qa':           '#ffaa00',
  'ppd':          '#ff7de0',
  'qa fail':      '#ff3355',
  'blocker':      '#ff3355',
  'sit pass':     '#6ff2a0',
  'ppd pass':     '#6ff2a0',
  'completed':    '#00ff88',
  // Legacy values that might still be on rows created before the 10-status
  // pipeline was introduced.
  'planning':     '#7cc6ff',
  'active':       '#6ff2a0',
  'paused':       '#ffaa00',
};
const colorFor = (s: string): string =>
  STATUS_COLOR[s.toLowerCase().trim()] ?? '#7cc6ff';

/* Component role → icon + accent. Local to this page because no other
   page renders components. */
const ROLE_STYLE: Record<string, { color: string; Icon: typeof ServerIcon }> = {
  core:     { color: '#00f0ff', Icon: Layers },
  backend:  { color: '#6ff2a0', Icon: ServerIcon },
  frontend: { color: '#ff7de0', Icon: Laptop },
  infra:    { color: '#ffaa00', Icon: Cloud },
};
const styleForRole = (role: string) =>
  ROLE_STYLE[role.toLowerCase()] ?? { color: '#ffffff', Icon: FileCode };

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString('en-ZA', {
      day: '2-digit', month: 'short', year: 'numeric',
    });
  } catch {
    return iso;
  }
}

/* Shortens a Windows path for the card footer while keeping the last
   two path segments readable ("…\Downloads\rainlex-deploy"). */
function shortPath(p: string, max: number = 52): string {
  if (!p) return '';
  if (p.length <= max) return p;
  const parts = p.split(/[\\/]/).filter(Boolean);
  if (parts.length <= 2) return p.slice(0, max - 1) + '…';
  return '…\\' + parts.slice(-2).join('\\');
}

function useClipboardPing() {
  const [pinged, setPinged] = useState<string | null>(null);
  const ping = useCallback((key: string, value: string) => {
    navigator.clipboard.writeText(value).catch(() => { /* ignore */ });
    setPinged(key);
    window.setTimeout(() => setPinged((cur) => (cur === key ? null : cur)), 1400);
  }, []);
  return { pinged, ping };
}

function ComponentChip({
  comp, projectId, pinged, onCopy,
}: {
  readonly comp: ProjectComponent;
  readonly projectId: string;
  readonly pinged: string | null;
  readonly onCopy: (key: string, path: string) => void;
}) {
  const { color, Icon } = styleForRole(comp.role);
  const key = `${projectId}:${comp.path}`;
  const isPinged = pinged === key;
  return (
    <button
      type="button"
      className={styles.componentChip}
      style={{
        color,
        borderColor: `${color}66`,
        background: `${color}18`,
      }}
      title={comp.path}
      onClick={(e) => {
        e.stopPropagation();
        onCopy(key, comp.path);
      }}
    >
      <Icon size={10} />
      <span className={styles.componentRole}>
        {isPinged ? 'copied!' : comp.role}
      </span>
    </button>
  );
}

export default function ProjectsPage() {
  const { state } = useCommandCentre();
  const [selected, setSelected] = useState<Project | null>(null);
  const { pinged, ping } = useClipboardPing();

  const openDetail = useCallback((p: Project) => setSelected(p), []);
  const closeDetail = useCallback(() => setSelected(null), []);

  const byStatus = useMemo(() => {
    const m = new Map<string, { label: string; value: number; color: string }>();
    for (const p of state.projects) {
      const label = p.status || 'To Do';
      const entry = m.get(label);
      if (entry) entry.value++;
      else m.set(label, { label, value: 1, color: colorFor(label) });
    }
    return Array.from(m.values());
  }, [state.projects]);

  const total = state.projects.length;
  const avgProgress = total === 0
    ? 0
    : state.projects.reduce((sum, p) => sum + p.progress, 0) / total / 100;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Projects · Portfolio"
        subtitle={`${total} workstream${total === 1 ? '' : 's'} · average progress ${Math.round(avgProgress * 100)}%`}
        gaugeValue={avgProgress}
        gaugeReadout={`${Math.round(avgProgress * 100)}%`}
        gaugeLabel="PROGRESS"
        gaugeColor="#6ff2a0"
        segments={byStatus}
        extra={
          <div className={styles.portfolioIcon}>
            <FolderKanban size={22} style={{ color: '#00f0ff' }} />
          </div>
        }
      />

      {total === 0 ? (
        <HudPanel
          title="Portfolio"
          accent="#7cc6ff"
          leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        >
          <div className={styles.empty}>
            <FolderOpen size={36} className={styles.emptyIcon} />
            <span>No projects — add one via POST /api/v1/projects</span>
          </div>
        </HudPanel>
      ) : (
        <div className={hudStyles.gridWide}>
          {state.projects.map((project) => {
            const accent = colorFor(project.status);
            const components = project.components ?? [];
            return (
              <HudPanel
                key={project.id}
                title={project.name}
                accent={accent}
                leading={<HudStatusLed color={accent} animate={
                  project.status.toLowerCase() === 'in progress'
                } />}
                meta={<>{project.progress}%</>}
                onClick={() => openDetail(project)}
                footer={
                  <div className={styles.footerRow}>
                    <span className={styles.footerMeta}>
                      <User size={10} /> {project.owner || 'unassigned'}
                    </span>
                    <span className={styles.footerMeta}>
                      <Calendar size={10} /> {formatDate(project.createdAt)}
                    </span>
                  </div>
                }
              >
                <div className={styles.body}>
                  {project.description && (
                    <p className={styles.desc}>{project.description}</p>
                  )}

                  <div className={styles.chipRow}>
                    <HudChip color={accent}>{project.status}</HudChip>
                    {project.priority && project.priority !== 'normal' && (
                      <HudChip color="#ffaa00">{project.priority}</HudChip>
                    )}
                  </div>

                  {components.length > 0 && (
                    <div className={styles.componentRow}>
                      {components.map((c) => (
                        <ComponentChip
                          key={c.path}
                          comp={c}
                          projectId={project.id}
                          pinged={pinged}
                          onCopy={ping}
                        />
                      ))}
                    </div>
                  )}

                  {project.localPath && (
                    <div className={styles.pathRow} title={project.localPath}>
                      <Folder size={10} className={styles.pathIcon} />
                      <code className={styles.pathCode}>
                        {shortPath(project.localPath)}
                      </code>
                    </div>
                  )}

                  <div className={styles.progressWrap}>
                    <div className={styles.progressMeta}>
                      <span className={styles.progressLabel}>Progress</span>
                      <span className={styles.progressPct} style={{ color: accent }}>
                        {project.progress}%
                      </span>
                    </div>
                    <div className={styles.progressTrack}>
                      <div
                        className={styles.progressFill}
                        style={{
                          width: `${Math.min(project.progress, 100)}%`,
                          background: accent,
                          boxShadow: `0 0 8px ${accent}`,
                        }}
                      />
                    </div>
                  </div>
                </div>
              </HudPanel>
            );
          })}
        </div>
      )}

      <Modal
        isOpen={selected !== null}
        onClose={closeDetail}
        title={selected?.name ?? 'Project Details'}
      >
        {selected && (
          <div>
            <div className={styles.detailGrid}>
              <span className={styles.detailLabel}>Status</span>
              <span className={styles.detailValue}>
                <HudChip color={colorFor(selected.status)}>{selected.status}</HudChip>
              </span>
              <span className={styles.detailLabel}>Priority</span>
              <span className={styles.detailValue}>{selected.priority || '—'}</span>
              <span className={styles.detailLabel}>Owner</span>
              <span className={styles.detailValue}>{selected.owner || '—'}</span>
              <span className={styles.detailLabel}>Progress</span>
              <span className={styles.detailValue}>{selected.progress}%</span>
              <span className={styles.detailLabel}>Created</span>
              <span className={styles.detailValue}>{formatDate(selected.createdAt)}</span>
            </div>

            <div className={styles.detailPaths}>
              <div className={styles.detailLabel} style={{ marginBottom: 6 }}>
                Components
              </div>
              {(selected.components ?? []).length === 0 ? (
                <div className={styles.detailMuted}>// no components registered</div>
              ) : (
                (selected.components ?? []).map((c) => (
                  <div key={c.path} className={styles.pathListRow}>
                    <HudChip color={styleForRole(c.role).color}>{c.role}</HudChip>
                    <code className={styles.pathText}>{c.path}</code>
                  </div>
                ))
              )}
            </div>

            {selected.description && (
              <p className={styles.descFull}>{selected.description}</p>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}
