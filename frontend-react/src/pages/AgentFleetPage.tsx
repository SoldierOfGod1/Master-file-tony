/* ============================================================
   Agent Fleet — filesystem-backed catalogue of everything
   agent-related in this workspace. Three sub-tabs:
     · Agents — both global (~/.claude/agents) and project
       (./.claude/agents), grouped by category, with a per-agent
       append-only memory file the agent can learn from.
     · Hooks — scripts under .claude/hooks/.
     · Rules — docs under .claude/rules/ (+ language subfolders).
   ============================================================ */

import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import {
  Bot,
  FileCode,
  GitBranch,
  Layers,
  Plus,
  RefreshCw,
  Save,
  Sparkles,
  Zap,
  Scroll,
} from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  appendAgentMemory,
  createFleetAgent,
  listFleetAgents,
  listFleetHooks,
  listFleetRules,
  readAgentMemory,
  readFleetFile,
  writeFleetFile,
  type FleetAgent,
  type FleetHook,
  type FleetRule,
} from '../api/agentFleet';
import hudStyles from '../theme/hud.module.css';
import styles from './AgentFleetPage.module.css';

type Tab = 'agents' | 'hooks' | 'rules';

/* Colours used by every sub-tab. Kept local because nothing else in the
   dashboard needs an agent-category palette. */
const CATEGORY_COLOR: Record<string, string> = {
  'Orchestration':       '#00f0ff',
  'Backend & Data':      '#6ff2a0',
  'Frontend & UI':       '#ff7de0',
  'AI & ML':             '#ffc566',
  'Quality & Security':  '#ff3355',
  'Testing':             '#7cc6ff',
  'DevOps & Infra':      '#ffaa00',
  'Research & Docs':     '#c07fff',
  'Utilities & Tools':   '#00ff88',
  'Language Reviewers':  '#80f0ff',
  'Other':               '#7cc6ff',
};
const colorFor = (cat: string): string => CATEGORY_COLOR[cat] ?? '#7cc6ff';

/* ============================================================
   Agents tab — grouped-by-category grid of clickable agent cards.
   Clicking a card opens a detail panel on the right with the
   full .md file + the append-only memory log + a "record a
   lesson" input.
   ============================================================ */

function AgentsTab() {
  const [agents, setAgents] = useState<FleetAgent[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<FleetAgent | null>(null);
  const [sourceFilter, setSourceFilter] = useState<'all' | 'global' | 'project' | 'plugin'>('all');
  const [categoryFilter, setCategoryFilter] = useState<string | null>(null);
  const [query, setQuery] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setAgents(await listFleetAgents());
    setLoading(false);
  }, []);
  useEffect(() => { void load(); }, [load]);

  /* Drop agents that don't match the active filters. The filter chips
     below update in real time as the user types into the search box. */
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return agents.filter((a) => {
      if (sourceFilter !== 'all' && a.source !== sourceFilter) return false;
      if (categoryFilter && a.category !== categoryFilter) return false;
      if (q && !(`${a.name} ${a.description}`.toLowerCase().includes(q))) return false;
      return true;
    });
  }, [agents, query, sourceFilter, categoryFilter]);

  /* Count every agent per category — used both for the chip counts and
     the grouped grid below. */
  const byCategory = useMemo(() => {
    const m = new Map<string, FleetAgent[]>();
    for (const a of filtered) {
      const arr = m.get(a.category) ?? [];
      arr.push(a);
      m.set(a.category, arr);
    }
    return Array.from(m.entries()).sort((a, b) => b[1].length - a[1].length);
  }, [filtered]);

  const sourceCounts = useMemo(() => {
    const m: Record<string, number> = { global: 0, project: 0, plugin: 0 };
    for (const a of agents) m[a.source] = (m[a.source] ?? 0) + 1;
    return m;
  }, [agents]);

  const allCategories = useMemo(() => {
    const counts = new Map<string, number>();
    for (const a of agents) counts.set(a.category, (counts.get(a.category) ?? 0) + 1);
    return Array.from(counts.entries()).sort((a, b) => b[1] - a[1]);
  }, [agents]);

  return (
    <div className={styles.agentsLayout}>
      {/* LEFT: filters + grid */}
      <div className={styles.agentsLeft}>
        <div className={styles.controls}>
          <input
            type="search"
            className={styles.search}
            placeholder="// search agents"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <div className={styles.sourceGroup}>
            {([
              ['all', `All (${agents.length})`],
              ['project', `Project (${sourceCounts.project ?? 0})`],
              ['global', `Global (${sourceCounts.global ?? 0})`],
              ['plugin', `Plugin (${sourceCounts.plugin ?? 0})`],
            ] as const).map(([key, label]) => (
              <button
                key={key}
                type="button"
                className={`${styles.sourceBtn} ${sourceFilter === key ? styles.sourceBtnActive : ''}`}
                onClick={() => setSourceFilter(key as 'all' | 'global' | 'project' | 'plugin')}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        <div className={styles.chipRow}>
          <button
            type="button"
            className={`${styles.chip} ${categoryFilter === null ? styles.chipActive : ''}`}
            onClick={() => setCategoryFilter(null)}
          >
            All categories
          </button>
          {allCategories.map(([cat, n]) => (
            <button
              key={cat}
              type="button"
              className={`${styles.chip} ${categoryFilter === cat ? styles.chipActive : ''}`}
              style={{
                borderColor: categoryFilter === cat ? colorFor(cat) : undefined,
                color: categoryFilter === cat ? colorFor(cat) : undefined,
              }}
              onClick={() => setCategoryFilter(categoryFilter === cat ? null : cat)}
            >
              {cat}
              <span className={styles.chipCount}>{n}</span>
            </button>
          ))}
        </div>

        {loading ? (
          <HudPanel title="Loading" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" />}>
            <div className={styles.empty}>// scanning .claude/agents…</div>
          </HudPanel>
        ) : byCategory.length === 0 ? (
          <HudPanel title="No matches" accent="#ffaa00" leading={<HudStatusLed color="#ffaa00" animate={false} />}>
            <div className={styles.empty}>// no agents match these filters</div>
          </HudPanel>
        ) : (
          <div className={styles.agentGroups}>
            {byCategory.map(([cat, rows]) => (
              <HudPanel
                key={cat}
                title={cat}
                accent={colorFor(cat)}
                leading={<HudStatusLed color={colorFor(cat)} animate={false} />}
                meta={<><Bot size={10} /> {rows.length}</>}
              >
                <div className={styles.agentGrid}>
                  {rows.map((a) => (
                    <AgentCard
                      key={a.id}
                      agent={a}
                      selected={selected?.id === a.id}
                      onSelect={setSelected}
                    />
                  ))}
                </div>
              </HudPanel>
            ))}
          </div>
        )}
      </div>

      {/* RIGHT: detail panel */}
      <div className={styles.agentsRight}>
        {selected ? (
          <AgentDetail agent={selected} onChange={load} />
        ) : (
          <HudPanel
            title="Agent Detail"
            accent="#7cc6ff"
            leading={<HudStatusLed color="#7cc6ff" animate={false} />}
          >
            <div className={styles.detailPlaceholder}>
              <Sparkles size={28} />
              <span>Select an agent to read its definition, inspect its memory log, or record a lesson.</span>
            </div>
          </HudPanel>
        )}
      </div>
    </div>
  );
}

function AgentCard({
  agent, selected, onSelect,
}: {
  readonly agent: FleetAgent;
  readonly selected: boolean;
  readonly onSelect: (a: FleetAgent) => void;
}) {
  const accent = colorFor(agent.category);
  const sourceColor =
    agent.source === 'project' ? '#6ff2a0' :
    agent.source === 'plugin'  ? '#c488ff' :
                                  '#00f0ff';
  return (
    <button
      type="button"
      className={`${styles.agentCard} ${selected ? styles.agentCardSelected : ''}`}
      style={{ borderLeftColor: accent }}
      onClick={() => onSelect(agent)}
    >
      <div className={styles.agentCardHead}>
        <span className={styles.agentCardName}>{agent.name}</span>
        <div className={styles.agentCardBadges}>
          <HudChip color={sourceColor}>{agent.source}</HudChip>
          {agent.plugin && <HudChip color="#c488ff">{agent.plugin}</HudChip>}
          {agent.overrides && <HudChip color="#ffaa00">OVERRIDE</HudChip>}
          {agent.has_memory && <HudChip color="#ff7de0">MEMORY</HudChip>}
        </div>
      </div>
      {agent.description && (
        <div className={styles.agentCardDesc}>{agent.description}</div>
      )}
      <div className={styles.agentCardFooter}>
        {agent.model && <span className={styles.agentCardMeta}>{agent.model}</span>}
        {agent.version && <span className={styles.agentCardMeta}>v{agent.version}</span>}
        {agent.thinking && <span className={styles.agentCardMeta}>{agent.thinking}</span>}
      </div>
    </button>
  );
}

function AgentDetail({ agent, onChange }: {
  readonly agent: FleetAgent;
  readonly onChange: () => void;
}) {
  const [body, setBody] = useState('');
  const [draft, setDraft] = useState('');      // editable copy of `body`
  const [memory, setMemory] = useState('');
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);
  const [savingSource, setSavingSource] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [lastSavedAt, setLastSavedAt] = useState<number | null>(null);
  const [mode, setMode] = useState<'source' | 'memory'>('memory');

  const isReadonly = agent.source === 'plugin';
  const dirty = draft !== body;

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const [src, mem] = await Promise.all([
        readFleetFile(agent.path),
        readAgentMemory(agent.path),
      ]);
      if (cancelled) return;
      setBody(src);
      setDraft(src);
      setMemory(mem);
      setSaveError(null);
    })();
    return () => { cancelled = true; };
  }, [agent.path]);

  const handleSaveSource = useCallback(async () => {
    if (!dirty || isReadonly) return;
    setSavingSource(true);
    setSaveError(null);
    try {
      const updated = await writeFleetFile(agent.path, draft);
      setBody(updated);
      setDraft(updated);
      setLastSavedAt(Date.now());
      onChange();
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'save failed');
    } finally {
      setSavingSource(false);
    }
  }, [agent.path, draft, dirty, isReadonly, onChange]);

  const handleCloneToGlobal = useCallback(async () => {
    setSavingSource(true);
    setSaveError(null);
    try {
      const created = await createFleetAgent({
        name: agent.name + '-copy',
        description: agent.description || 'Cloned from ' + agent.file_name,
        category: agent.category,
        source: 'global',
        body: draft,
      });
      if (created) {
        onChange();
      }
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'clone failed');
    } finally {
      setSavingSource(false);
    }
  }, [agent, draft, onChange]);

  const handleAppend = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    if (!note.trim()) return;
    setSaving(true);
    const updated = await appendAgentMemory(agent.path, note.trim());
    setMemory(updated);
    setNote('');
    setSaving(false);
    onChange();
  }, [agent.path, note, onChange]);

  const accent = colorFor(agent.category);

  return (
    <HudPanel
      title={agent.name}
      accent={accent}
      leading={<HudStatusLed color={accent} />}
      meta={<HudChip color={accent}>{agent.category}</HudChip>}
      footer={
        <div className={styles.detailFooter}>
          <code className={styles.detailPath}>{agent.path}</code>
        </div>
      }
    >
      <div className={styles.detailTabs}>
        <button
          type="button"
          className={`${styles.detailTab} ${mode === 'memory' ? styles.detailTabActive : ''}`}
          onClick={() => setMode('memory')}
        >
          <Sparkles size={11} /> Memory {memory ? '' : '(empty)'}
        </button>
        <button
          type="button"
          className={`${styles.detailTab} ${mode === 'source' ? styles.detailTabActive : ''}`}
          onClick={() => setMode('source')}
        >
          <FileCode size={11} /> Source
        </button>
      </div>

      {mode === 'memory' ? (
        <>
          <pre className={styles.memoryBox}>
            {memory || '// no lessons yet — record the first one below'}
          </pre>
          <form onSubmit={handleAppend} className={styles.appendForm}>
            <textarea
              className={styles.appendInput}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="What worked / what to avoid next time…"
              rows={3}
            />
            <button
              type="submit"
              className={styles.appendBtn}
              disabled={saving || !note.trim()}
            >
              <Save size={12} />
              {saving ? 'Saving…' : 'Record lesson'}
            </button>
          </form>
          <div className={styles.memoryHint}>
            Memory is an append-only file at{' '}
            <code>{agent.path.replace(/\.md$/, '.memory.md')}</code>.
            Claude reads it whenever this agent is invoked so lessons accumulate.
          </div>
        </>
      ) : (
        <div>
          {isReadonly && (
            <div style={{
              padding: '8px 10px',
              marginBottom: 8,
              background: 'rgba(255,170,0,0.08)',
              borderLeft: '3px solid #ffaa00',
              color: '#ffaa00',
              fontSize: 11,
            }}>
              Plugin-bundled agent — read-only. Next plugin update would
              overwrite edits. Use <b>Clone to global</b> to get an
              editable copy under <code>~/.claude/agents/</code>.
            </div>
          )}
          <textarea
            className={styles.sourceBox}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            readOnly={isReadonly}
            spellCheck={false}
            style={{
              width: '100%',
              minHeight: 320,
              fontFamily: 'var(--font-mono, monospace)',
              fontSize: 11,
              background: 'rgba(0,0,0,0.25)',
              color: 'var(--ink, #e6f6ff)',
              border: '1px solid rgba(124,198,255,0.2)',
              borderRadius: 4,
              padding: 10,
              resize: 'vertical',
            }}
          />
          <div style={{
            display: 'flex', gap: 8, marginTop: 8,
            alignItems: 'center', flexWrap: 'wrap',
          }}>
            {!isReadonly && (
              <button
                type="button"
                onClick={handleSaveSource}
                disabled={!dirty || savingSource}
                style={btnStyle(dirty ? '#6ff2a0' : '#7cc6ff', !dirty || savingSource)}
              >
                <Save size={12} /> {savingSource ? 'Saving…' : 'Save source'}
              </button>
            )}
            {isReadonly && (
              <button
                type="button"
                onClick={handleCloneToGlobal}
                disabled={savingSource}
                style={btnStyle('#c488ff', savingSource)}
              >
                <Plus size={12} /> {savingSource ? 'Cloning…' : 'Clone to global'}
              </button>
            )}
            {lastSavedAt && (
              <span style={{ fontSize: 10, opacity: 0.6 }}>
                saved {Math.max(1, Math.round((Date.now() - lastSavedAt) / 1000))}s ago
              </span>
            )}
            {saveError && (
              <span style={{ fontSize: 10, color: '#ff7b7b' }}>{saveError}</span>
            )}
          </div>
        </div>
      )}
    </HudPanel>
  );
}

function btnStyle(color: string, disabled: boolean): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 12px',
    fontFamily: 'inherit',
    fontSize: 11,
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
    color: disabled ? '#7cc6ff' : '#0a0c12',
    background: disabled ? 'rgba(124,198,255,0.12)' : color,
    border: `1px solid ${color}66`,
    borderRadius: 4,
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.6 : 1,
  };
}

/* ============================================================
   Hooks tab — list of files in .claude/hooks/ with a click-to-preview.
   ============================================================ */

function HooksTab() {
  const [hooks, setHooks] = useState<FleetHook[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<FleetHook | null>(null);
  const [body, setBody] = useState('');

  useEffect(() => {
    void (async () => {
      setLoading(true);
      const list = await listFleetHooks();
      setHooks(list);
      setLoading(false);
    })();
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (!selected) return;
    void (async () => {
      const src = await readFleetFile(selected.path);
      if (!cancelled) setBody(src);
    })();
    return () => { cancelled = true; };
  }, [selected]);

  const byKind = useMemo(() => {
    const m = new Map<string, FleetHook[]>();
    for (const h of hooks) {
      const arr = m.get(h.kind) ?? [];
      arr.push(h);
      m.set(h.kind, arr);
    }
    return Array.from(m.entries()).sort();
  }, [hooks]);

  return (
    <div className={styles.hooksLayout}>
      <div className={styles.hooksLeft}>
        {loading ? (
          <HudPanel title="Loading" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" />}>
            <div className={styles.empty}>// scanning .claude/hooks…</div>
          </HudPanel>
        ) : hooks.length === 0 ? (
          <HudPanel title="No hooks" accent="#ffaa00" leading={<HudStatusLed color="#ffaa00" animate={false} />}>
            <div className={styles.empty}>// no files under .claude/hooks/</div>
          </HudPanel>
        ) : (
          byKind.map(([kind, list]) => (
            <HudPanel
              key={kind}
              title={kind.toUpperCase()}
              accent={kind === 'script' ? '#00f0ff' : kind === 'config' ? '#ffaa00' : '#7cc6ff'}
              leading={<HudStatusLed color={kind === 'script' ? '#00f0ff' : '#ffaa00'} animate={false} />}
              meta={<><Zap size={10} /> {list.length}</>}
            >
              <div className={styles.hookList}>
                {list.map((h) => (
                  <button
                    key={h.path}
                    type="button"
                    className={`${styles.hookRow} ${selected?.path === h.path ? styles.hookRowActive : ''}`}
                    onClick={() => setSelected(h)}
                  >
                    <span className={styles.hookName}>{h.name}</span>
                    <span className={styles.hookMeta}>
                      <HudChip color="#7cc6ff">{h.language}</HudChip>
                      <span className={styles.hookSize}>{formatSize(h.size_bytes)}</span>
                    </span>
                  </button>
                ))}
              </div>
            </HudPanel>
          ))
        )}
      </div>

      <div className={styles.hooksRight}>
        {selected ? (
          <HudPanel
            title={selected.name}
            accent="#00f0ff"
            leading={<HudStatusLed color="#00f0ff" />}
            meta={<HudChip color="#00f0ff">{selected.language}</HudChip>}
            footer={<code className={styles.detailPath}>{selected.path}</code>}
          >
            <pre className={styles.sourceBox}>{body || '// empty file'}</pre>
          </HudPanel>
        ) : (
          <HudPanel title="Preview" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" animate={false} />}>
            <div className={styles.detailPlaceholder}>
              <GitBranch size={28} />
              <span>Select a hook on the left to read it.</span>
            </div>
          </HudPanel>
        )}
      </div>
    </div>
  );
}

/* ============================================================
   Rules tab — groups docs by their top-level folder
   (common / python / golang / cpp / ...).
   ============================================================ */

function RulesTab() {
  const [rules, setRules] = useState<FleetRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<FleetRule | null>(null);
  const [body, setBody] = useState('');

  useEffect(() => {
    void (async () => {
      setLoading(true);
      setRules(await listFleetRules());
      setLoading(false);
    })();
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (!selected) return;
    void (async () => {
      const src = await readFleetFile(selected.path);
      if (!cancelled) setBody(src);
    })();
    return () => { cancelled = true; };
  }, [selected]);

  const byGroup = useMemo(() => {
    const m = new Map<string, FleetRule[]>();
    for (const r of rules) {
      const arr = m.get(r.group) ?? [];
      arr.push(r);
      m.set(r.group, arr);
    }
    return Array.from(m.entries()).sort((a, b) => {
      // keep 'common' first, then alphabetical
      if (a[0] === 'common') return -1;
      if (b[0] === 'common') return 1;
      return a[0].localeCompare(b[0]);
    });
  }, [rules]);

  return (
    <div className={styles.hooksLayout}>
      <div className={styles.hooksLeft}>
        {loading ? (
          <HudPanel title="Loading" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" />}>
            <div className={styles.empty}>// scanning .claude/rules…</div>
          </HudPanel>
        ) : rules.length === 0 ? (
          <HudPanel title="No rules" accent="#ffaa00" leading={<HudStatusLed color="#ffaa00" animate={false} />}>
            <div className={styles.empty}>// no files under .claude/rules/</div>
          </HudPanel>
        ) : (
          byGroup.map(([group, list]) => (
            <HudPanel
              key={group}
              title={group}
              accent={group === 'common' ? '#00f0ff' : '#6ff2a0'}
              leading={<HudStatusLed color="#6ff2a0" animate={false} />}
              meta={<><Scroll size={10} /> {list.length}</>}
            >
              <div className={styles.hookList}>
                {list.map((r) => (
                  <button
                    key={r.path}
                    type="button"
                    className={`${styles.hookRow} ${selected?.path === r.path ? styles.hookRowActive : ''}`}
                    onClick={() => setSelected(r)}
                  >
                    <span className={styles.hookName}>{r.name}</span>
                    <HudChip color={r.source === 'project' ? '#6ff2a0' : '#00f0ff'}>
                      {r.source}
                    </HudChip>
                  </button>
                ))}
              </div>
            </HudPanel>
          ))
        )}
      </div>

      <div className={styles.hooksRight}>
        {selected ? (
          <HudPanel
            title={selected.name}
            accent="#6ff2a0"
            leading={<HudStatusLed color="#6ff2a0" />}
            meta={<HudChip color={selected.source === 'project' ? '#6ff2a0' : '#00f0ff'}>{selected.source}</HudChip>}
            footer={<code className={styles.detailPath}>{selected.path}</code>}
          >
            <pre className={styles.sourceBox}>{body || '// empty'}</pre>
          </HudPanel>
        ) : (
          <HudPanel title="Preview" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" animate={false} />}>
            <div className={styles.detailPlaceholder}>
              <Scroll size={28} />
              <span>Select a rule on the left to read it.</span>
            </div>
          </HudPanel>
        )}
      </div>
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)}MB`;
}

/* ============================================================
   Page shell — summary strip + sub-tab switcher + the active tab.
   ============================================================ */

export default function AgentFleetPage() {
  const [tab, setTab] = useState<Tab>('agents');
  const [counts, setCounts] = useState({ agents: 0, hooks: 0, rules: 0 });
  const [busy, setBusy] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  // Bumping this key remounts the AgentsTab so newly-created agents
  // show up immediately without a hard refresh.
  const [agentsReloadKey, setAgentsReloadKey] = useState(0);

  const refresh = useCallback(async () => {
    setBusy(true);
    const [a, h, r] = await Promise.all([
      listFleetAgents(), listFleetHooks(), listFleetRules(),
    ]);
    setCounts({ agents: a.length, hooks: h.length, rules: r.length });
    setBusy(false);
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Agent Fleet · Claude Code Configuration"
        subtitle={`${counts.agents} agents · ${counts.hooks} hooks · ${counts.rules} rule docs on disk`}
        gaugeValue={counts.agents === 0 ? 0 : Math.min(counts.agents / 80, 1)}
        gaugeReadout={`${counts.agents}`}
        gaugeLabel="AGENTS"
        gaugeColor="#00f0ff"
        extra={
          <div className={styles.headerActions}>
            <button
              type="button"
              className={styles.refreshBtn}
              onClick={() => void refresh()}
              disabled={busy}
            >
              <RefreshCw size={13} className={busy ? styles.spin : undefined} />
              Refresh
            </button>
            <div className={styles.headerIcon}>
              <Layers size={22} style={{ color: '#00f0ff' }} />
            </div>
          </div>
        }
      />

      <div className={styles.tabRow}>
        {([
          ['agents', 'Agents', counts.agents, <Bot size={12} />],
          ['hooks', 'Hooks', counts.hooks, <Zap size={12} />],
          ['rules', 'Rules', counts.rules, <Scroll size={12} />],
        ] as const).map(([key, label, count, icon]) => (
          <button
            key={key}
            type="button"
            className={`${styles.tab} ${tab === key ? styles.tabActive : ''}`}
            onClick={() => setTab(key as Tab)}
          >
            {icon} {label}
            <span className={styles.tabCount}>{count}</span>
          </button>
        ))}
        <button
          type="button"
          className={styles.newAgentBtn}
          onClick={() => setCreateOpen(true)}
        >
          <Plus size={13} /> New agent
        </button>
      </div>

      {tab === 'agents' && <AgentsTab key={agentsReloadKey} />}
      {tab === 'hooks' && <HooksTab />}
      {tab === 'rules' && <RulesTab />}

      {createOpen && (
        <CreateAgentModal
          onClose={() => setCreateOpen(false)}
          onCreated={() => {
            setCreateOpen(false);
            setAgentsReloadKey((k) => k + 1);
            void refresh();
          }}
        />
      )}
    </div>
  );
}

/* ---- New agent modal ---- */
function CreateAgentModal({ onClose, onCreated }: {
  readonly onClose: () => void;
  readonly onCreated: (a: FleetAgent) => void;
}) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [category, setCategory] = useState('');
  const [source, setSource] = useState<'global' | 'project'>('global');
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const canSubmit = name.trim().length > 0 && description.trim().length > 0 && !busy;

  const submit = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true);
    setErr(null);
    try {
      const created = await createFleetAgent({
        name: name.trim(), description: description.trim(),
        category: category.trim(), source, body: body.trim(),
      });
      if (created) onCreated(created);
      else setErr('create failed (no response)');
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'create failed');
    } finally {
      setBusy(false);
    }
  }, [name, description, category, source, body, canSubmit, onCreated]);

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 100,
      background: 'rgba(0,0,0,0.65)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      padding: 20,
    }} onClick={onClose}>
      <form
        onSubmit={submit}
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(560px, 100%)',
          maxHeight: '90vh', overflow: 'auto',
          background: 'var(--surface, #0d111b)',
          border: '1px solid rgba(0,240,255,0.3)',
          borderRadius: 8, padding: 20,
          fontFamily: 'inherit',
          display: 'grid', gap: 10,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Plus size={16} color="#00f0ff" />
          <h3 style={{ margin: 0, fontSize: 14, color: '#00f0ff' }}>New agent</h3>
        </div>
        <label style={labelStyle}>Name
          <input type="text" value={name} onChange={(e) => setName(e.target.value)}
                 placeholder="my-new-agent" autoFocus style={inputStyle} />
        </label>
        <label style={labelStyle}>Description
          <input type="text" value={description} onChange={(e) => setDescription(e.target.value)}
                 placeholder="What does this agent do?" style={inputStyle} />
        </label>
        <label style={labelStyle}>Category (optional)
          <input type="text" value={category} onChange={(e) => setCategory(e.target.value)}
                 placeholder="Productivity | Debug | Review …" style={inputStyle} />
        </label>
        <label style={labelStyle}>Destination
          <select value={source} onChange={(e) => setSource(e.target.value as 'global' | 'project')}
                  style={inputStyle}>
            <option value="global">Global · ~/.claude/agents</option>
            <option value="project">Project · .claude/agents</option>
          </select>
        </label>
        <label style={labelStyle}>Body (markdown — leave empty for a template)
          <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={8}
                    placeholder="# My agent&#10;&#10;Agent instructions go here…"
                    style={{ ...inputStyle, fontFamily: 'var(--font-mono, monospace)', resize: 'vertical' }} />
        </label>
        {err && <div style={{ color: '#ff7b7b', fontSize: 11 }}>{err}</div>}
        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <button type="button" onClick={onClose}
                  style={btnStyle('#7cc6ff', false)}>Cancel</button>
          <button type="submit" disabled={!canSubmit}
                  style={btnStyle('#6ff2a0', !canSubmit)}>
            {busy ? 'Creating…' : 'Create agent'}
          </button>
        </div>
      </form>
    </div>
  );
}

const labelStyle: React.CSSProperties = {
  display: 'grid', gap: 4, fontSize: 11, color: 'var(--ink-dim, #7cc6ff)',
};
const inputStyle: React.CSSProperties = {
  padding: '6px 8px',
  background: 'rgba(0,0,0,0.3)',
  color: 'var(--ink, #e6f6ff)',
  border: '1px solid rgba(124,198,255,0.25)',
  borderRadius: 4, fontFamily: 'inherit', fontSize: 12,
};
