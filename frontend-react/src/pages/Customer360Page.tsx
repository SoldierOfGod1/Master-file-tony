/* ============================================================
   Customer 360 — client lookup with full billing / contact /
   timeline / risk view. Backed by Axiom Postgres via the
   backend customer package.
   ============================================================ */

import {
  type FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react';
import {
  UserSearch,
  Phone,
  Mail,
  Search,
  ExternalLink,
  Copy,
  Calendar,
  DollarSign,
  AlertTriangle,
  Users,
  Activity,
  CreditCard,
  Ticket as TicketIcon,
  RotateCcw,
  MapPin,
} from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudGauge from '../components/shared/HudGauge';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  getCustomerConfig,
  lookupByEmail,
  lookupByPhone,
} from '../api/customer';
import { listConnections, type DBConnection } from '../api/connections';
import type { Customer360, CustomerTimelineEvent } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './Customer360Page.module.css';

type Mode = 'phone' | 'email';

const RISK_COLOR: Record<string, string> = {
  low:    '#6ff2a0',
  medium: '#ffaa00',
  high:   '#ff3355',
};

const TIMELINE_COLOR: Record<string, string> = {
  created:        '#7cc6ff',
  payment:        '#6ff2a0',
  payment_failed: '#ff3355',
  ticket_opened:  '#ffaa00',
  chargeback:     '#ff7de0',
  status_change:  '#00f0ff',
};

function formatRand(value: number): string {
  return `R${value.toLocaleString('en-ZA', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString('en-ZA', {
      day: '2-digit', month: 'short', year: 'numeric',
    });
  } catch {
    return iso;
  }
}

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString('en-ZA', {
      day: '2-digit', month: 'short', year: 'numeric',
      hour: '2-digit', minute: '2-digit',
    });
  } catch {
    return iso;
  }
}

function copyToClipboard(value: string): void {
  void navigator.clipboard.writeText(value).catch(() => { /* ignore */ });
}

/* ---- Identity panel ---- */
function IdentityPanel({ view }: { readonly view: Customer360 }) {
  const { identity, risk_score, lifetime_value, account_age, days_since_last_payment } = view;
  const riskColor = RISK_COLOR[risk_score.band] ?? '#7cc6ff';
  const dunningColor = days_since_last_payment < 0
    ? '#7cc6ff'
    : days_since_last_payment < 30
      ? '#6ff2a0'
      : days_since_last_payment < 60
        ? '#ffaa00'
        : '#ff3355';

  return (
    <HudPanel
      title={identity.full_name || '(unnamed customer)'}
      accent={riskColor}
      leading={<HudStatusLed color={riskColor} />}
      meta={
        <HudChip color={riskColor}>
          {risk_score.band.toUpperCase()} RISK
        </HudChip>
      }
      footer={
        <div className={styles.identityFooter}>
          <span>{identity.email || '—'}</span>
          {identity.status && <HudChip color="#00f0ff">{identity.status}</HudChip>}
        </div>
      }
    >
      <div className={styles.identityBody}>
        <div className={styles.identityGauge}>
          <HudGauge
            value={risk_score.value / 100}
            readout={String(risk_score.value)}
            label="RISK"
            color={riskColor}
            size={140}
          />
          <div className={styles.riskReason}>{risk_score.reason}</div>
        </div>
        <div className={styles.identityStats}>
          <div className={styles.statBlock}>
            <span className={styles.statLabel}>Lifetime Value</span>
            <span className={styles.statValue} style={{ color: '#6ff2a0' }}>
              {formatRand(lifetime_value)}
            </span>
          </div>
          <div className={styles.statBlock}>
            <span className={styles.statLabel}>Account Age</span>
            <span className={styles.statValue}>
              {account_age.human_friendly}
            </span>
            {account_age.since && (
              <span className={styles.statSub}>
                since {formatDate(account_age.since)}
              </span>
            )}
          </div>
          <div className={styles.statBlock}>
            <span className={styles.statLabel}>Days Since Last Payment</span>
            <span className={styles.statValue} style={{ color: dunningColor }}>
              {days_since_last_payment < 0 ? '—' : days_since_last_payment}
            </span>
          </div>
          <div className={styles.statBlock}>
            <span className={styles.statLabel}>Customer ID</span>
            <span className={styles.statValueMono}>
              {identity.id ? identity.id.slice(0, 12) + '…' : '—'}
              {identity.id && (
                <button
                  type="button"
                  className={styles.copyBtn}
                  onClick={() => copyToClipboard(identity.id)}
                  title="Copy full ID"
                >
                  <Copy size={10} />
                </button>
              )}
            </span>
          </div>
        </div>
      </div>
    </HudPanel>
  );
}

/* ---- Contacts panel ---- */
function ContactsPanel({ view }: { readonly view: Customer360 }) {
  const { contacts } = view;
  return (
    <HudPanel
      title="Contact Channels"
      accent="#00f0ff"
      leading={<HudStatusLed color="#6ff2a0" />}
      meta={<>{contacts.length}</>}
    >
      {contacts.length === 0 ? (
        <div className={styles.muted}>// no contact rows returned</div>
      ) : (
        <div className={styles.contactList}>
          {contacts.map((c, i) => (
            <div key={i} className={styles.contactRow}>
              {c.preferred && <HudChip color="#ffaa00">PRIMARY</HudChip>}
              <div className={styles.contactFields}>
                {c.phone && (
                  <span className={styles.contactField}>
                    <Phone size={10} />
                    <a href={`tel:${c.phone}`} className={styles.link}>{c.phone}</a>
                    <button
                      type="button"
                      className={styles.copyBtn}
                      onClick={() => copyToClipboard(c.phone)}
                    >
                      <Copy size={10} />
                    </button>
                  </span>
                )}
                {c.email && (
                  <span className={styles.contactField}>
                    <Mail size={10} />
                    <a href={`mailto:${c.email}`} className={styles.link}>{c.email}</a>
                    <button
                      type="button"
                      className={styles.copyBtn}
                      onClick={() => copyToClipboard(c.email)}
                    >
                      <Copy size={10} />
                    </button>
                  </span>
                )}
                {(c.street_name || c.city) && (
                  <span className={styles.contactField}>
                    <MapPin size={10} />
                    <span>
                      {[c.street_number, c.street_name].filter(Boolean).join(' ')},
                      {' '}
                      {[c.suburb, c.city, c.province, c.postal_code].filter(Boolean).join(', ')}
                    </span>
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Payments panel ---- */
function PaymentsPanel({ view }: { readonly view: Customer360 }) {
  const { payments } = view;
  const shown = payments.slice(0, 10);
  return (
    <HudPanel
      title="Recent Payments"
      accent="#6ff2a0"
      leading={<HudStatusLed color="#6ff2a0" />}
      meta={<><CreditCard size={10} /> {payments.length}</>}
    >
      {shown.length === 0 ? (
        <div className={styles.muted}>// no payment history found</div>
      ) : (
        <div className={styles.paymentList}>
          {shown.map((p) => {
            const isFail = /fail|declined/i.test(p.status);
            const color = isFail ? '#ff3355' : '#6ff2a0';
            return (
              <div key={p.id} className={styles.paymentRow}>
                <span className={styles.paymentDate}>{formatDate(p.payment_date)}</span>
                <HudChip color={color}>{p.status}</HudChip>
                <span className={styles.paymentChannel}>{p.channel}</span>
                <span className={styles.paymentAmount} style={{ color }}>
                  {formatRand(p.amount)}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Service timeline ---- */
function TimelinePanel({ events }: { readonly events: CustomerTimelineEvent[] }) {
  return (
    <HudPanel
      title="Service Timeline"
      accent="#00f0ff"
      leading={<HudStatusLed color="#00f0ff" />}
      meta={<><Activity size={10} /> {events.length}</>}
    >
      {events.length === 0 ? (
        <div className={styles.muted}>// no timeline events</div>
      ) : (
        <div className={styles.timeline}>
          {events.slice(0, 20).map((evt, i) => {
            const color = TIMELINE_COLOR[evt.type] ?? '#7cc6ff';
            return (
              <div key={i} className={styles.timelineRow}>
                <span className={styles.timelineDot} style={{ background: color, boxShadow: `0 0 6px ${color}` }} />
                <div className={styles.timelineBody}>
                  <div className={styles.timelineLabel}>{evt.label}</div>
                  {evt.detail && <div className={styles.timelineDetail}>{evt.detail}</div>}
                  <div className={styles.timelineWhen}>{formatDateTime(evt.at)}</div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Payment heatmap (30-day contribution grid) ---- */
function HeatmapPanel({ heatmap }: { readonly heatmap: number[] }) {
  const max = Math.max(1, ...heatmap);
  return (
    <HudPanel
      title="Payment Heatmap · 30d"
      accent="#6ff2a0"
      leading={<HudStatusLed color="#6ff2a0" animate={false} />}
      meta={<><Calendar size={10} /> {heatmap.reduce((a, b) => a + b, 0)} pmts</>}
    >
      <div className={styles.heatmap}>
        {heatmap.map((count, i) => {
          const intensity = count / max;
          const bg = count === 0
            ? 'rgba(0, 240, 255, 0.05)'
            : `rgba(111, 242, 160, ${0.25 + intensity * 0.7})`;
          const daysAgo = 29 - i; // reverse so oldest → newest reads L-to-R
          const label = daysAgo === 0 ? 'today' : `${daysAgo}d ago`;
          return (
            <span
              key={i}
              className={styles.heatmapCell}
              style={{ background: bg }}
              title={`${count} payment(s) · ${label}`}
            />
          );
        }).reverse()}
      </div>
    </HudPanel>
  );
}

/* ---- Neighbours panel ---- */
function NeighboursPanel({ view }: { readonly view: Customer360 }) {
  const { neighbours } = view;
  return (
    <HudPanel
      title="Same-Address Residents"
      accent="#ff7de0"
      leading={<HudStatusLed color="#ff7de0" animate={neighbours.length > 0} />}
      meta={<><Users size={10} /> {neighbours.length}</>}
    >
      {neighbours.length === 0 ? (
        <div className={styles.muted}>
          // no other customers at this address
        </div>
      ) : (
        <div className={styles.neighbourList}>
          {neighbours.map((n) => (
            <HudChip key={n.id} color="#ff7de0">
              {n.full_name}
            </HudChip>
          ))}
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Subscriptions / Tickets / Chargebacks — stacked minis ---- */
function ExtrasColumn({ view }: { readonly view: Customer360 }) {
  return (
    <>
      <HudPanel
        title="Subscriptions"
        accent="#7cc6ff"
        leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        meta={<>{view.subscriptions.length}</>}
      >
        {view.subscriptions.length === 0 ? (
          <div className={styles.muted}>// schema not detected on this cluster</div>
        ) : (
          view.subscriptions.map((s) => (
            <div key={s.id} className={styles.miniRow}>
              <span className={styles.miniLabel}>{s.name}</span>
              <HudChip color="#7cc6ff">{s.status}</HudChip>
              <span className={styles.miniAmt}>{formatRand(s.price)}</span>
            </div>
          ))
        )}
      </HudPanel>

      <HudPanel
        title="Tickets"
        accent="#ffaa00"
        leading={<HudStatusLed color="#ffaa00" animate={view.tickets.length > 0} />}
        meta={<><TicketIcon size={10} /> {view.tickets.length}</>}
      >
        {view.tickets.length === 0 ? (
          <div className={styles.muted}>// no support tickets found</div>
        ) : (
          view.tickets.slice(0, 5).map((t) => (
            <div key={t.id} className={styles.miniRow}>
              <span className={styles.miniLabel}>{t.subject}</span>
              <HudChip color="#ffaa00">{t.status}</HudChip>
              <span className={styles.miniDate}>{formatDate(t.created_at)}</span>
            </div>
          ))
        )}
      </HudPanel>

      <HudPanel
        title="Chargebacks / Reversals"
        accent="#ff3355"
        leading={<HudStatusLed color={view.chargebacks.length > 0 ? '#ff3355' : '#6ff2a0'} />}
        meta={<><RotateCcw size={10} /> {view.chargebacks.length}</>}
      >
        {view.chargebacks.length === 0 ? (
          <div className={styles.muted}>// none on record</div>
        ) : (
          view.chargebacks.map((c) => (
            <div key={c.id} className={styles.miniRow}>
              <span className={styles.miniLabel}>{c.reason || c.status}</span>
              <span className={styles.miniAmt} style={{ color: '#ff3355' }}>
                {formatRand(c.amount)}
              </span>
              <span className={styles.miniDate}>{formatDate(c.created_at)}</span>
            </div>
          ))
        )}
      </HudPanel>
    </>
  );
}

/* ---- Deep-link buttons ---- */
function DeepLinksPanel({ view }: { readonly view: Customer360 }) {
  return (
    <HudPanel
      title="Open Elsewhere"
      accent="#00f0ff"
      leading={<HudStatusLed color="#00f0ff" animate={false} />}
    >
      <div className={styles.deepLinks}>
        <a className={styles.deepLink} href={view.deep_links.station} target="_blank" rel="noreferrer">
          <ExternalLink size={12} /> Station (the101)
        </a>
        <a className={styles.deepLink} href={view.deep_links.athena} target="_blank" rel="noreferrer">
          <ExternalLink size={12} /> Athena Assisted Sales
        </a>
        <a className={styles.deepLink} href={view.deep_links.raingo} target="_blank" rel="noreferrer">
          <ExternalLink size={12} /> raingo
        </a>
      </div>
    </HudPanel>
  );
}

/* ---- Page ---- */
export default function Customer360Page() {
  const [mode, setMode] = useState<Mode>('phone');
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(false);
  const [view, setView] = useState<Customer360 | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [connections, setConnections] = useState<DBConnection[]>([]);
  const [activeConnID, setActiveConnID] = useState<string>('');

  // Load configured status + the full connection list so the user can pick
  // which cluster to query. Defaults to whichever connection is flagged
  // primary on the backend.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const [cfg, conns] = await Promise.all([
        getCustomerConfig(),
        listConnections(),
      ]);
      if (cancelled) return;
      setConfigured(cfg?.configured ?? false);
      const usable = conns.filter((c) => c.driver === 'postgres' && c.filled);
      setConnections(usable);
      // Initial pick: whichever was tagged primary, otherwise the first usable.
      const primary = usable.find((c) => c.is_primary) ?? usable[0];
      setActiveConnID(primary?.id ?? '');
    })();
    return () => { cancelled = true; };
  }, []);

  const handleLookup = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    const q = query.trim();
    if (!q) return;
    setLoading(true);
    setError(null);
    try {
      const result = mode === 'phone'
        ? await lookupByPhone(q, activeConnID || undefined)
        : await lookupByEmail(q, activeConnID || undefined);
      if (!result) {
        setView(null);
        setError(`No customer found for ${q}`);
      } else {
        setView(result);
      }
    } finally {
      setLoading(false);
    }
  }, [query, mode, activeConnID]);

  const segments = useMemo(() => {
    if (!view) return undefined;
    const success = view.payments.filter((p) => /success|paid/i.test(p.status)).length;
    const failed = view.payments.filter((p) => /fail|declined/i.test(p.status)).length;
    const other = Math.max(0, view.payments.length - success - failed);
    return [
      { label: 'Paid', value: success, color: '#6ff2a0' },
      { label: 'Failed', value: failed, color: '#ff3355' },
      { label: 'Other', value: other, color: '#7cc6ff' },
    ];
  }, [view]);

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Customer 360 · Client Lookup"
        subtitle={view
          ? `Looking at ${view.identity.full_name || view.identity.id}`
          : 'Enter a phone number or email to pull the full customer view'}
        gaugeValue={view ? view.risk_score.value / 100 : 0}
        gaugeReadout={view ? String(view.risk_score.value) : '—'}
        gaugeLabel={view ? 'RISK' : 'IDLE'}
        gaugeColor={view ? RISK_COLOR[view.risk_score.band] ?? '#7cc6ff' : '#7cc6ff'}
        segments={segments}
        extra={
          <div className={styles.searchIcon}>
            <UserSearch size={22} style={{ color: '#00f0ff' }} />
          </div>
        }
      />

      {configured === false && (
        <div className={styles.warnBanner}>
          <AlertTriangle size={14} />
          <div>
            <strong>Axiom not connected.</strong> Paste your Postgres password
            in <a href="/settings" className={styles.link}>Settings</a> to enable
            customer lookup. Host / user / database are already pre-filled.
          </div>
        </div>
      )}

      <form onSubmit={handleLookup} className={styles.searchRow}>
        <div className={styles.modeToggle}>
          <button
            type="button"
            className={`${styles.modeBtn} ${mode === 'phone' ? styles.modeBtnActive : ''}`}
            onClick={() => setMode('phone')}
          >
            <Phone size={11} /> Phone
          </button>
          <button
            type="button"
            className={`${styles.modeBtn} ${mode === 'email' ? styles.modeBtnActive : ''}`}
            onClick={() => setMode('email')}
          >
            <Mail size={11} /> Email
          </button>
        </div>
        <input
          type={mode === 'email' ? 'email' : 'tel'}
          className={styles.searchInput}
          placeholder={mode === 'phone' ? '+27 83 123 4567' : 'name@rain.co.za'}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          disabled={!configured}
          autoFocus
        />
        {connections.length > 1 && (
          <select
            className={styles.connSelect}
            value={activeConnID}
            onChange={(e) => setActiveConnID(e.target.value)}
            title="Which database to query"
          >
            {connections.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}{c.is_primary ? ' ★' : ''}
              </option>
            ))}
          </select>
        )}
        <button
          type="submit"
          className={styles.searchBtn}
          disabled={loading || !configured || !query.trim()}
        >
          <Search size={13} /> {loading ? 'Looking up…' : 'Lookup'}
        </button>
      </form>

      {error && (
        <HudPanel
          title="Not Found"
          accent="#ffaa00"
          leading={<HudStatusLed color="#ffaa00" animate={false} />}
        >
          <div className={styles.notFound}>
            <AlertTriangle size={28} />
            <span>{error}</span>
            <span className={styles.notFoundHint}>
              Try the other format (phone vs email) or check for typos.
            </span>
          </div>
        </HudPanel>
      )}

      {view && (
        <div className={styles.resultGrid}>
          <div className={styles.resultLeft}>
            <IdentityPanel view={view} />
            <ContactsPanel view={view} />
            <PaymentsPanel view={view} />
            <DeepLinksPanel view={view} />
          </div>
          <div className={styles.resultRight}>
            <TimelinePanel events={view.timeline} />
            <HeatmapPanel heatmap={view.payment_heatmap} />
            <NeighboursPanel view={view} />
            <ExtrasColumn view={view} />
          </div>
        </div>
      )}

      {!view && !error && configured && (
        <HudPanel
          title="Idle"
          accent="#7cc6ff"
          leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        >
          <div className={styles.idle}>
            <DollarSign size={28} />
            <span>Start by searching for a customer above.</span>
          </div>
        </HudPanel>
      )}
    </div>
  );
}
