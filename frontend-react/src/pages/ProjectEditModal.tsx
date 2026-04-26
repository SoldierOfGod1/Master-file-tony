/* ============================================================
   ProjectEditModal — shared create/edit dialog.
   Used from ProjectsPage and ClickUpPage for:
     • + New Project (when `project` is undefined)
     • ⋯ → Edit      (when `project` is populated)
   On submit it calls createProject / updateProject and invokes
   `onSaved(project)` so the caller can refresh its list.
   ============================================================ */

import {
  type FormEvent,
  useCallback,
  useEffect,
  useState,
} from 'react';
import { Save, X, Plus, Trash2 } from 'lucide-react';
import { createProject, updateProject } from '../api/projects';
import type { Project, ProjectComponent } from '../types/api';
import { PROJECT_STATUSES } from '../types/api';

interface Props {
  readonly project?: Project; // undefined = create mode
  readonly onClose: () => void;
  readonly onSaved: (p: Project) => void;
}

const PRIORITIES = ['urgent', 'high', 'normal', 'low'] as const;
const COMPONENT_ROLES = ['core', 'frontend', 'backend', 'infra'] as const;

export default function ProjectEditModal({ project, onClose, onSaved }: Props) {
  const isEdit = !!project?.id;

  const [name, setName] = useState(project?.name ?? '');
  const [description, setDescription] = useState(project?.description ?? '');
  const [status, setStatus] = useState<string>(project?.status ?? 'To Do');
  const [priority, setPriority] = useState<string>(project?.priority ?? 'normal');
  const [owner, setOwner] = useState(project?.owner ?? 'baptista');
  const [localPath, setLocalPath] = useState(project?.localPath ?? '');
  const [sitUrl, setSitUrl] = useState(project?.sitUrl ?? '');
  const [prodUrl, setProdUrl] = useState(project?.prodUrl ?? '');
  const [components, setComponents] = useState<ProjectComponent[]>(
    project?.components ?? [{ role: 'core', path: '' }],
  );
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Allow Escape to close.
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [onClose]);

  const addComponent = useCallback(() => {
    setComponents((cs) => [...cs, { role: 'core', path: '' }]);
  }, []);
  const removeComponent = useCallback((i: number) => {
    setComponents((cs) => cs.filter((_, idx) => idx !== i));
  }, []);
  const updateComponent = useCallback((i: number, patch: Partial<ProjectComponent>) => {
    setComponents((cs) => cs.map((c, idx) => idx === i ? { ...c, ...patch } : c));
  }, []);

  const canSubmit = name.trim().length > 0 && !busy;

  const submit = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true); setErr(null);
    try {
      const cleanComponents = components.filter((c) => c.role && c.path);
      const payload: Partial<Project> = {
        name: name.trim(),
        description: description.trim(),
        status,
        priority,
        owner: owner.trim(),
        localPath: localPath.trim(),
        sitUrl: sitUrl.trim(),
        prodUrl: prodUrl.trim(),
      };
      // Components round-trip would need a backend addition — for now we
      // send only fields the current PUT handler understands. Local
      // components still display from the existing seed/row.
      let saved: Project | null;
      if (isEdit && project) {
        saved = await updateProject(project.id, payload);
      } else {
        saved = await createProject(payload as Omit<Project, 'id'>);
      }
      if (!saved) {
        setErr('save failed');
      } else {
        // Merge local edits onto the returned record so the UI reflects
        // the user's latest intent immediately (backend returns minimal
        // shape {id, status}).
        onSaved({ ...(project as Project), ...payload, id: saved.id ?? project?.id ?? '' } as Project);
      }
      // Components list preserved in local state; backend read on next refresh
      void cleanComponents;
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'save failed');
    } finally {
      setBusy(false);
    }
  }, [canSubmit, components, name, description, status, priority, owner,
      localPath, sitUrl, prodUrl, isEdit, project, onSaved]);

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed', inset: 0, zIndex: 100,
        background: 'rgba(0,0,0,0.7)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        padding: 20,
      }}
    >
      <form
        onSubmit={submit}
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(640px, 100%)',
          maxHeight: '90vh', overflow: 'auto',
          background: 'var(--surface, #0d111b)',
          border: '1px solid rgba(0,240,255,0.3)',
          borderRadius: 8, padding: 20,
          display: 'grid', gap: 10,
          fontFamily: 'inherit',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {isEdit ? <Save size={16} color="#00f0ff" /> : <Plus size={16} color="#00f0ff" />}
          <h3 style={{ margin: 0, fontSize: 14, color: '#00f0ff' }}>
            {isEdit ? `Edit · ${project?.name}` : 'New project'}
          </h3>
          <button type="button" onClick={onClose}
                  style={{ marginLeft: 'auto', ...iconBtn() }}>
            <X size={14} />
          </button>
        </div>

        <Field label="Name">
          <input type="text" value={name} onChange={(e) => setName(e.target.value)}
                 autoFocus required style={inputStyle} placeholder="Baptista Finance Dashboard" />
        </Field>

        <Field label="Description">
          <textarea value={description} onChange={(e) => setDescription(e.target.value)}
                    rows={3} style={{ ...inputStyle, resize: 'vertical' }}
                    placeholder="What does this project do? Context for the team." />
        </Field>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 8 }}>
          <Field label="Status">
            <select value={status} onChange={(e) => setStatus(e.target.value)} style={inputStyle}>
              {PROJECT_STATUSES.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </Field>
          <Field label="Priority">
            <select value={priority} onChange={(e) => setPriority(e.target.value)} style={inputStyle}>
              {PRIORITIES.map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
          </Field>
          <Field label="Owner">
            <input type="text" value={owner} onChange={(e) => setOwner(e.target.value)}
                   style={inputStyle} />
          </Field>
        </div>

        <Field label="Local path (where the code lives on this machine)">
          <input type="text" value={localPath} onChange={(e) => setLocalPath(e.target.value)}
                 style={inputStyle}
                 placeholder="C:\Users\…\MyProject" />
        </Field>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
          <Field label="🧪 SIT URL">
            <input type="url" value={sitUrl} onChange={(e) => setSitUrl(e.target.value)}
                   style={inputStyle} placeholder="https://myproject-sit.vibe.rain.co.za/" />
          </Field>
          <Field label="🚀 Production URL">
            <input type="url" value={prodUrl} onChange={(e) => setProdUrl(e.target.value)}
                   style={inputStyle} placeholder="https://myproject.rain.co.za/" />
          </Field>
        </div>

        <div>
          <div style={labelTextStyle}>Components</div>
          <div style={{ display: 'grid', gap: 5 }}>
            {components.map((c, i) => (
              <div key={i} style={{ display: 'grid', gridTemplateColumns: '110px 1fr 26px', gap: 6 }}>
                <select value={c.role}
                        onChange={(e) => updateComponent(i, { role: e.target.value })}
                        style={inputStyle}>
                  {COMPONENT_ROLES.map((r) => <option key={r} value={r}>{r}</option>)}
                </select>
                <input type="text" value={c.path}
                       onChange={(e) => updateComponent(i, { path: e.target.value })}
                       placeholder="C:\…\component-folder"
                       style={inputStyle} />
                <button type="button" onClick={() => removeComponent(i)}
                        style={iconBtn()} title="Remove component">
                  <Trash2 size={12} />
                </button>
              </div>
            ))}
            <button type="button" onClick={addComponent} style={{ ...iconBtn(), width: 'auto', padding: '4px 10px' }}>
              <Plus size={12} /> Add component
            </button>
          </div>
          <div style={{ fontSize: 10, opacity: 0.6, marginTop: 4 }}>
            Note: component edits are saved on a later pass; today only name / description / status
            / priority / owner / paths / URLs round-trip via this form.
          </div>
        </div>

        {err && (
          <div style={{ color: '#ff7b7b', fontSize: 11, padding: 4 }}>{err}</div>
        )}

        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <button type="button" onClick={onClose} style={btn('#7cc6ff', false)}>Cancel</button>
          <button type="submit" disabled={!canSubmit} style={btn('#6ff2a0', !canSubmit)}>
            <Save size={12} /> {busy ? 'Saving…' : (isEdit ? 'Save changes' : 'Create project')}
          </button>
        </div>
      </form>
    </div>
  );
}

function Field({ label, children }: { readonly label: string; readonly children: React.ReactNode }) {
  return (
    <label style={{ display: 'grid', gap: 3 }}>
      <span style={labelTextStyle}>{label}</span>
      {children}
    </label>
  );
}

const labelTextStyle: React.CSSProperties = {
  fontSize: 11,
  color: 'var(--ink-dim, #7cc6ff)',
  textTransform: 'uppercase',
  letterSpacing: '0.06em',
};
const inputStyle: React.CSSProperties = {
  padding: '6px 8px',
  background: 'rgba(0,0,0,0.3)',
  color: 'var(--ink, #e6f6ff)',
  border: '1px solid rgba(124,198,255,0.25)',
  borderRadius: 4, fontFamily: 'inherit', fontSize: 12,
  width: '100%',
};
function iconBtn(): React.CSSProperties {
  return {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
    width: 26, height: 26,
    color: '#7cc6ff',
    background: 'transparent',
    border: '1px solid rgba(124,198,255,0.25)',
    borderRadius: 4, cursor: 'pointer',
  };
}
function btn(color: string, disabled: boolean): React.CSSProperties {
  return {
    display: 'inline-flex', alignItems: 'center', gap: 4,
    padding: '4px 12px', fontSize: 11, fontFamily: 'inherit',
    textTransform: 'uppercase', letterSpacing: '0.06em',
    color: disabled ? '#7cc6ff' : '#0a0c12',
    background: disabled ? 'rgba(124,198,255,0.12)' : color,
    border: `1px solid ${color}66`,
    borderRadius: 4,
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.6 : 1,
  };
}
