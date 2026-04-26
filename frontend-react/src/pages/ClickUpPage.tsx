/* ============================================================
   ClickUpPage — two sections, both backed by ClickUp.
     1. Project Kanban — the 14 projects from /projects flowing
        through the 10-status SDLC pipeline. 2-way synced.
     2. Ad-hoc Tasks — any ClickUp task in the same list that
        isn't linked to a project. 4-column board + create form.
   All ClickUp chrome lives here; /projects has no ClickUp UI.
   ============================================================ */

import {
  type FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react';
import {
  Plus,
  RefreshCw,
  ListTodo,
  ExternalLink,
  Inbox,
  FolderKanban,
  MoreHorizontal,
  Edit3,
  Trash2,
} from 'lucide-react';
import Modal from '../components/shared/Modal';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { useCommandCentre } from '../context/CommandCentreContext';
import {
  type ClickUpConfig,
  type ClickUpTask,
  createClickUpTask,
  getClickUpConfig,
  listClickUpTasks,
} from '../api/clickup';
import { deleteProject, getSyncStatus, syncProjects, updateProject } from '../api/projects';
import ProjectEditModal from './ProjectEditModal';
import {
  PROJECT_STATUSES,
  type Project,
  type ProjectStatus,
} from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './ClickUpPage.module.css';

/* ------------------------------------------------------------
   Project kanban (10 columns, synced to ClickUp)
   ------------------------------------------------------------ */

const PROJECT_STATUS_COLOR: Record<string, string> = {
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
};
const colorForStatus = (s: string): string =>
  PROJECT_STATUS_COLOR[s.toLowerCase().trim()] ?? '#7cc6ff';

function formatRelative(iso?: string): string {
  if (!iso) return '—';
  const then = new Date(iso).getTime();
  if (isNaN(then)) return iso;
  const diff = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

function ProjectCard({
  project, onStatus, onEdit, onDelete, updating,
}: {
  readonly project: Project;
  readonly onStatus: (p: Project, status: ProjectStatus) => void;
  readonly onEdit: (p: Project) => void;
  readonly onDelete: (p: Project) => void;
  readonly updating: boolean;
}) {
  const accent = colorForStatus(project.status);
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => {
    if (!menuOpen) return;
    const close = () => setMenuOpen(false);
    window.addEventListener('click', close);
    return () => window.removeEventListener('click', close);
  }, [menuOpen]);

  return (
    <div className={styles.projCard} style={{ borderLeftColor: accent }}>
      <div className={styles.projCardHead}>
        <span className={styles.projTitle}>{project.name}</span>
        {project.clickupUrl && (
          <a
            href={project.clickupUrl}
            target="_blank"
            rel="noreferrer"
            className={styles.extLink}
            onClick={(e) => e.stopPropagation()}
            title="Open in ClickUp"
          >
            <ExternalLink size={11} />
          </a>
        )}
        <span style={{ position: 'relative', display: 'inline-flex' }}>
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); setMenuOpen((v) => !v); }}
            title="Edit or delete"
            style={{
              padding: 2, color: '#7cc6ff', background: 'transparent',
              border: 'none', cursor: 'pointer',
            }}
          >
            <MoreHorizontal size={12} />
          </button>
          {menuOpen && (
            <div
              onClick={(e) => e.stopPropagation()}
              style={{
                position: 'absolute', top: '100%', right: 0, zIndex: 5,
                marginTop: 4, minWidth: 128,
                background: 'var(--surface, #0d111b)',
                border: '1px solid rgba(124,198,255,0.3)',
                borderRadius: 4, boxShadow: '0 6px 22px rgba(0,0,0,0.5)',
              }}
            >
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); setMenuOpen(false); onEdit(project); }}
                style={menuItemStyle('#00f0ff')}
              >
                <Edit3 size={11} /> Edit
              </button>
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); setMenuOpen(false); onDelete(project); }}
                style={menuItemStyle('#ff7b7b')}
              >
                <Trash2 size={11} /> Delete
              </button>
            </div>
          )}
        </span>
      </div>

      {project.localPath && (
        <div className={styles.projPath} title={project.localPath}>
          {project.localPath}
        </div>
      )}

      <div className={styles.projCardFooter}>
        <select
          className={styles.statusSelect}
          value={project.status}
          disabled={updating}
          onClick={(e) => e.stopPropagation()}
          onChange={(e) => onStatus(project, e.target.value as ProjectStatus)}
          style={{ color: accent, borderColor: `${accent}66` }}
        >
          {PROJECT_STATUSES.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <span className={styles.projSync} title="Last ClickUp sync">
          {project.clickupTaskId
            ? `sync: ${formatRelative(project.clickupLastSync)}`
            : 'not synced'}
        </span>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------
   Ad-hoc tasks (the legacy free-form kanban, narrowed to tasks
   that aren't backing any project).
   ------------------------------------------------------------ */

const AD_HOC_COLUMNS: {
  key: string;
  label: string;
  color: string;
  match: (s: string) => boolean;
}[] = [
  { key: 'todo',        label: 'To Do',       color: '#7cc6ff', match: (s) => /^(to.?do|open|new|backlog)$/i.test(s) },
  { key: 'in_progress', label: 'In Progress', color: '#00f0ff', match: (s) => /^(in.?progress|doing|active|working)$/i.test(s) },
  { key: 'review',      label: 'Review',      color: '#ffc566', match: (s) => /^(review|in.?review|qa|testing)$/i.test(s) },
  { key: 'done',        label: 'Done',        color: '#6ff2a0', match: (s) => /^(done|closed|complete|completed|resolved)$/i.test(s) },
];

function priorityColor(p?: string): string {
  switch ((p ?? '').toLowerCase()) {
    case 'urgent': return '#ff3355';
    case 'high':   return '#ff7de0';
    case 'normal': return '#7cc6ff';
    case 'low':    return '#6ff2a0';
    default:       return '#7cc6ff';
  }
}

function TaskCard({ task }: { readonly task: ClickUpTask }) {
  return (
    <a className={styles.taskCard} href={task.url} target="_blank" rel="noreferrer">
      <div className={styles.taskCardHead}>
        <span className={styles.taskTitle}>{task.name}</span>
        <ExternalLink size={10} className={styles.taskExt} />
      </div>
      <div className={styles.taskMeta}>
        {task.priority && <HudChip color={priorityColor(task.priority)}>{task.priority}</HudChip>}
        {(task.tags ?? []).slice(0, 3).map((t) => (
          <HudChip key={t} color="#00f0ff">{t}</HudChip>
        ))}
        {(task.assignees ?? []).slice(0, 2).map((a) => (
          <span key={a} className={styles.assignee}>@{a}</span>
        ))}
      </div>
    </a>
  );
}

/* ------------------------------------------------------------
   Page component
   ------------------------------------------------------------ */

export default function ClickUpPage() {
  const { state, refreshAll } = useCommandCentre();
  const projects = state.projects;

  // ---- Ad-hoc tasks state (fetched lazily; separate from global state) ----
  const [config, setConfig] = useState<ClickUpConfig | null>(null);
  const [tasks, setTasks] = useState<ClickUpTask[]>([]);
  const [loadingTasks, setLoadingTasks] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');

  // ---- Kanban state (shared with /projects, just rendered differently) ----
  const [syncing, setSyncing] = useState(false);
  const [updatingId, setUpdatingId] = useState<string | null>(null);
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [creatingNew, setCreatingNew] = useState(false);

  // ---- Data loaders ----
  const reloadTasks = useCallback(async () => {
    setLoadingTasks(true);
    const [cfg, t] = await Promise.all([getClickUpConfig(), listClickUpTasks()]);
    setConfig(cfg);
    setTasks(t);
    setLoadingTasks(false);
  }, []);
  useEffect(() => { void reloadTasks(); }, [reloadTasks]);

  // Filter project-linked tasks out of the ad-hoc list.
  const adHocTasks = useMemo(() => {
    const linkedIds = new Set(
      projects.map((p) => p.clickupTaskId).filter((x): x is string => !!x),
    );
    return tasks.filter((t) => !linkedIds.has(t.id));
  }, [tasks, projects]);

  // Bucket projects + ad-hoc tasks for their respective kanbans.
  const projectsByStatus = useMemo(() => {
    const m = new Map<string, Project[]>();
    for (const s of PROJECT_STATUSES) m.set(s, []);
    for (const p of projects) {
      const match = PROJECT_STATUSES.find(
        (s) => s.toLowerCase() === p.status.toLowerCase().trim(),
      ) ?? 'To Do';
      (m.get(match) ?? []).push(p);
    }
    return m;
  }, [projects]);

  const tasksByColumn = useMemo(() => {
    const m: Record<string, ClickUpTask[]> = {};
    for (const c of AD_HOC_COLUMNS) m[c.key] = [];
    const backlog: ClickUpTask[] = [];
    for (const t of adHocTasks) {
      const match = AD_HOC_COLUMNS.find((c) => c.match(t.status ?? ''));
      if (match) m[match.key].push(t);
      else backlog.push(t);
    }
    m['todo'] = [...backlog, ...m['todo']];
    return m;
  }, [adHocTasks]);

  // ---- Actions ----
  const handleStatus = useCallback(async (p: Project, status: ProjectStatus) => {
    setUpdatingId(p.id);
    try {
      await updateProject(p.id, { status });
      await refreshAll();
    } finally {
      setUpdatingId(null);
    }
  }, [refreshAll]);

  const handleEdit = useCallback((p: Project) => setEditingProject(p), []);

  const handleDelete = useCallback(async (p: Project) => {
    const ok = window.confirm(
      `Delete "${p.name}"?\n\nThis removes the project locally AND its ClickUp task (+ subtasks).\nThis cannot be undone.`,
    );
    if (!ok) return;
    const success = await deleteProject(p.id);
    if (success) {
      await refreshAll();
    } else {
      window.alert('Delete failed — check backend logs.');
    }
  }, [refreshAll]);

  const handleSaved = useCallback(async () => {
    setEditingProject(null);
    setCreatingNew(false);
    await refreshAll();
  }, [refreshAll]);

  const handleSyncAll = useCallback(async () => {
    setSyncing(true);
    try {
      // Kick off the async sync — returns 202 immediately.
      await syncProjects();
      // Poll every 1.5s until the server reports in_progress=false.
      // Caps at 10 minutes to prevent runaway polling on a hung run.
      const deadline = Date.now() + 10 * 60 * 1000;
      for (;;) {
        await new Promise((r) => setTimeout(r, 1500));
        const s = await getSyncStatus();
        if (!s || !s.in_progress) break;
        if (Date.now() > deadline) break;
      }
      await refreshAll();
      await reloadTasks();
    } finally {
      setSyncing(false);
    }
  }, [refreshAll, reloadTasks]);

  const handleCreateTask = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setSubmitting(true);
    const result = await createClickUpTask({
      name: name.trim(),
      description: description.trim() || undefined,
    });
    setSubmitting(false);
    if (result) {
      setModalOpen(false);
      setName('');
      setDescription('');
      void reloadTasks();
    }
  }, [name, description, reloadTasks]);

  // Configuration status — drives a warning banner + disables sync buttons.
  // The project kanban itself renders regardless (projects live in the
  // local DB, ClickUp is optional sync).
  const clickupReady = !!config && config.configured;

  const total = projects.length;
  const completed = projectsByStatus.get('Completed')?.length ?? 0;
  const syncedCount = projects.filter((p) => p.clickupTaskId).length;
  const ratio = total === 0 ? 0 : completed / total;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="ClickUp · Project Pipeline"
        subtitle={`${total} projects · ${completed} completed · ${syncedCount}/${total} synced · list ${config?.list_id ?? '…'}`}
        gaugeValue={ratio}
        gaugeReadout={`${completed}/${total}`}
        gaugeLabel="DONE"
        gaugeColor="#6ff2a0"
        segments={PROJECT_STATUSES.map((s) => ({
          label: s,
          value: projectsByStatus.get(s)?.length ?? 0,
          color: colorForStatus(s),
        }))}
        extra={
          <div className={styles.headerActions}>
            <button
              type="button"
              className={styles.iconBtn}
              onClick={() => { void refreshAll(); void reloadTasks(); }}
              title="Refresh"
            >
              <RefreshCw size={13} />
              Refresh
            </button>
            <button
              type="button"
              className={styles.syncBtn}
              onClick={handleSyncAll}
              disabled={syncing || !clickupReady}
              title={clickupReady ? 'Push every project to ClickUp' : 'ClickUp not configured — see Settings'}
            >
              <RefreshCw size={13} className={syncing ? styles.spin : undefined} />
              {syncing ? 'Syncing…' : 'Sync now'}
            </button>
            <button
              type="button"
              className={styles.syncBtn}
              onClick={() => setCreatingNew(true)}
              title="Create a new project (auto-pushes to ClickUp on save)"
              style={{
                background: 'rgba(111,242,160,0.15)',
                borderColor: 'rgba(111,242,160,0.5)',
                color: '#6ff2a0',
              }}
            >
              <Plus size={13} /> New project
            </button>
          </div>
        }
      />

      {!clickupReady && !loadingTasks && (
        <div className={styles.warnBanner}>
          <ListTodo size={14} />
          <div>
            <strong>ClickUp not connected.</strong> The project pipeline still
            works locally. To sync statuses to ClickUp (and see ad-hoc tasks),
            paste your API token in{' '}
            <a href="/settings" className={styles.link}>Settings</a>.
          </div>
        </div>
      )}

      {/* ========== Section 1 — project kanban (10 statuses) ========== */}
      <div className={styles.sectionTitle}>
        <FolderKanban size={14} /> Project Pipeline
        <span className={styles.sectionCount}>{total}</span>
      </div>

      {total === 0 ? (
        <HudPanel
          title="Pipeline"
          accent="#7cc6ff"
          leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        >
          <div className={styles.empty}>// no projects yet</div>
        </HudPanel>
      ) : (
        <div className={styles.kanban}>
          {PROJECT_STATUSES.map((status) => {
            const list = projectsByStatus.get(status) ?? [];
            const accent = colorForStatus(status);
            return (
              <div key={status} className={styles.column}>
                <HudPanel
                  title={status}
                  accent={accent}
                  leading={<HudStatusLed color={accent} animate={list.length > 0} />}
                  meta={<>{list.length}</>}
                >
                  <div className={styles.cardStack}>
                    {list.length === 0 ? (
                      <div className={styles.dropHint}>// empty</div>
                    ) : (
                      list.map((p) => (
                        <ProjectCard
                          key={p.id}
                          project={p}
                          onStatus={handleStatus}
                          onEdit={handleEdit}
                          onDelete={handleDelete}
                          updating={updatingId === p.id}
                        />
                      ))
                    )}
                  </div>
                </HudPanel>
              </div>
            );
          })}
        </div>
      )}

      {/* ========== Section 2 — ad-hoc tasks (4 columns + create) ========== */}
      <div className={styles.sectionTitle}>
        <Inbox size={14} /> Ad-hoc Tasks
        <span className={styles.sectionCount}>{adHocTasks.length}</span>
        <span className={styles.sectionSub}>
          tasks on the same ClickUp list but not linked to a project
        </span>
        <button
          type="button"
          className={styles.addBtn}
          onClick={() => setModalOpen(true)}
          disabled={!clickupReady}
          title={clickupReady ? 'Create a new ad-hoc task' : 'ClickUp not configured'}
        >
          <Plus size={13} /> New Task
        </button>
      </div>

      {!clickupReady ? (
        <HudPanel
          title="Ad-hoc"
          accent="#ffaa00"
          leading={<HudStatusLed color="#ffaa00" animate={false} />}
        >
          <div className={styles.empty}>
            // connect ClickUp in Settings to see ad-hoc tasks
          </div>
        </HudPanel>
      ) : adHocTasks.length === 0 ? (
        <HudPanel
          title="Ad-hoc"
          accent="#7cc6ff"
          leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        >
          <div className={styles.empty}>
            // no free-form tasks — use the New Task button to add one
          </div>
        </HudPanel>
      ) : (
        <div className={styles.adHocBoard}>
          {AD_HOC_COLUMNS.map((col) => {
            const list = tasksByColumn[col.key] ?? [];
            return (
              <HudPanel
                key={col.key}
                title={col.label}
                accent={col.color}
                leading={<HudStatusLed color={col.color} animate={list.length > 0} />}
                meta={<>{list.length}</>}
              >
                <div className={styles.cardStack}>
                  {list.length === 0 ? (
                    <div className={styles.dropHint}>// no tasks</div>
                  ) : (
                    list.map((t) => <TaskCard key={t.id} task={t} />)
                  )}
                </div>
              </HudPanel>
            );
          })}
        </div>
      )}

      {/* ---- Create-task modal ---- */}
      <Modal
        isOpen={modalOpen}
        onClose={() => setModalOpen(false)}
        title="Create Ad-hoc Task"
      >
        <form onSubmit={handleCreateTask} className={styles.form}>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="cu-name">Task name</label>
            <input
              id="cu-name"
              type="text"
              className={styles.input}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. triage BulkRisk flag export"
              autoFocus
              required
            />
          </div>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="cu-desc">Description</label>
            <textarea
              id="cu-desc"
              className={styles.textarea}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional details"
            />
          </div>
          <div className={styles.actions}>
            <button type="button" className={styles.cancel} onClick={() => setModalOpen(false)}>
              Cancel
            </button>
            <button type="submit" className={styles.submit} disabled={submitting || !name.trim()}>
              {submitting ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </Modal>

      {(editingProject || creatingNew) && (
        <ProjectEditModal
          project={editingProject ?? undefined}
          onClose={() => { setEditingProject(null); setCreatingNew(false); }}
          onSaved={handleSaved}
        />
      )}
    </div>
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
