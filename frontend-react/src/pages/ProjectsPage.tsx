/* ============================================================
   ProjectsPage — pure workstream catalogue.
   One HudPanel per project showing path, components, description,
   progress, owner. NO ClickUp chrome here — the ClickUp kanban +
   sync lives on /clickup. Projects here are the user's local
   workstreams, similar to how Claude Code surfaces its known
   projects with paths + recent activity.
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
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
  Monitor,
  FlaskConical,
  Rocket,
  ExternalLink,
  MoreHorizontal,
  Plus,
  Edit3,
  Trash2,
  Play,
  Square,
  Terminal,
} from 'lucide-react';
import Modal from '../components/shared/Modal';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { useCommandCentre } from '../context/CommandCentreContext';
import type { Project, ProjectComponent } from '../types/api';
import { deleteProject } from '../api/projects';
import {
  groupState,
  listRunners,
  RUNNER_STATE_COLOR,
  startProject,
  stopProject,
  type RunnerGroup,
  type RunnerState,
} from '../api/runner';
import ProjectEditModal from './ProjectEditModal';
import ProjectRunnerDrawer from './ProjectRunnerDrawer';
import hudStyles from '../theme/hud.module.css';
import styles from './ProjectsPage.module.css';

type ProjectView = 'current' | 'sit' | 'prod';
const VIEW_STORAGE_KEY = 'projectsView';

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
  const { state, refreshAll } = useCommandCentre();
  const [selected, setSelected] = useState<Project | null>(null);
  const [editing, setEditing] = useState<Project | null>(null);
  const [creatingNew, setCreatingNew] = useState(false);
  const [menuOpenFor, setMenuOpenFor] = useState<string | null>(null);
  const [runners, setRunners] = useState<Record<string, RunnerGroup>>({});
  const [logsFor, setLogsFor] = useState<Project | null>(null);
  const [runnerBusy, setRunnerBusy] = useState<string | null>(null);
  const { pinged, ping } = useClipboardPing();

  // Poll the runner registry so LEDs on every project row stay fresh.
  // 2s is snappy enough to reflect start/stop clicks without hammering
  // the backend when no project is running.
  useEffect(() => {
    let cancelled = false;
    const tick = async () => {
      const all = await listRunners();
      if (cancelled) return;
      const map: Record<string, RunnerGroup> = {};
      for (const g of all) map[g.project_id] = g;
      setRunners(map);
    };
    void tick();
    const t = window.setInterval(() => { void tick(); }, 2000);
    return () => { cancelled = true; window.clearInterval(t); };
  }, []);

  const runnerStateFor = useCallback(
    (projectId: string): RunnerState => groupState(runners[projectId]),
    [runners],
  );

  const toggleRun = useCallback(async (p: Project) => {
    if (runnerBusy === p.id) return;
    setRunnerBusy(p.id);
    try {
      const s = runnerStateFor(p.id);
      if (s === 'running' || s === 'starting') {
        await stopProject(p.id);
      } else {
        await startProject(p.id);
      }
      const all = await listRunners();
      const map: Record<string, RunnerGroup> = {};
      for (const g of all) map[g.project_id] = g;
      setRunners(map);
    } finally {
      setRunnerBusy(null);
    }
  }, [runnerBusy, runnerStateFor]);

  // 3-way view toggle — persisted across reloads via localStorage.
  const [view, setView] = useState<ProjectView>(
    () => (localStorage.getItem(VIEW_STORAGE_KEY) as ProjectView) || 'current',
  );
  const changeView = useCallback((v: ProjectView) => {
    setView(v);
    localStorage.setItem(VIEW_STORAGE_KEY, v);
  }, []);

  const openDetail = useCallback((p: Project) => setSelected(p), []);
  const closeDetail = useCallback(() => setSelected(null), []);

  // Close the ⋯ menu when the user clicks anywhere outside it.
  useEffect(() => {
    if (!menuOpenFor) return;
    const close = () => setMenuOpenFor(null);
    window.addEventListener('click', close);
    return () => window.removeEventListener('click', close);
  }, [menuOpenFor]);

  const handleSaved = useCallback((p: Project) => {
    setEditing(null);
    setCreatingNew(false);
    void refreshAll();
    void p;
  }, [refreshAll]);

  const handleDelete = useCallback(async (p: Project) => {
    const ok = window.confirm(
      `Delete "${p.name}"?\n\nThis removes the project locally AND its ClickUp task.\nThis cannot be undone.`,
    );
    if (!ok) return;
    const success = await deleteProject(p.id);
    if (success) {
      void refreshAll();
      setSelected((cur) => (cur?.id === p.id ? null : cur));
    } else {
      window.alert('Delete failed — check backend logs.');
    }
  }, [refreshAll]);

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
  const sitCount = state.projects.filter((p) => p.sitUrl).length;
  const prodCount = state.projects.filter((p) => p.prodUrl).length;

  const subtitle = view === 'current'
    ? `${total} workstream${total === 1 ? '' : 's'} · avg progress ${(avgProgress * 100).toFixed(2)}%`
    : view === 'sit'
      ? `${sitCount}/${total} deployed to SIT · click a card to open its environment`
      : `${prodCount}/${total} in production · click a card to open its environment`;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Projects · Portfolio"
        subtitle={subtitle}
        gaugeValue={avgProgress}
        gaugeReadout={`${(avgProgress * 100).toFixed(2)}%`}
        gaugeLabel="PROGRESS"
        gaugeColor="#6ff2a0"
        segments={byStatus}
        extra={
          <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
            <ViewToggle
              view={view}
              onChange={changeView}
              sitCount={sitCount}
              prodCount={prodCount}
            />
            <button
              type="button"
              onClick={() => setCreatingNew(true)}
              style={newBtnStyle}
              title="Create a new project"
            >
              <Plus size={13} /> New project
            </button>
            <div className={styles.portfolioIcon}>
              <FolderKanban size={22} style={{ color: '#00f0ff' }} />
            </div>
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
                meta={
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <RunControl
                      state={runnerStateFor(project.id)}
                      busy={runnerBusy === project.id}
                      disabled={!project.localPath}
                      onRun={(e) => { e.stopPropagation(); void toggleRun(project); }}
                      onOpenLogs={(e) => { e.stopPropagation(); setLogsFor(project); }}
                    />
                    <CardActionMenu
                      project={project}
                      open={menuOpenFor === project.id}
                      onToggle={(e) => {
                        e.stopPropagation();
                        setMenuOpenFor((cur) => cur === project.id ? null : project.id);
                      }}
                      onEdit={(p) => { setMenuOpenFor(null); setEditing(p); }}
                      onDelete={(p) => { setMenuOpenFor(null); void handleDelete(p); }}
                      progress={project.progress}
                    />
                  </div>
                }
                onClick={() => openDetail(project)}
                footer={
                  <div className={styles.footerRow}>
                    <span className={styles.footerMeta}>
                      <User size={10} /> {project.owner || 'unassigned'}
                    </span>
                    <span className={styles.footerMeta}>
                      <Calendar size={10} /> {formatDate(project.createdAt)}
                    </span>
                    <DeployChips project={project} />
                  </div>
                }
              >
                <div className={styles.body}>
                  {project.description && (
                    <p className={styles.desc}>{project.description}</p>
                  )}

                  {view !== 'current' && (
                    <EnvironmentSlab
                      view={view}
                      project={project}
                      onEdit={() => setEditing(project)}
                    />
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

      {(editing || creatingNew) && (
        <ProjectEditModal
          project={editing ?? undefined}
          onClose={() => { setEditing(null); setCreatingNew(false); }}
          onSaved={handleSaved}
        />
      )}

      {logsFor && (
        <ProjectRunnerDrawer
          projectId={logsFor.id}
          projectName={logsFor.name}
          hasLocalPath={!!logsFor.localPath}
          onClose={() => setLogsFor(null)}
        />
      )}
    </div>
  );
}

/* ---- Per-row run button + logs button + live LED ---- */
function RunControl({
  state, busy, disabled, onRun, onOpenLogs,
}: {
  readonly state: RunnerState;
  readonly busy: boolean;
  readonly disabled: boolean;
  readonly onRun: (e: React.MouseEvent) => void;
  readonly onOpenLogs: (e: React.MouseEvent) => void;
}) {
  const live = state === 'running' || state === 'starting';
  const color = RUNNER_STATE_COLOR[state];
  const pulse = state === 'starting' || state === 'stopping';
  return (
    <div style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }} onClick={(e) => e.stopPropagation()}>
      <button
        type="button"
        onClick={onRun}
        disabled={busy || disabled}
        title={
          disabled
            ? 'Set a local_path on this project to enable Run'
            : live ? 'Stop dev servers' : 'Start frontend + backend'
        }
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
          padding: '3px 7px',
          fontSize: 9,
          fontFamily: 'inherit',
          textTransform: 'uppercase',
          letterSpacing: '0.08em',
          color,
          background: `${color}11`,
          border: `1px solid ${color}55`,
          borderRadius: 3,
          cursor: busy || disabled ? 'not-allowed' : 'pointer',
          opacity: disabled ? 0.45 : 1,
          animation: pulse ? 'pulse 1.2s ease-in-out infinite' : undefined,
        }}
      >
        {live ? <Square size={9} /> : <Play size={9} />}
        {live ? 'stop' : 'run'}
      </button>
      <button
        type="button"
        onClick={onOpenLogs}
        title="View live logs"
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          padding: '3px 5px',
          fontSize: 9,
          color: '#7cc6ff',
          background: 'transparent',
          border: '1px solid #7cc6ff55',
          borderRadius: 3,
          cursor: 'pointer',
        }}
      >
        <Terminal size={9} />
      </button>
    </div>
  );
}

/* ---- View toggle (Current · SIT · Production) ---- */
function ViewToggle({ view, onChange, sitCount, prodCount }: {
  readonly view: ProjectView;
  readonly onChange: (v: ProjectView) => void;
  readonly sitCount: number;
  readonly prodCount: number;
}) {
  const pill = (k: ProjectView, icon: React.ReactNode, label: string, count: number, color: string) => {
    const active = view === k;
    return (
      <button
        key={k}
        type="button"
        onClick={() => onChange(k)}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 5,
          padding: '4px 10px',
          fontSize: 10,
          fontFamily: 'inherit',
          textTransform: 'uppercase',
          letterSpacing: '0.08em',
          color: active ? '#0a0c12' : color,
          background: active ? color : 'transparent',
          border: `1px solid ${color}${active ? '' : '55'}`,
          borderRadius: 4,
          cursor: 'pointer',
          opacity: count === 0 && !active ? 0.6 : 1,
        }}
      >
        {icon}
        {label}
        {k !== 'current' && (
          <span style={{
            fontSize: 9,
            padding: '1px 5px',
            borderRadius: 3,
            background: active ? 'rgba(10,12,18,0.3)' : `${color}22`,
            marginLeft: 3,
          }}>
            {count}
          </span>
        )}
        {k !== 'current' && count > 0 && !active && (
          <span style={{
            width: 6, height: 6, borderRadius: 3, marginLeft: 1,
            background: color, boxShadow: `0 0 6px ${color}`,
            animation: 'pulse 1.8s infinite',
          }} />
        )}
      </button>
    );
  };
  return (
    <div style={{ display: 'inline-flex', gap: 4 }}>
      {pill('current', <Monitor size={11} />, 'Current', 0, '#00f0ff')}
      {pill('sit', <FlaskConical size={11} />, 'SIT', sitCount, '#ffaa00')}
      {pill('prod', <Rocket size={11} />, 'Production', prodCount, '#6ff2a0')}
    </div>
  );
}

/* ---- Environment slab shown in SIT / Prod views ---- */
function EnvironmentSlab({ view, project, onEdit }: {
  readonly view: ProjectView;
  readonly project: Project;
  readonly onEdit: () => void;
}) {
  const isSit = view === 'sit';
  const url = isSit ? project.sitUrl : project.prodUrl;
  const label = isSit ? 'SIT deployment' : 'Production deployment';
  const color = isSit ? '#ffaa00' : '#6ff2a0';

  if (!url) {
    return (
      <div style={{
        display: 'flex', alignItems: 'center', gap: 8,
        padding: '8px 10px', marginTop: 6,
        background: 'rgba(124,198,255,0.05)',
        borderLeft: '2px solid rgba(124,198,255,0.2)',
        fontSize: 11, opacity: 0.75,
      }}>
        <span style={{ flex: 1 }}>No {isSit ? 'SIT' : 'production'} URL yet</span>
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onEdit(); }}
          style={{
            padding: '2px 8px', fontSize: 10, color: '#00f0ff',
            background: 'transparent', border: '1px solid rgba(0,240,255,0.3)',
            borderRadius: 3, cursor: 'pointer', fontFamily: 'inherit',
          }}
        >
          <Edit3 size={10} /> Add URL
        </button>
      </div>
    );
  }
  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      onClick={(e) => e.stopPropagation()}
      style={{
        display: 'flex', alignItems: 'center', gap: 8,
        padding: '8px 10px', marginTop: 6,
        background: `${color}12`,
        borderLeft: `2px solid ${color}`,
        fontSize: 11, color: color,
        textDecoration: 'none',
      }}
    >
      <ExternalLink size={12} />
      <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        Open in {isSit ? 'SIT' : 'Production'}
      </span>
      <span style={{ fontSize: 9, opacity: 0.75 }}>{label}</span>
    </a>
  );
}

/* ---- Deploy chips (footer) ---- */
function DeployChips({ project }: { readonly project: Project }) {
  const chip = (label: string, on: boolean, color: string) => (
    <span
      key={label}
      style={{
        padding: '1px 5px', fontSize: 9,
        textTransform: 'uppercase',
        letterSpacing: '0.08em',
        color: on ? color : '#7cc6ff',
        background: on ? `${color}22` : 'transparent',
        border: `1px solid ${on ? color : '#7cc6ff44'}`,
        borderRadius: 3,
        opacity: on ? 1 : 0.5,
      }}
    >
      {label}
    </span>
  );
  return (
    <span style={{ marginLeft: 'auto', display: 'inline-flex', gap: 4 }}>
      {chip('LOCAL', !!project.localPath, '#00f0ff')}
      {chip('SIT', !!project.sitUrl, '#ffaa00')}
      {chip('PROD', !!project.prodUrl, '#6ff2a0')}
    </span>
  );
}

/* ---- ⋯ action menu on the card header ---- */
function CardActionMenu({
  project, open, onToggle, onEdit, onDelete, progress,
}: {
  readonly project: Project;
  readonly open: boolean;
  readonly onToggle: (e: React.MouseEvent) => void;
  readonly onEdit: (p: Project) => void;
  readonly onDelete: (p: Project) => void;
  readonly progress: number;
}) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, position: 'relative' }}>
      <span style={{ fontSize: 10, opacity: 0.75 }}>{progress}%</span>
      <button
        type="button"
        onClick={onToggle}
        style={{
          padding: 2, color: '#7cc6ff',
          background: 'transparent', border: '1px solid transparent',
          borderRadius: 3, cursor: 'pointer',
        }}
        title="Edit or delete this project"
      >
        <MoreHorizontal size={14} />
      </button>
      {open && (
        <div
          onClick={(e) => e.stopPropagation()}
          style={{
            position: 'absolute', top: '100%', right: 0, zIndex: 5,
            marginTop: 4, minWidth: 140,
            background: 'var(--surface, #0d111b)',
            border: '1px solid rgba(124,198,255,0.3)',
            borderRadius: 4, boxShadow: '0 6px 22px rgba(0,0,0,0.5)',
          }}
        >
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onEdit(project); }}
            style={menuItemStyle('#00f0ff')}
          >
            <Edit3 size={12} /> Edit
          </button>
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onDelete(project); }}
            style={menuItemStyle('#ff7b7b')}
          >
            <Trash2 size={12} /> Delete
          </button>
        </div>
      )}
    </span>
  );
}

function menuItemStyle(color: string): React.CSSProperties {
  return {
    display: 'flex', alignItems: 'center', gap: 6,
    width: '100%', padding: '6px 10px', fontSize: 11,
    fontFamily: 'inherit',
    color, background: 'transparent', border: 0,
    borderBottom: '1px solid rgba(124,198,255,0.1)',
    cursor: 'pointer', textAlign: 'left',
  };
}

const newBtnStyle: React.CSSProperties = {
  display: 'inline-flex', alignItems: 'center', gap: 4,
  padding: '4px 10px', fontSize: 10, fontFamily: 'inherit',
  textTransform: 'uppercase', letterSpacing: '0.08em',
  color: '#0a0c12',
  background: '#6ff2a0',
  border: '1px solid #6ff2a088',
  borderRadius: 4, cursor: 'pointer',
};
