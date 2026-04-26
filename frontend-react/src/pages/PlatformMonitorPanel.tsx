/* ============================================================
   PlatformMonitorPanel — live reachability + latency for rain's
   BSS middleware (Snowflake), Station, Athena, raingo, engineering
   docs. Polls /api/v1/platforms/health every 30s.
   ============================================================ */

import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Radio, ExternalLink, BookOpen, ArrowRight } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { listPlatformHealth, type PlatformStatus, type PlatformState } from '../api/platforms';

const STATE_COLOR: Record<PlatformState, string> = {
  up: '#6ff2a0',
  degraded: '#ffaa00',
  down: '#ff7b7b',
  unknown: '#ffb86b',
};

const STATE_LABEL: Record<PlatformState, string> = {
  up: 'UP',
  degraded: 'DEGRADED',
  down: 'DOWN',
  unknown: '—',
};

function PlatformRow({ p }: { readonly p: PlatformStatus }) {
  const color = STATE_COLOR[p.state];
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '1fr auto',
        gap: 6,
        alignItems: 'center',
        padding: '6px 8px',
        borderLeft: `2px solid ${color}55`,
        fontSize: 11,
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <HudStatusLed color={color} animate={p.state === 'up'} />
          <span style={{ fontFamily: 'var(--font-mono, monospace)' }}>{p.name}</span>
          <HudChip color={color}>
            {STATE_LABEL[p.state]}
            {p.state === 'up' && p.latency_ms > 0 ? ` · ${p.latency_ms}ms` : ''}
          </HudChip>
          {p.http_code > 0 && (
            <span style={{ fontSize: 9, opacity: 0.7 }}>HTTP {p.http_code}</span>
          )}
        </div>
        <div
          style={{
            fontSize: 10,
            opacity: 0.75,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {p.error ? p.error : p.url}
        </div>
      </div>
      <div style={{ display: 'flex', gap: 4 }}>
        {p.docs_url && (
          <a
            href={p.docs_url}
            target="_blank"
            rel="noreferrer"
            title="Open Swagger / docs"
            style={iconBtn('#00f0ff')}
          >
            <BookOpen size={11} />
          </a>
        )}
        <a
          href={p.url}
          target="_blank"
          rel="noreferrer"
          title="Open endpoint"
          style={iconBtn('#7cc6ff')}
        >
          <ExternalLink size={11} />
        </a>
      </div>
    </div>
  );
}

function iconBtn(color: string): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 22,
    height: 22,
    color,
    background: 'transparent',
    border: `1px solid ${color}66`,
    borderRadius: 4,
    textDecoration: 'none',
  };
}

export default function PlatformMonitorPanel() {
  const [rows, setRows] = useState<PlatformStatus[]>([]);

  const refresh = useCallback(async () => {
    setRows(await listPlatformHealth());
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 30_000);
    return () => clearInterval(t);
  }, [refresh]);

  // Dashboard tile shows ONLY the top-criticality services so the
  // card stays compact. The full rain Service tab renders all 15
  // including standards. Fallback filter keeps this working if the
  // backend ever returns mixed data without criticality set.
  const shown = rows.filter((r) => r.criticality === 'top' || !r.criticality);
  const total = rows.length || shown.length;
  const upCount = shown.filter((r) => r.state === 'up').length;
  const overallColor =
    shown.length === 0
      ? '#7cc6ff'
      : shown.some((r) => r.state === 'down')
      ? '#ff7b7b'
      : shown.some((r) => r.state === 'degraded')
      ? '#ffaa00'
      : '#6ff2a0';

  // Group by `group` so BSS / Customer / Dev render as separate stacks.
  const byGroup = new Map<string, PlatformStatus[]>();
  for (const r of shown) {
    const arr = byGroup.get(r.group) ?? [];
    arr.push(r);
    byGroup.set(r.group, arr);
  }

  return (
    <HudPanel
      icon={<Radio size={12} />}
      title="Platform Monitor"
      subtitle={`top ${shown.length} of ${total} services · rain Service for full view`}
      leading={<HudStatusLed color={overallColor} animate={upCount > 0} />}
      meta={<>{upCount}/{shown.length} up</>}
    >
      {shown.length === 0 && (
        <div style={{ fontSize: 11, opacity: 0.7, padding: '6px 0' }}>
          Loading platform health…
        </div>
      )}
      {Array.from(byGroup.entries()).map(([group, items]) => (
        <div key={group} style={{ marginBottom: 8 }}>
          <div
            style={{
              fontSize: 9,
              opacity: 0.7,
              textTransform: 'uppercase',
              letterSpacing: '0.08em',
              padding: '4px 6px',
            }}
          >
            {group}
          </div>
          <div style={{ display: 'grid', gap: 4 }}>
            {items.map((p) => (
              <PlatformRow key={p.id} p={p} />
            ))}
          </div>
        </div>
      ))}
      <Link
        to="/service"
        style={{
          display: 'inline-flex', alignItems: 'center', gap: 5,
          fontSize: 10, color: '#00f0ff', textDecoration: 'none',
          padding: '6px 8px', marginTop: 4,
          borderTop: '1px solid rgba(0,240,255,0.18)',
          letterSpacing: '0.08em', textTransform: 'uppercase',
        }}
      >
        open rain Service for full view <ArrowRight size={11} />
      </Link>
    </HudPanel>
  );
}
