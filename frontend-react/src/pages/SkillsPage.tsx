/* ============================================================
   SkillsPage — Skill + MCP catalogue using HUD primitives.
   Category → HudPanel grouping each bucket of skills.
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Sparkles, Server, FileText, Package, Save, Plus } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  listMCPHealth,
  listMCPServers,
  listSkills,
  readSkillFile,
  writeSkillFile,
  type MCPHealth,
  type MCPHealthState,
  type MCPServer,
  type Skill,
  type SkillSource,
} from '../api/skills';
import hudStyles from '../theme/hud.module.css';
import styles from './SkillsPage.module.css';

type Tab = 'skills' | 'mcp';
type SourceFilter = 'all' | SkillSource;

const SOURCE_LABELS: Record<SourceFilter, string> = {
  all: 'All',
  global: 'Global',
  project: 'Project',
  plugin: 'Plugin',
};

/* source → colour used both for the row border and summary-strip segment. */
const SOURCE_COLOR: Record<SkillSource, string> = {
  global:  '#00f0ff',
  project: '#6ff2a0',
  plugin:  '#ff7de0',
};

function SkillRow({ skill, selected, onSelect }: {
  readonly skill: Skill;
  readonly selected: boolean;
  readonly onSelect: (s: Skill) => void;
}) {
  const color = SOURCE_COLOR[skill.source];
  return (
    <button
      type="button"
      onClick={() => onSelect(skill)}
      className={styles.row}
      style={{
        borderLeftColor: `${color}55`,
        textAlign: 'left',
        background: selected ? 'rgba(0,240,255,0.07)' : 'transparent',
        border: selected ? '1px solid rgba(0,240,255,0.3)' : '1px solid transparent',
        cursor: 'pointer',
        width: '100%',
        fontFamily: 'inherit',
        color: 'inherit',
      }}
    >
      <div className={styles.rowHead}>
        <span className={styles.rowName}>{skill.name}</span>
        <HudChip color={color}>{skill.source}</HudChip>
      </div>
      <div className={styles.rowDesc}>
        {skill.description || '// no description provided'}
      </div>
      {skill.plugin && (
        <div className={styles.rowFooter}>
          <Package size={9} /> {skill.plugin}
        </div>
      )}
    </button>
  );
}

/* ---- Detail pane for the selected skill ---- */
function SkillDetail({ skill, onSaved }: {
  readonly skill: Skill;
  readonly onSaved: () => void;
}) {
  const [body, setBody] = useState('');
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const isReadonly = skill.source === 'plugin';
  const dirty = draft !== body;

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const text = await readSkillFile(skill.path);
      if (cancelled) return;
      setBody(text);
      setDraft(text);
      setErr(null);
    })();
    return () => { cancelled = true; };
  }, [skill.path]);

  const onSave = useCallback(async () => {
    if (!dirty || isReadonly) return;
    setBusy(true); setErr(null);
    try {
      const updated = await writeSkillFile(skill.path, draft);
      setBody(updated); setDraft(updated);
      setSavedAt(Date.now());
      onSaved();
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'save failed');
    } finally {
      setBusy(false);
    }
  }, [skill.path, draft, dirty, isReadonly, onSaved]);

  const color = SOURCE_COLOR[skill.source];
  return (
    <HudPanel
      title={skill.name}
      accent={color}
      leading={<HudStatusLed color={color} />}
      meta={<HudChip color={color}>{skill.source}</HudChip>}
    >
      <div style={{ fontSize: 10, opacity: 0.7, marginBottom: 8, fontFamily: 'var(--font-mono, monospace)' }}>
        {skill.path}
      </div>
      {isReadonly && (
        <div style={{
          padding: '8px 10px',
          marginBottom: 8,
          background: 'rgba(255,170,0,0.08)',
          borderLeft: '3px solid #ffaa00',
          color: '#ffaa00',
          fontSize: 11,
        }}>
          Plugin-bundled skill — read-only. Next plugin update would
          overwrite edits. Copy the content into a new skill under
          <code> ~/.claude/skills/</code> if you need to modify it.
        </div>
      )}
      <textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        readOnly={isReadonly}
        spellCheck={false}
        style={{
          width: '100%',
          minHeight: 360,
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
      {!isReadonly && (
        <div style={{ display: 'flex', gap: 8, marginTop: 8, alignItems: 'center' }}>
          <button
            type="button"
            onClick={onSave}
            disabled={!dirty || busy}
            style={{
              display: 'inline-flex', alignItems: 'center', gap: 4,
              padding: '4px 12px', fontSize: 11, fontFamily: 'inherit',
              textTransform: 'uppercase', letterSpacing: '0.06em',
              color: dirty ? '#0a0c12' : '#7cc6ff',
              background: dirty ? '#6ff2a0' : 'rgba(124,198,255,0.1)',
              border: '1px solid rgba(111,242,160,0.4)',
              borderRadius: 4,
              cursor: dirty && !busy ? 'pointer' : 'not-allowed',
              opacity: dirty ? 1 : 0.6,
            }}
          >
            <Save size={12} /> {busy ? 'Saving…' : 'Save'}
          </button>
          {savedAt && (
            <span style={{ fontSize: 10, opacity: 0.6 }}>
              saved {Math.max(1, Math.round((Date.now() - savedAt) / 1000))}s ago
            </span>
          )}
          {err && <span style={{ fontSize: 10, color: '#ff7b7b' }}>{err}</span>}
        </div>
      )}
    </HudPanel>
  );
}

const HEALTH_COLOR: Record<MCPHealthState, string> = {
  up:      '#6ff2a0',
  down:    '#ff7b7b',
  local:   '#7cc6ff',
  unknown: '#ffb86b',
};

const HEALTH_LABEL: Record<MCPHealthState, string> = {
  up:      'UP',
  down:    'DOWN',
  local:   'LOCAL',
  unknown: '—',
};

function MCPRow({ server, health }: { readonly server: MCPServer; readonly health?: MCPHealth }) {
  const color = server.enabled ? '#6ff2a0' : '#7cc6ff';
  const detail = server.url || server.command || 'n/a';
  const state: MCPHealthState = health?.status ?? 'unknown';
  const healthColor = HEALTH_COLOR[state];
  return (
    <div className={styles.row} style={{ borderLeftColor: `${color}55` }}>
      <div className={styles.rowHead}>
        <span className={styles.rowName}>{server.name}</span>
        <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
          <HudStatusLed color={healthColor} />
          <HudChip color={healthColor}>
            {HEALTH_LABEL[state]}
            {state === 'up' && typeof health?.latency_ms === 'number' && health.latency_ms > 0
              ? ` · ${health.latency_ms}ms`
              : ''}
          </HudChip>
          <HudChip color={color}>{server.enabled ? 'ON' : 'OFF'}</HudChip>
          <HudChip color={SOURCE_COLOR[server.source]}>{server.source}</HudChip>
        </div>
      </div>
      {server.comment && (
        <div className={styles.rowDesc}>{server.comment}</div>
      )}
      <div className={styles.rowFooter}>
        <strong style={{ color: 'var(--neon-cyan)' }}>{server.transport || 'unknown'}</strong>
        {' · '}
        {detail.length > 60 ? detail.slice(0, 57) + '…' : detail}
        {health?.error ? ` · err: ${health.error.slice(0, 60)}` : ''}
      </div>
    </div>
  );
}

export default function SkillsPage() {
  const [tab, setTab] = useState<Tab>('skills');
  const [skills, setSkills] = useState<Skill[]>([]);
  const [selectedSkill, setSelectedSkill] = useState<Skill | null>(null);
  const [mcp, setMcp] = useState<MCPServer[]>([]);
  const [mcpHealth, setMcpHealth] = useState<Map<string, MCPHealth>>(new Map());
  const [query, setQuery] = useState('');
  const [source, setSource] = useState<SourceFilter>('all');
  const [category, setCategory] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    Promise.all([listSkills(), listMCPServers()]).then(([s, m]) => {
      if (cancelled) return;
      setSkills(s);
      setMcp(m);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  // Poll MCP health every 30s while the MCP tab is visible. The backend
  // refreshes every 60s; polling more frequently just picks up the
  // latest cached snapshot without blocking.
  useEffect(() => {
    if (tab !== 'mcp') return;
    let cancelled = false;
    const refresh = () => {
      listMCPHealth().then((rows) => {
        if (cancelled) return;
        setMcpHealth(new Map(rows.map((r) => [r.name, r])));
      });
    };
    refresh();
    const t = setInterval(refresh, 30_000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [tab]);

  const filteredSkills = useMemo(() => {
    const q = query.trim().toLowerCase();
    return skills.filter((s) => {
      if (source !== 'all' && s.source !== source) return false;
      if (category && s.category !== category) return false;
      if (q) {
        const hay = `${s.name} ${s.description}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [skills, query, source, category]);

  const categoryCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const s of skills) {
      if (source !== 'all' && s.source !== source) continue;
      const q = query.trim().toLowerCase();
      if (q) {
        const hay = `${s.name} ${s.description}`.toLowerCase();
        if (!hay.includes(q)) continue;
      }
      counts.set(s.category, (counts.get(s.category) ?? 0) + 1);
    }
    return Array.from(counts.entries()).sort((a, b) => {
      if (b[1] !== a[1]) return b[1] - a[1];
      return a[0].localeCompare(b[0]);
    });
  }, [skills, source, query]);

  const skillsByCategory = useMemo(() => {
    const m = new Map<string, Skill[]>();
    for (const s of filteredSkills) {
      const arr = m.get(s.category) ?? [];
      arr.push(s);
      m.set(s.category, arr);
    }
    return Array.from(m.entries()).sort((a, b) => a[0].localeCompare(b[0]));
  }, [filteredSkills]);

  const sourceCounts = useMemo(() => {
    const m: Record<string, number> = { global: 0, project: 0, plugin: 0 };
    for (const s of skills) m[s.source] = (m[s.source] ?? 0) + 1;
    return m;
  }, [skills]);

  const activeColor = tab === 'skills' ? '#00f0ff' : '#ff7de0';
  const title = tab === 'skills' ? 'Intelligence Catalogue' : 'MCP Servers';
  const subtitle = tab === 'skills'
    ? `${skills.length} skills · ${categoryCounts.length} categories · ${sourceCounts.global} global · ${sourceCounts.project} project · ${sourceCounts.plugin} plugin`
    : `${mcp.length} server${mcp.length === 1 ? '' : 's'} registered`;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title={title}
        subtitle={subtitle}
        gaugeValue={tab === 'skills'
          ? (skills.length === 0 ? 0 : Math.min(skills.length / 1000, 1))
          : (mcp.length === 0 ? 0 : Math.min(mcp.length / 10, 1))}
        gaugeReadout={tab === 'skills' ? `${skills.length}` : `${mcp.length}`}
        gaugeLabel={tab === 'skills' ? 'SKILLS' : 'MCP'}
        gaugeColor={activeColor}
        segments={tab === 'skills'
          ? [
            { label: 'Global',  value: sourceCounts.global,  color: SOURCE_COLOR.global },
            { label: 'Project', value: sourceCounts.project, color: SOURCE_COLOR.project },
            { label: 'Plugin',  value: sourceCounts.plugin,  color: SOURCE_COLOR.plugin },
          ]
          : undefined}
        extra={
          <div className={styles.tabs}>
            <button
              type="button"
              className={`${styles.tab} ${tab === 'skills' ? styles.tabActive : ''}`}
              onClick={() => setTab('skills')}
            >
              <Sparkles size={11} /> Skills
            </button>
            <button
              type="button"
              className={`${styles.tab} ${tab === 'mcp' ? styles.tabActive : ''}`}
              onClick={() => setTab('mcp')}
            >
              <Server size={11} /> MCP
            </button>
          </div>
        }
      />

      {tab === 'skills' ? (
        <>
          <div className={styles.controls}>
            <input
              type="search"
              className={styles.search}
              placeholder="// search skills..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <div className={styles.sourceGroup}>
              {(Object.keys(SOURCE_LABELS) as SourceFilter[]).map((key) => (
                <button
                  key={key}
                  type="button"
                  className={`${styles.sourceBtn} ${source === key ? styles.sourceBtnActive : ''}`}
                  onClick={() => setSource(key)}
                >
                  {SOURCE_LABELS[key]}
                </button>
              ))}
            </div>
          </div>

          <div className={styles.chipRow}>
            <button
              type="button"
              className={`${styles.chip} ${category === null ? styles.chipActive : ''}`}
              onClick={() => setCategory(null)}
            >
              All
            </button>
            {categoryCounts.map(([cat, count]) => (
              <button
                key={cat}
                type="button"
                className={`${styles.chip} ${category === cat ? styles.chipActive : ''}`}
                onClick={() => setCategory(category === cat ? null : cat)}
              >
                {cat}
                <span className={styles.chipCount}>{count}</span>
              </button>
            ))}
          </div>

          {loading ? (
            <HudPanel title="Loading" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" />}>
              <div className={styles.empty}>// loading skill catalogue…</div>
            </HudPanel>
          ) : skillsByCategory.length === 0 ? (
            <HudPanel title="No Results" accent="#ffaa00" leading={<HudStatusLed color="#ffaa00" animate={false} />}>
              <div className={styles.empty}>// no skills match your filters</div>
            </HudPanel>
          ) : (
            <div style={selectedSkill ? {
              display: 'grid',
              gridTemplateColumns: '1fr 1fr',
              gap: 12,
              alignItems: 'start',
            } : undefined}>
            <div className={styles.categoryGrid}>
              {skillsByCategory.map(([cat, rows]) => {
                // Cap rows rendered per category so 1000+ skills don't tank
                // the DOM. The full set stays in memory; typing in the
                // search box narrows it below the cap instantly.
                const MAX_PER_CATEGORY = 40;
                const visible = rows.slice(0, MAX_PER_CATEGORY);
                const hidden = rows.length - visible.length;
                return (
                  <HudPanel
                    key={cat}
                    title={cat}
                    accent={activeColor}
                    leading={<HudStatusLed color="#6ff2a0" animate={false} />}
                    meta={<><FileText size={10} /> {rows.length}</>}
                  >
                    <div className={styles.rowList}>
                      {visible.map((s) => (
                        <SkillRow
                          key={`${s.source}:${s.plugin ?? ''}:${s.name}`}
                          skill={s}
                          selected={selectedSkill?.path === s.path}
                          onSelect={setSelectedSkill}
                        />
                      ))}
                      {hidden > 0 && (
                        <div
                          style={{
                            padding: '6px 8px',
                            fontSize: 10,
                            opacity: 0.7,
                            fontFamily: 'var(--font-mono, monospace)',
                          }}
                        >
                          + {hidden} more — narrow with search or a source filter
                        </div>
                      )}
                    </div>
                  </HudPanel>
                );
              })}
            </div>
            {selectedSkill && (
              <div style={{ position: 'sticky', top: 12 }}>
                <button
                  type="button"
                  onClick={() => setSelectedSkill(null)}
                  style={{
                    padding: '3px 8px', fontSize: 10, marginBottom: 8,
                    color: '#7cc6ff', background: 'transparent',
                    border: '1px solid rgba(124,198,255,0.3)', borderRadius: 4,
                    cursor: 'pointer', fontFamily: 'inherit',
                  }}
                >
                  ✕ close
                </button>
                <SkillDetail skill={selectedSkill} onSaved={() => { /* UI already in sync */ }} />
              </div>
            )}
            </div>
          )}
        </>
      ) : loading ? (
        <HudPanel title="Loading" accent="#ff7de0" leading={<HudStatusLed color="#ff7de0" />}>
          <div className={styles.empty}>// loading MCP servers…</div>
        </HudPanel>
      ) : mcp.length === 0 ? (
        <HudPanel
          title="No MCP Servers"
          accent="#7cc6ff"
          leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        >
          <div className={styles.empty}>
            // no servers found. Add them to <code>.mcp.json</code> at project root
            or <code>~/.claude/mcp.json</code> for global.
          </div>
        </HudPanel>
      ) : (
        (() => {
          // Group servers by their _group field; ungrouped ones fall into "Other".
          const byGroup = new Map<string, MCPServer[]>();
          for (const s of mcp) {
            const g = s.group || 'Other';
            const arr = byGroup.get(g) ?? [];
            arr.push(s);
            byGroup.set(g, arr);
          }
          // Stable order: put the big buckets first, then alphabetical.
          const sortedGroups = Array.from(byGroup.entries()).sort((a, b) => {
            if (b[1].length !== a[1].length) return b[1].length - a[1].length;
            return a[0].localeCompare(b[0]);
          });
          return (
            <div className={styles.categoryGrid}>
              {sortedGroups.map(([group, servers]) => {
                const enabledCount = servers.filter((s) => s.enabled).length;
                return (
                  <HudPanel
                    key={group}
                    title={group}
                    accent="#ff7de0"
                    leading={<HudStatusLed color={enabledCount > 0 ? '#6ff2a0' : '#7cc6ff'} animate={enabledCount > 0} />}
                    meta={<><Server size={10} /> {enabledCount}/{servers.length}</>}
                  >
                    <div className={styles.rowList}>
                      {servers.map((s) => (
                        <MCPRow
                          key={`${s.source}:${s.name}`}
                          server={s}
                          health={mcpHealth.get(s.name)}
                        />
                      ))}
                    </div>
                  </HudPanel>
                );
              })}
            </div>
          );
        })()
      )}
    </div>
  );
}
