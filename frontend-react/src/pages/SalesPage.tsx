/* ============================================================
   rain Sales — live dashboard (rainOne + Loop).
   Reads a cached snapshot from the backend poller — zero
   user-driven Axiom load. Inner tabs switch between the two
   product lines. Styled to echo the tv-final aesthetic (big
   KPI tiles + trend line + MTD gauges) using the house HUD
   palette + components so it lives natively alongside the
   rest of Command Centre.
   ============================================================ */

import { useCallback, useEffect, useState } from 'react';
import {
  TrendingUp, Wifi, Radio, Phone, Store,
  ArrowUpRight, ArrowDownRight, Truck, Package, CheckCircle2, XCircle,
  CreditCard, Headphones, AlertCircle,
} from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  getSalesSnapshot,
  refreshSalesSnapshot,
  CHANNEL_COLOURS,
  type SalesSnapshot,
  type ProductSnapshot,
  type TrendPoint,
  type MTDProgress,
  type CallCentreTrendPoint,
  type PaymentStatusBucket,
} from '../api/sales';
import hudStyles from '../theme/hud.module.css';

type Tab = 'rainone' | 'loop';

const TAB_META: Record<Tab, { label: string; accent: string }> = {
  rainone: { label: 'rainOne',  accent: '#00f0ff' },
  loop:    { label: 'Loop',     accent: '#6ff2a0' },
};

export default function SalesPage() {
  const [tab, setTab] = useState<Tab>(() => (localStorage.getItem('salesTab') as Tab) || 'rainone');
  const [snap, setSnap] = useState<SalesSnapshot | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const refresh = useCallback(async () => {
    const s = await getSalesSnapshot();
    setSnap(s);
    setLoaded(true);
  }, []);

  // Backend now refreshes its snapshot every 15 min (was 3 min —
  // dropped to reduce load on the Axiom prod primary). The frontend
  // re-reads the cached snapshot once a minute so the "last poll"
  // timestamp ticks over without hammering the backend.
  useEffect(() => {
    void refresh();
    const t = window.setInterval(() => { void refresh(); }, 60_000);
    return () => window.clearInterval(t);
  }, [refresh]);

  // Manual refresh — triggers a fresh poll on the backend. Backend
  // collapses concurrent refresh calls to one in-flight query, so
  // rapid clicks are safe.
  const forceRefresh = useCallback(async () => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      const s = await refreshSalesSnapshot();
      if (s) setSnap(s);
    } finally {
      setRefreshing(false);
    }
  }, [refreshing]);

  const switchTab = useCallback((t: Tab) => {
    setTab(t);
    localStorage.setItem('salesTab', t);
  }, []);

  const product: ProductSnapshot | null = snap ? snap[tab] : null;
  const asOf = snap?.as_of ? new Date(snap.as_of) : null;
  const fresh = asOf ? (Date.now() - asOf.getTime()) / 1000 < 600 : false;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title={`rain Sales · ${TAB_META[tab].label}`}
        subtitle={
          loaded && asOf
            ? `last poll ${asOf.toLocaleTimeString('en-ZA', { hour: '2-digit', minute: '2-digit', second: '2-digit' })} SAST · ${snap?.poll_latency_ms}ms · ${snap?.poll_errors ?? 0} errors`
            : 'loading snapshot…'
        }
        gaugeValue={product ? (product.mtd_sales_count.pct / 100) : 0}
        gaugeReadout={product ? `${product.mtd_sales_count.pct.toFixed(2)}%` : '—'}
        gaugeLabel="MTD VS BUDGET"
        gaugeColor={TAB_META[tab].accent}
        segments={
          product
            ? [
                { label: 'web',      value: product.sales_count.web,        color: CHANNEL_COLOURS.web },
                { label: 'call',     value: product.sales_count.call_centre, color: CHANNEL_COLOURS.call_centre },
                { label: 'retail',   value: product.sales_count.retail,      color: CHANNEL_COLOURS.retail },
              ]
            : []
        }
        extra={
          <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
            <TabSwitch tab={tab} onChange={switchTab} />
            <button
              type="button"
              onClick={() => { void forceRefresh(); }}
              disabled={refreshing}
              title="Force a fresh poll against Axiom (backend collapses concurrent calls)"
              style={{
                padding: '4px 10px', fontSize: 10, fontFamily: 'inherit',
                textTransform: 'uppercase', letterSpacing: '0.08em',
                color: '#7cc6ff', background: 'transparent',
                border: '1px solid #7cc6ff55', borderRadius: 4,
                cursor: refreshing ? 'wait' : 'pointer',
                opacity: refreshing ? 0.5 : 1,
              }}
            >
              {refreshing ? 'polling…' : 'refresh'}
            </button>
            <HudStatusLed color={fresh ? '#6ff2a0' : '#ffaa00'} animate={!fresh} />
          </div>
        }
      />

      {!loaded ? (
        <HudPanel title="Loading" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" />}>
          <div style={{ padding: 12, fontSize: 11, opacity: 0.7 }}>// waiting for snapshot…</div>
        </HudPanel>
      ) : !product ? (
        <HudPanel title="No data" accent="#ff7b7b" leading={<HudStatusLed color="#ff7b7b" />}>
          <div style={{ padding: 12, fontSize: 11 }}>Snapshot endpoint returned empty. Check backend logs.</div>
        </HudPanel>
      ) : (product.errors && product.errors.length > 0 && product.sales_count.total === 0) ? (
        /* Poll ran but every sub-query failed — usually a network
           block (Axiom unreachable) or expired credentials. Show
           the underlying error so the operator can act, instead
           of staring at zero tiles and wondering why. */
        <HudPanel
          title={`Sales poll failed · ${product.errors.length} errors`}
          accent="#ff7b7b"
          leading={<HudStatusLed color="#ff7b7b" animate />}
        >
          <div style={{ padding: 12, fontSize: 11 }}>
            <div style={{ color: '#ff7b7b', marginBottom: 6 }}>
              The backend reached Axiom but every sub-query errored. Common causes:
            </div>
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 10.5, opacity: 0.85 }}>
              <li>VPN / Cloudflare WARP dropped (Axiom prod is internal-only)</li>
              <li>Windows Defender Firewall blocking outbound TCP</li>
              <li>Axiom credentials expired</li>
            </ul>
            <div style={{ marginTop: 8, fontSize: 10, opacity: 0.7, fontFamily: 'var(--font-mono, monospace)' }}>
              first error: {product.errors[0].error.slice(0, 200)}
            </div>
          </div>
        </HudPanel>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <ChannelRow product={product} />
          <TrendAndMTDRow product={product} accent={TAB_META[tab].accent} />
          <OpsRow product={product} accent={TAB_META[tab].accent} />
          <CallCentreRow product={product} accent={TAB_META[tab].accent} />
          {product.errors && product.errors.length > 0 && (
            <HudPanel
              title={`Partial data · ${product.errors.length} source(s) errored`}
              accent="#ffaa00"
              leading={<HudStatusLed color="#ffaa00" />}
            >
              <div style={{ padding: '6px 10px', fontSize: 10.5 }}>
                {product.errors.map((e) => (
                  <div key={e.source} style={{ marginBottom: 4, opacity: 0.85 }}>
                    <span style={{ color: '#ffaa00' }}>{e.source}</span> — {e.error}
                  </div>
                ))}
              </div>
            </HudPanel>
          )}
        </div>
      )}
    </div>
  );
}

/* ---- Tab switcher (rainOne · Loop) ---- */
function TabSwitch({ tab, onChange }: { readonly tab: Tab; readonly onChange: (t: Tab) => void }) {
  const pill = (t: Tab, icon: React.ReactNode) => {
    const active = tab === t;
    const colour = TAB_META[t].accent;
    return (
      <button
        type="button"
        key={t}
        onClick={() => onChange(t)}
        style={{
          display: 'inline-flex', alignItems: 'center', gap: 5,
          padding: '4px 10px', fontSize: 10, fontFamily: 'inherit',
          textTransform: 'uppercase', letterSpacing: '0.08em',
          color: active ? '#0a0c12' : colour,
          background: active ? colour : 'transparent',
          border: `1px solid ${colour}${active ? '' : '55'}`,
          borderRadius: 4, cursor: 'pointer',
        }}
      >
        {icon} {TAB_META[t].label}
      </button>
    );
  };
  return (
    <div style={{ display: 'inline-flex', gap: 4 }}>
      {pill('rainone', <Radio size={11} />)}
      {pill('loop', <Wifi size={11} />)}
    </div>
  );
}

/* ---- Row 1: 4 channel tiles — Total / Web / Call Centre / Retail ---- */
function ChannelRow({ product }: { readonly product: ProductSnapshot }) {
  // Build the per-channel sparkline from the hourly trend series we
  // already have. For v1 we show today's cumulative total on every
  // tile — the shape is the same for the overall line and visually
  // matches tv-final. Per-channel splits require a trend-by-channel
  // SQL that we haven't wired yet.
  const spark = (product.trend ?? []).map((p) => p.today);
  const tiles: Array<{
    label: string; value: number; yesterday: number;
    colour: string; icon: React.ReactNode;
  }> = [
    { label: 'Total Sales', value: product.sales_count.total,       yesterday: product.yesterday_sales_count?.total ?? 0,       colour: CHANNEL_COLOURS.total,       icon: <TrendingUp size={14} /> },
    { label: 'Web',         value: product.sales_count.web,         yesterday: product.yesterday_sales_count?.web ?? 0,         colour: CHANNEL_COLOURS.web,         icon: <Wifi size={14} /> },
    { label: 'Call Centre', value: product.sales_count.call_centre, yesterday: product.yesterday_sales_count?.call_centre ?? 0, colour: CHANNEL_COLOURS.call_centre, icon: <Phone size={14} /> },
    { label: 'Retail',      value: product.sales_count.retail,      yesterday: product.yesterday_sales_count?.retail ?? 0,      colour: CHANNEL_COLOURS.retail,      icon: <Store size={14} /> },
  ];
  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
      gap: 12,
    }}>
      {tiles.map((t) => <KpiTile key={t.label} {...t} sparkline={spark} />)}
    </div>
  );
}

function KpiTile({
  label, value, yesterday, colour, icon, sparkline,
}: {
  readonly label: string;
  readonly value: number;
  readonly yesterday: number;
  readonly colour: string;
  readonly icon: React.ReactNode;
  readonly sparkline: number[];
}) {
  const delta = yesterday > 0 ? ((value - yesterday) / yesterday) * 100 : 0;
  const up = delta >= 0;
  const deltaColour = up ? '#6ff2a0' : '#ff7b7b';
  const DeltaIcon = up ? ArrowUpRight : ArrowDownRight;
  return (
    <HudPanel
      title={label}
      accent={colour}
      leading={<HudStatusLed color={colour} />}
      meta={yesterday > 0 ? (
        <HudChip color={deltaColour}>
          <DeltaIcon size={10} /> {up ? '+' : ''}{delta.toFixed(1)}%
        </HudChip>
      ) : null}
    >
      <div style={{
        display: 'flex', alignItems: 'baseline', justifyContent: 'space-between',
        padding: '6px 10px',
      }}>
        <span style={{
          fontFamily: 'var(--font-display, Orbitron, monospace)',
          fontSize: 42, lineHeight: 1,
          color: colour,
          textShadow: `0 0 12px ${colour}66`,
        }}>
          {value.toLocaleString('en-ZA')}
        </span>
        <span style={{ color: colour, opacity: 0.7 }}>{icon}</span>
      </div>
      <Sparkline points={sparkline} colour={colour} />
      <div style={{
        padding: '2px 10px 6px',
        fontSize: 9, opacity: 0.6,
        letterSpacing: '0.08em', textTransform: 'uppercase',
        display: 'flex', justifyContent: 'space-between',
      }}>
        <span>today · orders</span>
        <span>vs {yesterday.toLocaleString('en-ZA')} yest</span>
      </div>
    </HudPanel>
  );
}

/* ---- Sparkline — inline mini area chart for KPI tiles ----
   Accepts the hourly cumulative series; renders an area-fill from the
   baseline plus the stroke. Empty series renders nothing so the tile
   doesn't reserve dead space before the first poll lands. */
function Sparkline({ points, colour }: { readonly points: number[]; readonly colour: string }) {
  if (!points || points.length < 2) {
    return <div style={{ height: 28 }} />;
  }
  const w = 200, h = 28;
  const max = Math.max(1, ...points);
  const xAt = (i: number) => (i / (points.length - 1)) * w;
  const yAt = (v: number) => h - (v / max) * h;
  const linePath = points.map((v, i) => `${i === 0 ? 'M' : 'L'}${xAt(i).toFixed(1)},${yAt(v).toFixed(1)}`).join(' ');
  const areaPath = `${linePath} L${w},${h} L0,${h} Z`;
  const gradId = `sp-${colour.replace('#', '')}`;
  return (
    <svg viewBox={`0 0 ${w} ${h}`} width="100%" height={h} style={{ padding: '0 10px' }} preserveAspectRatio="none">
      <defs>
        <linearGradient id={gradId} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor={colour} stopOpacity="0.45" />
          <stop offset="100%" stopColor={colour} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={areaPath} fill={`url(#${gradId})`} />
      <path d={linePath} fill="none" stroke={colour} strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

/* ---- Row 2: trend line (takes 2/3) + MTD gauges (1/3) ---- */
function TrendAndMTDRow({ product, accent }: { readonly product: ProductSnapshot; readonly accent: string }) {
  return (
    <div style={{
      display: 'grid',
      // Trend chart wants a lot of width. Collapse to a single
      // column below ~1100px so the chart stays legible on laptops.
      gridTemplateColumns: 'repeat(auto-fit, minmax(min(520px, 100%), 1fr))',
      gap: 12,
    }}>
      <HudPanel
        title="Today vs Yesterday vs 7 days ago"
        accent={accent}
        leading={<HudStatusLed color={accent} />}
        meta={
          <HudChip color="#7cc6ff">
            R {Math.round(product.written_revenue.total).toLocaleString('en-ZA')} revenue today
          </HudChip>
        }
      >
        <TrendChart points={product.trend} accent={accent} />
      </HudPanel>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <MTDGauge label="MTD Sales Count" data={product.mtd_sales_count} suffix="orders" accent="#6ff2a0" />
        <MTDGauge
          label="MTD Written Revenue"
          data={product.mtd_revenue}
          suffix="ZAR"
          accent="#ff7de0"
          fmt={(v) => 'R ' + Math.round(v).toLocaleString('en-ZA')}
        />
      </div>
    </div>
  );
}

/* ---- Trend chart — SVG, today area-filled + yesterday + 7d ago ---- */
function TrendChart({ points, accent }: { readonly points: TrendPoint[]; readonly accent: string }) {
  const w = 640, h = 220, pad = 28;
  if (!points || points.length === 0) {
    return <div style={{ padding: 12, fontSize: 11, opacity: 0.6 }}>// no trend data</div>;
  }
  const maxVal = Math.max(
    1,
    ...points.map((p) => Math.max(p.today, p.yesterday, p.last_week)),
  );
  const x = (i: number) => pad + (i / (points.length - 1)) * (w - pad * 2);
  const y = (v: number) => h - pad - (v / maxVal) * (h - pad * 2);

  const path = (key: 'today' | 'yesterday' | 'last_week') =>
    points.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(p[key]).toFixed(1)}`).join(' ');
  const todayArea = `${path('today')} L${x(points.length - 1).toFixed(1)},${(h - pad).toFixed(1)} L${x(0).toFixed(1)},${(h - pad).toFixed(1)} Z`;

  const series = [
    { key: 'last_week' as const, colour: '#7cc6ff55', width: 1.5, dash: '3,3', label: '7 days ago' },
    { key: 'yesterday' as const, colour: '#ffffff66', width: 1.5, dash: '',    label: 'yesterday' },
    { key: 'today' as const,     colour: accent,      width: 2.5, dash: '',    label: 'today' },
  ];
  const gradId = `tr-${accent.replace('#', '')}`;
  const gridSteps = 4;
  return (
    <div style={{ padding: 8 }}>
      <svg viewBox={`0 0 ${w} ${h}`} width="100%" height={h}>
        <defs>
          <linearGradient id={gradId} x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stopColor={accent} stopOpacity="0.35" />
            <stop offset="100%" stopColor={accent} stopOpacity="0" />
          </linearGradient>
        </defs>
        {Array.from({ length: gridSteps + 1 }, (_, i) => {
          const gy = pad + (i / gridSteps) * (h - pad * 2);
          const value = Math.round(maxVal * (1 - i / gridSteps));
          return (
            <g key={i}>
              <line x1={pad} x2={w - pad} y1={gy} y2={gy}
                    stroke="rgba(124,198,255,0.08)" strokeDasharray="2,4" />
              <text x={pad - 4} y={gy + 3} fontSize={9} fill="#7cc6ff88" textAnchor="end"
                    fontFamily="var(--font-mono, monospace)">{value}</text>
            </g>
          );
        })}
        {points.filter((_, i) => i % 4 === 0).map((p, idx) => {
          const i = idx * 4;
          return (
            <text key={p.hour}
                  x={x(i)} y={h - 6}
                  fontSize={9} fill="#7cc6ff88"
                  fontFamily="var(--font-mono, monospace)" textAnchor="middle">
              {p.hour}
            </text>
          );
        })}
        <path d={todayArea} fill={`url(#${gradId})`} />
        {series.map((s) => (
          <path key={s.key} d={path(s.key)} fill="none"
                stroke={s.colour} strokeWidth={s.width}
                strokeDasharray={s.dash || undefined}
                strokeLinejoin="round" strokeLinecap="round" />
        ))}
      </svg>
      <div style={{ display: 'flex', gap: 14, padding: '6px 10px 0', fontSize: 10, flexWrap: 'wrap' }}>
        {series.map((s) => (
          <span key={s.key} style={{ color: s.colour, display: 'inline-flex', alignItems: 'center', gap: 5 }}>
            <span style={{ display: 'inline-block', width: 14, height: 2, background: s.colour }} />
            {s.label}
          </span>
        ))}
      </div>
    </div>
  );
}

/* ---- Row 3: Fulfilment + Payment Health ---- */
function OpsRow({ product, accent }: { readonly product: ProductSnapshot; readonly accent: string }) {
  return (
    <div style={{
      display: 'grid',
      // 1:2 ratio above 1100px, stacks below. Fulfilment card is
      // narrow-friendly so it drops first; Payment Health needs the
      // bar-chart space.
      gridTemplateColumns: 'repeat(auto-fit, minmax(min(380px, 100%), 1fr))',
      gap: 12,
    }}>
      <FulfilmentPanel product={product} accent={accent} />
      <PaymentHealthPanel product={product} />
    </div>
  );
}

function FulfilmentPanel({ product, accent }: { readonly product: ProductSnapshot; readonly accent: string }) {
  const f = product.fulfilment ?? { manufactured: 0, in_transit: 0, delivered: 0, failed: 0, pct_delivered: 0 };
  const total = f.manufactured + f.in_transit + f.delivered + f.failed;
  const rows: Array<{ label: string; value: number; colour: string; icon: React.ReactNode }> = [
    { label: 'Manufactured', value: f.manufactured, colour: '#7cc6ff', icon: <Package size={12} /> },
    { label: 'In Transit',   value: f.in_transit,   colour: '#ffaa00', icon: <Truck size={12} /> },
    { label: 'Delivered',    value: f.delivered,    colour: '#6ff2a0', icon: <CheckCircle2 size={12} /> },
    { label: 'Failed',       value: f.failed,       colour: '#ff7b7b', icon: <XCircle size={12} /> },
  ];
  return (
    <HudPanel
      title="Order Fulfilment"
      accent={accent}
      leading={<HudStatusLed color={accent} animate={total > 0} />}
      meta={total > 0 ? <HudChip color="#6ff2a0">{f.pct_delivered.toFixed(2)}% delivered</HudChip> : <HudChip color="#7cc6ff">not wired</HudChip>}
    >
      {total === 0 ? (
        <div style={{ padding: 10, fontSize: 10.5, opacity: 0.7 }}>
          // no order-fulfilment source wired — awaiting OMS/warehouse query spec.
        </div>
      ) : (
        <div style={{ padding: 8, display: 'grid', gap: 4 }}>
          {rows.map((r) => (
            <div key={r.label} style={{
              display: 'grid', gridTemplateColumns: '16px 1fr auto',
              gap: 8, padding: '4px 6px', alignItems: 'center',
              borderLeft: `2px solid ${r.colour}55`, fontSize: 11,
            }}>
              <span style={{ color: r.colour }}>{r.icon}</span>
              <span style={{ opacity: 0.85 }}>{r.label}</span>
              <span style={{
                fontFamily: 'var(--font-display, Orbitron, monospace)',
                color: r.colour, fontSize: 16,
              }}>{r.value.toLocaleString('en-ZA')}</span>
            </div>
          ))}
          <div style={{
            marginTop: 4, height: 6, borderRadius: 2,
            background: 'rgba(124,198,255,0.15)', overflow: 'hidden',
          }}>
            <div style={{
              width: `${Math.min(100, f.pct_delivered)}%`, height: '100%',
              background: '#6ff2a0', boxShadow: '0 0 6px #6ff2a0',
            }} />
          </div>
        </div>
      )}
    </HudPanel>
  );
}

function PaymentHealthPanel({ product }: { readonly product: ProductSnapshot }) {
  const h = product.payment_health ?? {
    total_payments: 0, total_value: 0,
    successful: { count: 0, pct: 0 },
    failed:     { count: 0, pct: 0 },
    retry:      { count: 0, pct: 0 },
    pending:    { count: 0, pct: 0 },
  };
  const hasData = h.total_payments > 0;
  const buckets: Array<{ label: string; bucket: PaymentStatusBucket; colour: string }> = [
    { label: 'Successful', bucket: h.successful, colour: '#6ff2a0' },
    { label: 'Failed',     bucket: h.failed,     colour: '#ff7b7b' },
    { label: 'Retry',      bucket: h.retry,      colour: '#ffaa00' },
    { label: 'Pending',    bucket: h.pending,    colour: '#7cc6ff' },
  ];
  return (
    <HudPanel
      title="Payment Health"
      accent="#ff7de0"
      icon={<CreditCard size={12} />}
      leading={<HudStatusLed color="#ff7de0" animate={hasData} />}
      meta={hasData ? null : <HudChip color="#7cc6ff">not wired</HudChip>}
    >
      {!hasData ? (
        <div style={{ padding: 10, fontSize: 10.5, opacity: 0.7 }}>
          // no payment-health source wired — awaiting payment.payment query spec.
        </div>
      ) : (
        <div style={{ padding: 8 }}>
          <div style={{
            display: 'grid', gridTemplateColumns: '1fr 1fr',
            gap: 10, padding: '4px 6px', marginBottom: 8,
          }}>
            <div>
              <div style={{ fontSize: 9, opacity: 0.6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                Total Payments
              </div>
              <div style={{
                fontFamily: 'var(--font-display, Orbitron, monospace)',
                fontSize: 26, color: '#ff7de0',
              }}>{h.total_payments.toLocaleString('en-ZA')}</div>
            </div>
            <div>
              <div style={{ fontSize: 9, opacity: 0.6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                Total Value
              </div>
              <div style={{
                fontFamily: 'var(--font-display, Orbitron, monospace)',
                fontSize: 26, color: '#6ff2a0',
              }}>R {(h.total_value / 1_000_000).toFixed(1)}M</div>
            </div>
          </div>
          <div style={{ display: 'grid', gap: 6 }}>
            {buckets.map((b) => (
              <div key={b.label}>
                <div style={{
                  display: 'flex', justifyContent: 'space-between',
                  fontSize: 10.5, padding: '0 6px',
                }}>
                  <span style={{ color: b.colour }}>{b.label}</span>
                  <span>
                    {b.bucket.count.toLocaleString('en-ZA')} · {b.bucket.pct.toFixed(2)}%
                  </span>
                </div>
                <div style={{
                  height: 5, borderRadius: 2, margin: '2px 6px 0',
                  background: 'rgba(124,198,255,0.15)', overflow: 'hidden',
                }}>
                  <div style={{
                    width: `${Math.min(100, b.bucket.pct)}%`, height: '100%',
                    background: b.colour, boxShadow: `0 0 5px ${b.colour}`,
                  }} />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Row 4: Call Centre KPIs + Call Centre Orders line + Bill Run Errors ---- */
function CallCentreRow({ product, accent }: { readonly product: ProductSnapshot; readonly accent: string }) {
  return (
    <div style={{
      display: 'grid',
      // Three equal-ish panels; auto-fit collapses to 2-wide then
      // 1-wide as the viewport shrinks. 340px min keeps the mini-
      // tiles in the CC KPI panel readable.
      gridTemplateColumns: 'repeat(auto-fit, minmax(min(340px, 100%), 1fr))',
      gap: 12,
    }}>
      <CallCentreKPIsPanel product={product} />
      <CallCentreOrdersPanel product={product} accent={accent} />
      <BillRunErrorsPanel product={product} />
    </div>
  );
}

function CallCentreKPIsPanel({ product }: { readonly product: ProductSnapshot }) {
  const k = product.call_centre_kpis ?? { calls_today: 0, answer_rate_pct: 0, avg_wait_sec: 0, abandoned: 0, service_level_pct: 0 };
  const hasData = k.calls_today > 0;
  const tiles: Array<{ label: string; value: string; colour: string }> = [
    { label: 'Calls Today',   value: k.calls_today.toLocaleString('en-ZA'), colour: '#00f0ff' },
    { label: 'Answer Rate',   value: `${k.answer_rate_pct.toFixed(2)}%`,    colour: '#6ff2a0' },
    { label: 'Avg Wait',      value: formatSec(k.avg_wait_sec),             colour: '#ffaa00' },
    { label: 'Abandoned',     value: k.abandoned.toLocaleString('en-ZA'),   colour: '#ff7b7b' },
    { label: 'Service Level', value: `${k.service_level_pct.toFixed(2)}%`,  colour: '#7cc6ff' },
  ];
  return (
    <HudPanel
      title="Call Centre"
      accent="#ffaa00"
      icon={<Headphones size={12} />}
      leading={<HudStatusLed color="#ffaa00" animate={hasData} />}
      meta={hasData ? null : <HudChip color="#7cc6ff">not wired</HudChip>}
    >
      {!hasData ? (
        <div style={{ padding: 10, fontSize: 10.5, opacity: 0.7 }}>
          // no call-centre source wired — awaiting CC export (Genesys / Five9 / internal).
        </div>
      ) : (
        <div style={{ padding: 6, display: 'grid', gap: 4 }}>
          {tiles.map((t) => (
            <div key={t.label} style={{
              display: 'grid', gridTemplateColumns: '1fr auto',
              gap: 6, padding: '4px 6px', alignItems: 'baseline',
              borderLeft: `2px solid ${t.colour}55`, fontSize: 10.5,
            }}>
              <span style={{ opacity: 0.8, textTransform: 'uppercase', letterSpacing: '0.06em', fontSize: 9 }}>{t.label}</span>
              <span style={{ color: t.colour, fontSize: 15, fontFamily: 'var(--font-display, Orbitron, monospace)' }}>
                {t.value}
              </span>
            </div>
          ))}
        </div>
      )}
    </HudPanel>
  );
}

function formatSec(s: number): string {
  if (!s || s <= 0) return '—';
  if (s < 60) return `${s.toFixed(0)}s`;
  const m = Math.floor(s / 60);
  const r = s - m * 60;
  return `${m}.${Math.round(r / 6).toString().padStart(1, '0')}m`;
}

function CallCentreOrdersPanel({ product, accent }: { readonly product: ProductSnapshot; readonly accent: string }) {
  const points = product.call_centre_trend ?? [];
  return (
    <HudPanel
      title="Call Centre — Orders Today"
      accent={accent}
      leading={<HudStatusLed color={accent} animate={points.length > 0} />}
      meta={points.length === 0 ? <HudChip color="#7cc6ff">not wired</HudChip> : null}
    >
      {points.length === 0 ? (
        <div style={{ padding: 10, fontSize: 10.5, opacity: 0.7 }}>
          // no call-centre-by-hour series wired — awaiting CC hourly export.
        </div>
      ) : (
        <CallCentreLine points={points} accent={accent} />
      )}
    </HudPanel>
  );
}

function CallCentreLine({ points, accent }: { readonly points: CallCentreTrendPoint[]; readonly accent: string }) {
  const w = 560, h = 200, pad = 26;
  const maxVal = Math.max(1, ...points.map((p) => Math.max(p.today, p.yesterday)));
  const x = (i: number) => pad + (i / Math.max(1, points.length - 1)) * (w - pad * 2);
  const y = (v: number) => h - pad - (v / maxVal) * (h - pad * 2);
  const path = (key: 'today' | 'yesterday') =>
    points.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(p[key]).toFixed(1)}`).join(' ');
  return (
    <div style={{ padding: 8 }}>
      <svg viewBox={`0 0 ${w} ${h}`} width="100%" height={h}>
        <line x1={pad} x2={w - pad} y1={h - pad} y2={h - pad}
              stroke="rgba(124,198,255,0.15)" />
        <path d={path('yesterday')} fill="none" stroke="#ffffff55" strokeWidth={1.5} strokeDasharray="3,3" />
        <path d={path('today')} fill="none" stroke={accent} strokeWidth={2.5} />
      </svg>
      <div style={{ display: 'flex', gap: 14, padding: '6px 10px 0', fontSize: 10 }}>
        <span style={{ color: accent }}>
          <span style={{ display: 'inline-block', width: 14, height: 2, background: accent, marginRight: 5 }} />today
        </span>
        <span style={{ color: '#ffffff88' }}>
          <span style={{ display: 'inline-block', width: 14, height: 2, background: '#ffffff88', marginRight: 5 }} />yesterday
        </span>
      </div>
    </div>
  );
}

function BillRunErrorsPanel({ product }: { readonly product: ProductSnapshot }) {
  const errs = product.bill_run_errors ?? [];
  const hasData = errs.length > 0;
  const palette = ['#ff7b7b', '#ffaa00', '#ff7de0', '#7cc6ff', '#c488ff', '#6ff2a0'];
  const max = Math.max(1, ...errs.map((e) => e.count));
  return (
    <HudPanel
      title="Bill Run Errors"
      accent="#ff7b7b"
      icon={<AlertCircle size={12} />}
      leading={<HudStatusLed color="#ff7b7b" animate={hasData} />}
      meta={!hasData ? <HudChip color="#7cc6ff">not wired</HudChip> : <HudChip color="#ff7b7b">{errs.reduce((a, b) => a + b.count, 0).toLocaleString('en-ZA')}</HudChip>}
    >
      {!hasData ? (
        <div style={{ padding: 10, fontSize: 10.5, opacity: 0.7 }}>
          // no bill-run error source wired — awaiting billing.bill_run / dunning query.
        </div>
      ) : (
        <div style={{ padding: 6, display: 'grid', gap: 5 }}>
          {errs.map((e, i) => {
            const colour = palette[i % palette.length];
            const pct = (e.count / max) * 100;
            return (
              <div key={e.label} style={{ padding: '2px 6px', fontSize: 10.5 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: colour }}>{e.label}</span>
                  <span style={{ opacity: 0.8 }}>{e.count.toLocaleString('en-ZA')}</span>
                </div>
                <div style={{
                  height: 5, borderRadius: 2, marginTop: 2,
                  background: 'rgba(124,198,255,0.15)', overflow: 'hidden',
                }}>
                  <div style={{
                    width: `${pct}%`, height: '100%',
                    background: colour, boxShadow: `0 0 5px ${colour}`,
                  }} />
                </div>
              </div>
            );
          })}
        </div>
      )}
    </HudPanel>
  );
}

/* ---- MTD vs Budget gauge ---- */
function MTDGauge({
  label, data, suffix, accent, fmt,
}: {
  readonly label: string;
  readonly data: MTDProgress;
  readonly suffix: string;
  readonly accent: string;
  readonly fmt?: (v: number) => string;
}) {
  const pct = Math.max(0, Math.min(150, data.pct));
  const format = fmt ?? ((v: number) => v.toLocaleString('en-ZA'));
  const colour = pct >= 100 ? '#6ff2a0' : pct >= 80 ? '#ffaa00' : accent;
  const noBudget = data.budget <= 0;
  return (
    <HudPanel title={label} accent={colour} leading={<HudStatusLed color={colour} />}>
      <div style={{ padding: '6px 10px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
          <span style={{
            fontFamily: 'var(--font-display, Orbitron, monospace)',
            fontSize: 22, color: colour,
          }}>
            {format(data.actual)}
          </span>
          <span style={{ fontSize: 10, opacity: 0.7 }}>{suffix}</span>
        </div>
        <div style={{ fontSize: 10, opacity: 0.7, marginTop: 2 }}>
          {noBudget ? 'budget not configured' : `of ${format(data.budget)} · ${pct.toFixed(2)}%`}
        </div>
        <div style={{
          marginTop: 6, height: 6, borderRadius: 2,
          background: 'rgba(124,198,255,0.15)', overflow: 'hidden',
        }}>
          <div style={{
            width: `${Math.min(100, pct)}%`, height: '100%',
            background: colour, boxShadow: `0 0 6px ${colour}`,
            transition: 'width 600ms ease-out',
          }} />
        </div>
      </div>
    </HudPanel>
  );
}
