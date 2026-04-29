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
  Banknote,
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
  lookupByID,
  lookupByPhone,
  getIMSIOverride,
  setIMSIOverride,
  getUsageSummary,
  type UsageSummary,
} from '../api/customer';
import { listConnections, type DBConnection } from '../api/connections';
import type {
  Customer360,
  CustomerProduct,
  CustomerTimelineEvent,
  CustomerUsageSnapshot,
  IdentityCandidate,
} from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './Customer360Page.module.css';
import CommandBar from './customer360/CommandBar';
import PredictionStackPanel from './customer360/PredictionStackPanel';
import JourneyStagePanel from './customer360/JourneyStagePanel';
import NBAPanel from './customer360/NBAPanel';

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
  const contacts = view.contacts ?? [];
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
  const payments = view.payments ?? [];
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
function TimelinePanel({ events }: { readonly events: CustomerTimelineEvent[] | null | undefined }) {
  const rows = events ?? [];
  return (
    <HudPanel
      title="Service Timeline"
      accent="#00f0ff"
      leading={<HudStatusLed color="#00f0ff" />}
      meta={<><Activity size={10} /> {rows.length}</>}
    >
      {rows.length === 0 ? (
        <div className={styles.muted}>// no timeline events</div>
      ) : (
        <div className={styles.timeline}>
          {rows.slice(0, 20).map((evt, i) => {
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
function HeatmapPanel({ heatmap }: { readonly heatmap: number[] | null | undefined }) {
  const cells = heatmap ?? [];
  const max = Math.max(1, ...cells);
  return (
    <HudPanel
      title="Payment Heatmap · 30d"
      accent="#6ff2a0"
      leading={<HudStatusLed color="#6ff2a0" animate={false} />}
      meta={<><Calendar size={10} /> {cells.reduce((a, b) => a + b, 0)} pmts</>}
    >
      <div className={styles.heatmap}>
        {cells.map((count, i) => {
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
  const neighbours = view.neighbours ?? [];
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
  const subscriptions = view.subscriptions ?? [];
  const tickets = view.tickets ?? [];
  const chargebacks = view.chargebacks ?? [];
  return (
    <>
      <HudPanel
        title="Subscriptions"
        accent="#7cc6ff"
        leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        meta={<>{subscriptions.length}</>}
      >
        {subscriptions.length === 0 ? (
          <div className={styles.muted}>// schema not detected on this cluster</div>
        ) : (
          subscriptions.map((s) => (
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
        leading={<HudStatusLed color="#ffaa00" animate={tickets.length > 0} />}
        meta={<><TicketIcon size={10} /> {tickets.length}</>}
      >
        {tickets.length === 0 ? (
          <div className={styles.muted}>// no support tickets found</div>
        ) : (
          tickets.slice(0, 5).map((t) => (
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
        leading={<HudStatusLed color={chargebacks.length > 0 ? '#ff3355' : '#6ff2a0'} />}
        meta={<><RotateCcw size={10} /> {chargebacks.length}</>}
      >
        {chargebacks.length === 0 ? (
          <div className={styles.muted}>// none on record</div>
        ) : (
          chargebacks.map((c) => (
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

      {/* Live Usage Overview from rain Axiom HTTP API. Sits under
          Chargebacks so the operator sees the customer-summary
          stack (subscriptions → tickets → chargebacks → usage →
          contacts) without scrolling past the SIM Diagnostics list. */}
      <UsageOverviewLivePanel view={view} />

      {/* Contact Channels — moved here from the left column at
          operator request so the address / phone / email block
          sits inside the customer-summary stack right under the
          headline usage numbers. */}
      <ContactsPanel view={view} />
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
      </div>
    </HudPanel>
  );
}

/* ---- Products panel (mobile / loop / 101 families) ---- */
function ProductsPanel({ view }: { readonly view: Customer360 }) {
  const all = view.products ?? [];
  const [detailFor, setDetailFor] = useState<CustomerProduct | null>(null);
  const families: Array<['mobile' | 'loop' | '101', string, string]> = [
    ['mobile', 'Mobile', '#00f0ff'],
    ['loop',   'Loop',   '#6ff2a0'],
    ['101',    '101',    '#ffaa00'],
  ];
  const groups = families
    .map(([k, label, colour]) => ({
      k,
      label,
      colour,
      items: collapseFamily(k, all.filter((p) => p.family === k)),
    }))
    .filter((g) => g.items.length > 0);
  const status = (view.data_sources ?? []).find((s) => s.name === 'products');

  if (all.length === 0) {
    return (
      <HudPanel
        title="Products"
        accent="#00f0ff"
        leading={<HudStatusLed color="#7cc6ff" />}
        meta={<HudChip color="#7cc6ff">empty</HudChip>}
      >
        <div style={{ fontSize: 11, opacity: 0.7, padding: 6 }}>
          // no mobile / loop / 101 products linked to this customer
          {status?.latency_ms ? ` (probed in ${status.latency_ms}ms)` : ''}
        </div>
      </HudPanel>
    );
  }

  return (
    <HudPanel
      title={`Products · ${all.length}`}
      accent="#00f0ff"
      leading={<HudStatusLed color="#00f0ff" />}
    >
      {groups.map((g) => (
        <div key={g.k} style={{ marginBottom: 10 }}>
          <div style={{
            fontSize: 9, textTransform: 'uppercase', letterSpacing: '0.1em',
            color: g.colour, padding: '2px 4px',
          }}>
            {g.label} · {g.items.length}
          </div>
          <div style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(min(150px, 100%), 1fr))',
            gap: 8,
          }}>
            {g.items.slice(0, 8).map((p) => (
              <ProductCard
                key={p.id}
                p={p}
                colour={g.colour}
                onClick={() => setDetailFor(p)}
              />
            ))}
            {g.items.length > 8 && (
              <div style={{
                fontSize: 10,
                opacity: 0.6,
                padding: '3px 6px',
                gridColumn: '1 / -1',
              }}>
                + {g.items.length - 8} more
              </div>
            )}
          </div>
        </div>
      ))}
      {detailFor && (
        <ProductUsageModal
          product={detailFor}
          usage={view.usage ?? []}
          onClose={() => setDetailFor(null)}
        />
      )}
    </HudPanel>
  );
}

function ProductCard({
  p, colour, onClick,
}: {
  readonly p: CustomerProduct;
  readonly colour: string;
  readonly onClick?: () => void;
}) {
  const title = displayTitle(p);
  const subtitle = displaySubtitle(p, title);
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        padding: 8,
        background: 'rgba(0, 240, 255, 0.04)',
        border: `1px solid ${colour}33`,
        borderLeft: `3px solid ${colour}`,
        borderRadius: 4,
        fontFamily: 'var(--font-mono, monospace)',
        textAlign: 'left',
        cursor: onClick ? 'pointer' : 'default',
        color: 'inherit',
      }}
      title="Click for usage details"
    >
      <div style={{
        height: 80,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'rgba(0,0,0,0.25)',
        borderRadius: 3,
        overflow: 'hidden',
      }}>
        {p.image_url ? (
          <img
            src={p.image_url}
            alt={title}
            style={{ maxHeight: '100%', maxWidth: '100%', objectFit: 'contain' }}
            loading="lazy"
          />
        ) : (
          <span style={{ fontSize: 9, opacity: 0.5 }}>no image</span>
        )}
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 4 }}>
        <span style={{
          color: colour,
          fontSize: 11,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          flex: 1,
        }}>
          {title}
        </span>
        {p.state && <HudChip color={productStateColor(p.state)}>{p.state}</HudChip>}
      </div>
      {subtitle && (
        <div style={{ fontSize: 10, opacity: 0.75, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {subtitle}
        </div>
      )}
      {p.start_date && (
        <div style={{ fontSize: 9, opacity: 0.55 }}>
          {formatDate(p.start_date)}
        </div>
      )}
    </button>
  );
}

/* ---- Bundle collapsing ---------------------------------------------------
   Axiom stores the loop/101 product as many rows: a bundle, several care
   plans, device groups, SIM groups, colour variants, etc. The parent_id
   chain lives in products we don't fetch (hierarchy parents are outside
   the billing_account slice), so walking parents doesn't merge them. The
   customer only recognises "rain Loop" and "101 Pro" as single things —
   so for those families we collapse every row into one representative
   card, rolling up the colour variant and the ACTIVE state from siblings.
   Mobile is preserved one-card-per-SIM because each MSISDN is its own
   distinct product the customer sees. */
function collapseFamily(family: string, items: CustomerProduct[]): CustomerProduct[] {
  if (items.length === 0) return items;
  if (family === 'mobile') return items;
  if (family !== 'loop' && family !== '101') return items;

  const keeper = pickRepresentative(items);
  const colour = items.find((p) => p.colour_variant)?.colour_variant ?? keeper.colour_variant;
  // Prefer the image from whichever sibling actually carries the
  // rolled-up colour — the keeper row usually has the plain white
  // image because its own name doesn't mention the colour.
  const colourBearer = colour ? items.find((p) => p.colour_variant === colour) : undefined;
  const imageURL = colourBearer?.image_url || keeper.image_url;
  const activeState = items.find((p) => (p.state ?? '').toUpperCase() === 'ACTIVE')?.state;
  const startDates = items
    .map((p) => p.start_date)
    .filter((s): s is string => !!s)
    .sort();
  // Also take the first non-empty identifier we find across siblings
  // so the click-through modal has something to correlate with usage.
  const msisdn = keeper.msisdn || items.find((p) => p.msisdn)?.msisdn;
  const iccid  = keeper.iccid  || items.find((p) => p.iccid)?.iccid;
  const imei   = keeper.imei   || items.find((p) => p.imei)?.imei;
  const imsi   = keeper.imsi   || items.find((p) => p.imsi)?.imsi;
  return [{
    ...keeper,
    colour_variant: colour,
    image_url: imageURL,
    state: activeState ?? keeper.state,
    start_date: startDates[0] ?? keeper.start_date,
    msisdn, iccid, imei, imsi,
  }];
}

function pickRepresentative(items: CustomerProduct[]): CustomerProduct {
  // 101 has two SKUs — Pro and Home. Surface whichever one the
  // customer actually owns, ignoring generic "101" bundle rows.
  const pro = items.find((p) => p.product_line === '101 Pro');
  if (pro) return pro;
  const home = items.find((p) => p.product_line === '101 Home');
  if (home) return home;
  // For loop (and generic 101), prefer a row that has a resolved
  // product_line, then any bundle, then the longest name.
  const withLine = items.find((p) => p.product_line);
  if (withLine) return withLine;
  const bundle = items.find((p) => p.is_bundle);
  if (bundle) return bundle;
  return [...items].sort((a, b) => (b.name?.length ?? 0) - (a.name?.length ?? 0))[0];
}

function displayTitle(p: CustomerProduct): string {
  const base = p.product_line || p.name || p.category || '(unnamed)';
  if (p.family === 'loop' && p.colour_variant) {
    const c = p.colour_variant.charAt(0).toUpperCase() + p.colour_variant.slice(1);
    if (!base.toLowerCase().includes(p.colour_variant)) return `${base} · ${c}`;
  }
  return base;
}

function displaySubtitle(p: CustomerProduct, title: string): string {
  if (p.msisdn) return p.msisdn;
  if (p.iccid) return 'ICCID ' + p.iccid;
  if (p.imei) return 'IMEI ' + p.imei;
  if (p.name && p.name !== title) return p.name;
  return '';
}

/* ---- Product usage modal --------------------------------------------------
   Opens when the user clicks a product card. Correlates the product to the
   usage snapshots we already fetched (resource_policy per msisdn) by matching
   on msisdn/imei/iccid where available. When no link can be made we show
   every MSISDN on the account + a soft warning. */
function ProductUsageModal({
  product, usage, onClose,
}: {
  readonly product: CustomerProduct;
  readonly usage: CustomerUsageSnapshot[];
  readonly onClose: () => void;
}) {
  // Correlation strategy, widest-first: we always want SOMETHING
  // useful in the modal. First try exact identifier matches (msisdn,
  // imsi, imei), then fall back to showing every row and flagging
  // which look likely for this family.
  const strongMatch = (u: CustomerUsageSnapshot): boolean => {
    if (product.msisdn && u.msisdn === product.msisdn) return true;
    if (product.imsi && u.imsi && u.imsi === product.imsi) return true;
    if (product.imei && u.imei && u.imei === product.imei) return true;
    return false;
  };
  const matches = usage.filter(strongMatch);
  const showAll = matches.length === 0 && usage.length > 0;
  const rows = showAll ? usage : matches;
  const title = displayTitle(product);

  return (
    <div
      style={{
        position: 'fixed', inset: 0, background: 'rgba(5,8,16,0.65)',
        display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
      }}
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(560px, 92vw)', maxHeight: '85vh', overflow: 'auto',
          background: '#0a0c12', border: '1px solid #00f0ff44', borderRadius: 6,
          fontFamily: 'var(--font-mono, monospace)', color: '#c7e9ff',
        }}
      >
        <div style={{
          display: 'flex', gap: 12, padding: 14, alignItems: 'center',
          borderBottom: '1px solid #00f0ff22',
        }}>
          <div style={{
            width: 72, height: 72, flexShrink: 0,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            background: 'rgba(0,0,0,0.35)', borderRadius: 4, overflow: 'hidden',
          }}>
            {product.image_url ? (
              <img src={product.image_url} alt={title} style={{ maxHeight: '100%', maxWidth: '100%', objectFit: 'contain' }} />
            ) : (
              <span style={{ fontSize: 10, opacity: 0.5 }}>no image</span>
            )}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{
              fontSize: 9, opacity: 0.6, textTransform: 'uppercase', letterSpacing: '0.1em',
            }}>
              {product.family}
            </div>
            <div style={{ fontSize: 15, color: '#00f0ff' }}>{title}</div>
            <div style={{ fontSize: 10, opacity: 0.75, marginTop: 2 }}>
              {[product.state, product.start_date ? 'since ' + formatDate(product.start_date) : '']
                .filter(Boolean).join(' · ')}
            </div>
          </div>
          <button
            type="button" onClick={onClose}
            style={{
              background: 'transparent', border: '1px solid #7cc6ff55', color: '#7cc6ff',
              padding: '3px 8px', borderRadius: 3, fontSize: 10, cursor: 'pointer',
            }}
          >close</button>
        </div>

        <div style={{ padding: 14, display: 'grid', gap: 10 }}>
          <section>
            <SectionHeader>Device</SectionHeader>
            <KV label="MSISDN"    value={product.msisdn} />
            <KV label="ICCID"     value={product.iccid} />
            <KV label="IMEI"      value={product.imei} />
            <KV label="IMSI"      value={product.imsi} />
            <KV label="Account"   value={product.account_number} />
            <KV label="Offering"  value={product.service_type} />
          </section>

          <section>
            <SectionHeader>Usage</SectionHeader>
            {usage.length === 0 ? (
              <div style={{ fontSize: 10, opacity: 0.7 }}>
                // no resource_policy rows loaded for this customer.
              </div>
            ) : (
              <>
                {showAll && (
                  <div style={{ fontSize: 10, opacity: 0.7, marginBottom: 6 }}>
                    // no direct correlation (product has no msisdn/imsi/imei) — showing
                    all {usage.length} usage row{usage.length === 1 ? '' : 's'} on this account.
                  </div>
                )}
                {rows.map((u) => <UsageBlock key={u.msisdn + ':' + (u.imsi ?? '')} u={u} />)}
              </>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}

function SectionHeader({ children }: { readonly children: React.ReactNode }) {
  return (
    <div style={{
      fontSize: 9, textTransform: 'uppercase', letterSpacing: '0.12em',
      color: '#00f0ff', marginBottom: 6, opacity: 0.85,
    }}>{children}</div>
  );
}

function KV({ label, value }: { readonly label: string; readonly value?: string }) {
  if (!value) return null;
  return (
    <div style={{
      display: 'grid', gridTemplateColumns: '100px 1fr', gap: 6,
      fontSize: 10.5, padding: '2px 0',
    }}>
      <span style={{ opacity: 0.6 }}>{label}</span>
      <span style={{ color: '#c7e9ff', wordBreak: 'break-all' }}>{value}</span>
    </div>
  );
}

function UsageBlock({ u }: { readonly u: CustomerUsageSnapshot }) {
  const { pct } = parseQuota(u.quota, u.load);
  const color = pct >= 90 ? '#ff7b7b' : pct >= 70 ? '#ffaa00' : '#6ff2a0';
  return (
    <div style={{
      padding: 8, border: `1px solid ${color}33`, borderLeft: `3px solid ${color}`,
      borderRadius: 3, marginBottom: 6,
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11 }}>
        <span style={{ color: '#00f0ff' }}>{u.msisdn}</span>
        <HudChip color={color}>{u.quota_status || '—'}</HudChip>
      </div>
      {u.policy_name && (
        <div style={{ fontSize: 10, opacity: 0.8, marginTop: 3 }}>
          {u.policy_name}
          {u.service_name && u.service_name !== u.policy_name ? ' · ' + u.service_name : ''}
        </div>
      )}
      <div style={{
        marginTop: 5, height: 5, borderRadius: 2,
        background: 'rgba(124,198,255,0.15)', overflow: 'hidden',
      }}>
        <div style={{ width: `${Math.min(100, pct)}%`, height: '100%', background: color, boxShadow: `0 0 6px ${color}` }} />
      </div>
      <div style={{ fontSize: 9, opacity: 0.7, marginTop: 4, display: 'flex', gap: 10, flexWrap: 'wrap' }}>
        <span>load {u.load || '—'}</span>
        <span>quota {u.quota || '—'}</span>
        {u.ip_address && <span>ip {u.ip_address}</span>}
        {u.state && <span>state {u.state}</span>}
      </div>
    </div>
  );
}

function productStateColor(state: string): string {
  const s = state.toUpperCase();
  if (/ACTIVE|STARTED/.test(s)) return '#6ff2a0';
  if (/SUSPEND|HOLD/.test(s)) return '#ffaa00';
  if (/CANCEL|TERMINATED|ENDED/.test(s)) return '#ff7b7b';
  return '#7cc6ff';
}

/* ---- Usage panel (policy + quota per msisdn) ---- */
/* ---- Usage Overview LIVE — pulls from rain Axiom HTTP API ----
   Replaces the Athena-driven Overview as the primary tile. Hits
   /api/v1/customer/usage/summary?msisdn=<X> for every SIM the
   customer has and renders the 4-KPI strip exactly like the
   Greenshot mock (red Total / blue Avg / purple Active Days /
   orange Peak). Source is api.sit.rain.co.za, not Athena —
   results are real-time, no S3 token expiry. */
function UsageOverviewLivePanel({ view }: { readonly view: Customer360 }) {
  // Collect every identifier we know for this customer that the
  // upstream API will accept. The API param is named `msisdn` but
  // also accepts IMSI (the rain endpoint is liberal). De-dupe so
  // a customer with one SIM doesn't trigger two identical fetches.
  const ids = useMemo(() => {
    const out = new Set<string>();
    for (const u of view.usage ?? []) {
      if (u.msisdn) out.add(u.msisdn);
    }
    for (const s of view.sim_diagnostics ?? []) {
      if (s.imsi) out.add(String(s.imsi));
    }
    return Array.from(out).slice(0, 5); // cap at 5 to avoid runaway fetches
  }, [view]);

  const [byID, setByID] = useState<Record<string, UsageSummary | null>>({});
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (ids.length === 0) {
      setByID({});
      return;
    }
    let cancelled = false;
    setLoading(true);
    void Promise.all(
      ids.map(async (id) => [id, await getUsageSummary(id)] as const),
    ).then((pairs) => {
      if (cancelled) return;
      const next: Record<string, UsageSummary | null> = {};
      for (const [id, s] of pairs) next[id] = s;
      setByID(next);
      setLoading(false);
    });
    return () => { cancelled = true; };
  }, [ids]);

  if (ids.length === 0) {
    return null;
  }

  // Sort SIMs by total descending so the most-used SIM renders first.
  const sims = ids
    .map((id) => ({ id, summary: byID[id] }))
    .sort((a, b) => (b.summary?.total_bytes ?? 0) - (a.summary?.total_bytes ?? 0));

  // Source provenance — read from the first non-null summary. The
  // backend dispatches centrally based on USAGE_SOURCE env, so every
  // SIM in a single page render shares one source. Guard for the
  // legacy server that doesn't echo `source` yet — assume axiom-api.
  const source: string =
    sims.find((s) => s.summary?.source)?.summary?.source ?? 'axiom-api';
  const sourceLabel = source === 'gaussdb' ? 'GaussDB DWS · PROD' : 'rain Axiom API';
  // GaussDB chip green tint vs Axiom magenta — fast visual cue when
  // the operator flips USAGE_SOURCE between environments.
  const sourceChipColor = source === 'gaussdb' ? '#6ff2a0' : '#ff7de0';

  return (
    <HudPanel
      title="Usage Overview"
      subtitle={`last 30 days · live from ${sourceLabel}`}
      accent="#ff7de0"
      leading={<HudStatusLed color="#ff7de0" animate={loading} />}
      meta={
        <>
          <HudChip color={sourceChipColor}>from: {source}</HudChip>
          <HudChip color="#ff7de0">{ids.length} SIM{ids.length === 1 ? '' : 's'}</HudChip>
        </>
      }
    >
      {ids.length === 0 && (
        <div style={{ padding: 10, fontSize: 11, opacity: 0.7 }}>
          // no MSISDN/IMSI on this customer — paste known IMSIs below.
        </div>
      )}
      {sims.map(({ id, summary }) => (
        <div key={id} style={{
          padding: '8px 6px',
          borderTop: '1px solid rgba(255,125,224,0.12)',
        }}>
          <div style={{
            fontSize: 10, opacity: 0.75, marginBottom: 6,
            fontFamily: 'var(--font-mono, monospace)',
            display: 'flex', gap: 8, alignItems: 'baseline',
          }}>
            <span style={{ color: '#00f0ff' }}>{id}</span>
            {summary && (
              <span style={{ opacity: 0.6 }}>
                {summary.first_day} → {summary.last_day}
              </span>
            )}
            {!summary && !loading && (
              <span style={{ color: '#ffaa00' }}>// no data returned</span>
            )}
          </div>
          <div style={{
            // Auto-fit so the 4 tiles render in one row when the
            // panel sits in its own column (full page width), and
            // wrap to 2×2 when the panel shares a row with SIM
            // Diagnostics. Min tile width (140) keeps "1.74 GB" big
            // text legible without horizontal scroll.
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))',
            gap: 6,
          }}>
            <UsageKpiTile
              label="Total Data Usage"
              value={fmtBytes(summary?.total_bytes ?? 0)}
              colour="#ff3355"
              tooltip="Sum of bytes across the 30-day window."
            />
            <UsageKpiTile
              label="Avg Daily Usage"
              value={fmtBytes(summary?.avg_daily_bytes ?? 0)}
              colour="#00f0ff"
              tooltip="Total bytes ÷ active days. Zero when the SIM had no traffic."
            />
            <UsageKpiTile
              label="Active Days"
              value={String(summary?.active_days ?? 0)}
              colour="#c488ff"
              tooltip="Count of days where bytes > 0 in the 30-day window."
            />
            <UsageKpiTile
              label="Peak Daily Usage"
              value={fmtBytes(summary?.peak_daily_bytes ?? 0)}
              colour="#ffaa00"
              tooltip={summary?.peak_day ? `Peak on ${summary.peak_day}.` : 'Highest single-day byte total.'}
            />
          </div>
        </div>
      ))}
      {/* Manual IMSI override editor — kept here so an operator can
          paste known IMSIs when the 3-pivot cascade misses, then the
          Usage Overview repopulates from the override. Used to live
          inside the now-deleted CDRUsagePanel. */}
      {view.identity.id && <IMSIOverrideEditor customerID={view.identity.id} />}
    </HudPanel>
  );
}

// fmtBytes renders a byte count in the most readable scale (B/KB/MB/GB/TB).
// Uses 1000-based decimal (telco convention; 1 GB = 10^9 bytes), one
// decimal place above MB. Always returns a string with a unit so the
// tile never shows a bare number.
function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let v = n;
  let i = 0;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
    i++;
  }
  if (i === 0) return `${Math.round(v)} ${units[i]}`;
  if (i <= 2) return `${v.toFixed(0)} ${units[i]}`;
  return `${v.toFixed(2)} ${units[i]}`;
}

function UsageKpiTile({ label, value, colour, tooltip }: {
  readonly label: string;
  readonly value: string;
  readonly colour: string;
  readonly tooltip?: string;
}) {
  return (
    <div
      title={tooltip}
      style={{
        padding: '14px 16px',
        background: `linear-gradient(135deg, ${colour}, ${colour}cc)`,
        borderRadius: 4,
        fontFamily: 'var(--font-mono, monospace)',
        minHeight: 88,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'space-between',
      }}
    >
      <div style={{
        fontSize: 10, opacity: 0.85, color: '#fff',
        textTransform: 'capitalize', letterSpacing: '0.02em',
      }}>
        {label}
      </div>
      <div style={{
        fontSize: 32, lineHeight: 1.0,
        color: '#fff',
        fontFamily: 'var(--font-display, Orbitron, monospace)',
        textShadow: '0 1px 2px rgba(0,0,0,0.4)',
        fontWeight: 600,
      }}>
        {value}
      </div>
    </div>
  );
}

/* CDRUsageOverviewPanel + CDRUsagePanel + their helpers (OverviewTile,
   fmtGB) deleted on 2026-04-29. Both pulled from view.cdr_usage which
   was populated exclusively by the AWS Athena fetcher. Athena is
   deprecated as the rain CDR source — UsageOverviewLivePanel reads
   the rain Axiom HTTP API at /api/v1/customer/usage/summary?msisdn=X
   directly and now lives in ExtrasColumn under Chargebacks. The
   backend still defines the cdr_usage fetch but it's gated off by
   default via CUSTOMER360_ATHENA_ENABLED — set =true to re-enable. */

/* ---- Manual IMSI override editor ----
   When our 3-pivot IMSI resolver (billing-account → msisdn →
   subscriber) can't find a customer's SIMs but the operator knows
   them, they paste them here. Saved to SQLite and used on every
   subsequent lookup — Usage + CDR panels populate from the
   supplied IMSIs directly. Collapsed by default so it doesn't
   clutter the page; click "manage" to reveal. */
function IMSIOverrideEditor({ customerID, onSaved }: {
  readonly customerID: string;
  readonly onSaved?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState<string[] | null>(null);
  // Lazily load current override when panel opens.
  useEffect(() => {
    if (!open || saved !== null) return;
    void (async () => {
      const r = await getIMSIOverride(customerID);
      const list = r?.imsis ?? [];
      setSaved(list);
      setValue(list.join(', '));
    })();
  }, [open, saved, customerID]);
  const handleSave = async () => {
    setSaving(true);
    try {
      const parsed = value
        .split(/[,\s;|\n]+/)
        .map((s) => s.trim())
        .filter(Boolean);
      const r = await setIMSIOverride(customerID, parsed);
      setSaved(r?.imsis ?? []);
      onSaved?.();
    } finally {
      setSaving(false);
    }
  };
  return (
    <div style={{
      padding: 6, marginTop: 6, fontSize: 10,
      borderTop: '1px dashed rgba(124,198,255,0.15)',
    }}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        style={{
          background: 'transparent', border: '1px solid #7cc6ff55',
          color: '#7cc6ff', padding: '3px 8px', fontSize: 9,
          textTransform: 'uppercase', letterSpacing: '0.08em',
          borderRadius: 3, cursor: 'pointer', fontFamily: 'inherit',
        }}
      >
        {open ? 'hide' : 'manage'} IMSI override
        {saved && saved.length > 0 ? ` · ${saved.length} saved` : ''}
      </button>
      {open && (
        <div style={{ marginTop: 6 }}>
          <div style={{ opacity: 0.7, marginBottom: 4 }}>
            Comma-separated IMSIs. Overrides the automatic pivot.
            Leave empty to clear.
          </div>
          <textarea
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="655380004807362, 655380004807363, 655380005322460"
            rows={2}
            style={{
              width: '100%', fontSize: 11,
              fontFamily: 'var(--font-mono, monospace)',
              background: 'rgba(0,0,0,0.3)',
              border: '1px solid #7cc6ff33', color: '#c7e9ff',
              padding: 6, borderRadius: 3, resize: 'vertical',
            }}
          />
          <button
            type="button"
            onClick={() => { void handleSave(); }}
            disabled={saving}
            style={{
              background: 'transparent', border: '1px solid #6ff2a066',
              color: '#6ff2a0', padding: '4px 10px', fontSize: 9,
              textTransform: 'uppercase', letterSpacing: '0.08em',
              borderRadius: 3, cursor: saving ? 'wait' : 'pointer',
              marginTop: 4, fontFamily: 'inherit',
            }}
          >
            {saving ? 'saving…' : 'save'}
          </button>
          {saved && saved.length > 0 && (
            <span style={{ marginLeft: 8, opacity: 0.75 }}>
              saved {saved.length}: {saved.slice(0, 3).join(', ')}
              {saved.length > 3 ? '…' : ''}
            </span>
          )}
          <div style={{ marginTop: 4, opacity: 0.6, fontSize: 9 }}>
            Reload or re-run the lookup to use the new list.
          </div>
        </div>
      )}
    </div>
  );
}

function UsagePanel({ view }: { readonly view: Customer360 }) {
  const usage = view.usage ?? [];
  const status = (view.data_sources ?? []).find((s) => s.name === 'usage');
  if (usage.length === 0) {
    return (
      <HudPanel
        title="Usage"
        accent="#6ff2a0"
        leading={<HudStatusLed color="#7cc6ff" />}
        meta={<HudChip color="#7cc6ff">empty</HudChip>}
      >
        <div style={{ fontSize: 11, opacity: 0.7, padding: 6 }}>
          // no resource_policy rows for this customer's MSISDNs
          {status?.latency_ms ? ` (probed in ${status.latency_ms}ms)` : ''}
        </div>
      </HudPanel>
    );
  }
  return (
    <HudPanel
      title={`Usage · ${usage.length}`}
      accent="#6ff2a0"
      leading={<HudStatusLed color="#6ff2a0" animate={false} />}
    >
      {usage.map((u) => {
        const { pct, quotaBytes, loadBytes } = parseQuota(u.quota, u.load);
        const color = pct >= 90 ? '#ff7b7b' : pct >= 70 ? '#ffaa00' : '#6ff2a0';
        return (
          <div key={u.msisdn} style={{
            padding: '6px 8px',
            borderLeft: `2px solid ${color}55`,
            marginBottom: 4,
            fontFamily: 'var(--font-mono, monospace)',
            fontSize: 11,
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 6 }}>
              <span style={{ color: '#00f0ff' }}>{u.msisdn}</span>
              <HudChip color={color}>{u.quota_status || '—'}</HudChip>
            </div>
            {u.policy_name && (
              <div style={{ fontSize: 10, opacity: 0.8, marginTop: 2 }}>
                {u.policy_name}
                {u.service_name && u.service_name !== u.policy_name ? ' · ' + u.service_name : ''}
              </div>
            )}
            {quotaBytes > 0 && (
              <div style={{
                marginTop: 4, height: 5,
                borderRadius: 2,
                background: 'rgba(124,198,255,0.15)',
                overflow: 'hidden',
              }}>
                <div style={{
                  width: `${Math.min(100, pct)}%`,
                  height: '100%',
                  background: color,
                  transition: 'width 200ms ease',
                }} />
              </div>
            )}
            <div style={{ fontSize: 10, opacity: 0.7, marginTop: 2, display: 'flex', justifyContent: 'space-between' }}>
              <span>{formatBytes(loadBytes)} / {formatBytes(quotaBytes)}</span>
              <span>{u.updated_at ? formatDate(u.updated_at) : ''}</span>
            </div>
          </div>
        );
      })}
    </HudPanel>
  );
}

// Rain's resource_policy stores quota + load as strings in bytes
// (sometimes with a unit suffix like "10G" or "500M"). Parse both and
// compute a percentage — returns zeros when the fields are empty.
function parseQuota(quota?: string, load?: string) {
  const quotaBytes = parseBytes(quota);
  const loadBytes = parseBytes(load);
  const pct = quotaBytes > 0 ? Math.round((loadBytes / quotaBytes) * 100) : 0;
  return { pct, quotaBytes, loadBytes };
}
function parseBytes(v?: string): number {
  if (!v) return 0;
  const m = v.trim().match(/^([\d.]+)\s*([kmgtKMGT]?)[bB]?$/);
  if (!m) {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }
  const n = parseFloat(m[1]);
  const mult: Record<string, number> = {
    '': 1, K: 1024, M: 1024 ** 2, G: 1024 ** 3, T: 1024 ** 4,
  };
  return Math.round(n * (mult[m[2].toUpperCase()] ?? 1));
}
function formatBytes(bytes: number): string {
  if (!bytes) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

/* ---- Billing accounts panel ---- */
function BillingPanel({ view }: { readonly view: Customer360 }) {
  const bas = view.billing_accounts ?? [];
  if (bas.length === 0) return null;
  return (
    <HudPanel
      title={`Billing accounts · ${bas.length}`}
      accent="#00f0ff"
      icon={<CreditCard size={12} />}
      leading={<HudStatusLed color="#00f0ff" />}
    >
      {bas.map((b) => {
        const badColor = /SUSPEND|DELINQ/.test(b.payment_status.toUpperCase()) ? '#ff7b7b' : '#6ff2a0';
        return (
          <div key={b.id} style={{
            padding: '6px 8px', borderLeft: `2px solid ${badColor}55`,
            fontFamily: 'var(--font-mono, monospace)', fontSize: 11, marginBottom: 4,
          }}>
            <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', justifyContent: 'space-between' }}>
              <span style={{ color: '#00f0ff' }}>{b.name}</span>
              <HudChip color={badColor}>{b.payment_status || '—'}</HudChip>
            </div>
            <div style={{ fontSize: 10, opacity: 0.8, marginTop: 2 }}>
              {b.account_type} · {b.state} · credit R{b.credit_limit.toFixed(0)} · pay day {b.payment_day}
            </div>
            {b.financial_account_id && (
              <div style={{ fontSize: 9, opacity: 0.5 }}>finAcct {b.financial_account_id.slice(0, 8)}…</div>
            )}
          </div>
        );
      })}
    </HudPanel>
  );
}

/* ---- Balances panel ---- */
function BalancesPanel({ view }: { readonly view: Customer360 }) {
  const bals = view.balances ?? [];
  if (bals.length === 0) return null;
  return (
    <HudPanel title={`Balances · ${bals.length}`} accent="#ffaa00" icon={<Banknote size={12} />}>
      {bals.map((b, i) => (
        <div key={`${b.balance_type}-${i}`} style={{
          display: 'grid', gridTemplateColumns: '1fr auto', gap: 6, padding: '3px 6px',
          fontSize: 11, fontFamily: 'var(--font-mono, monospace)',
        }}>
          <span style={{ color: '#ffaa00' }}>{b.balance_type}</span>
          <span style={{ color: b.amount < 0 ? '#ff7b7b' : '#6ff2a0' }}>R{b.amount.toFixed(2)}</span>
        </div>
      ))}
    </HudPanel>
  );
}

/* ---- Invoices panel ---- */
function InvoicesPanel({ view }: { readonly view: Customer360 }) {
  const inv = view.invoices ?? [];
  if (inv.length === 0) return null;
  return (
    <HudPanel title={`Invoices · ${inv.length}`} accent="#c488ff" icon={<Banknote size={12} />}>
      {inv.slice(0, 10).map((i) => (
        <div key={i.invoice_number + i.invoice_date} style={{
          display: 'grid', gridTemplateColumns: '1fr auto auto', gap: 6, padding: '3px 6px',
          fontSize: 11, fontFamily: 'var(--font-mono, monospace)',
        }}>
          <span>{i.invoice_number || '(no #)'}</span>
          <span style={{ opacity: 0.7 }}>{formatDate(i.invoice_date)}</span>
          <span style={{ color: i.balance > 0 ? '#ffaa00' : '#6ff2a0' }}>R{i.amount.toFixed(2)}</span>
        </div>
      ))}
    </HudPanel>
  );
}

/* ---- Promise to pay panel ---- */
function PromisesPanel({ view }: { readonly view: Customer360 }) {
  const pros = view.promises ?? [];
  if (pros.length === 0) return null;
  return (
    <HudPanel title={`Promises to pay · ${pros.length}`} accent="#ffe08a">
      {pros.map((p) => {
        const color = /BROKEN|DEFAULT/.test(p.status.toUpperCase()) ? '#ff7b7b' :
                      /ACTIVE|OPEN/.test(p.status.toUpperCase()) ? '#ffe08a' : '#6ff2a0';
        return (
          <div key={p.id} style={{
            padding: '6px 8px', borderLeft: `2px solid ${color}55`,
            fontFamily: 'var(--font-mono, monospace)', fontSize: 11, marginBottom: 4,
          }}>
            <div style={{ display: 'flex', gap: 6, justifyContent: 'space-between' }}>
              <HudChip color={color}>{p.status}</HudChip>
              <span>R{p.total_amount.toFixed(2)} / {p.number_of_payments}×</span>
            </div>
            <div style={{ fontSize: 10, opacity: 0.75, marginTop: 2 }}>
              R{p.installment_amount.toFixed(2)} {p.payment_frequency || ''} · balance R{p.balance.toFixed(2)}
            </div>
          </div>
        );
      })}
    </HudPanel>
  );
}

/* ---- Notifications panel ---- */
function NotificationsPanel({ view }: { readonly view: Customer360 }) {
  const notifs = view.recent_notifications ?? [];
  if (notifs.length === 0) return null;
  return (
    <HudPanel title={`Recent SMS · ${notifs.length}`} accent="#7cc6ff">
      {notifs.slice(0, 10).map((n, i) => (
        <div key={i} style={{
          padding: '4px 6px', fontSize: 11, borderBottom: '1px dashed rgba(124,198,255,0.1)',
        }}>
          <div style={{ display: 'flex', gap: 6, opacity: 0.7 }}>
            <span>{formatDate(n.inserted_at)}</span>
            <span>{n.msisdn}</span>
          </div>
          <div style={{ marginTop: 2, fontFamily: 'var(--font-mono, monospace)' }}>
            {n.message?.slice(0, 120) ?? ''}
          </div>
        </div>
      ))}
    </HudPanel>
  );
}

/* ---- Data sources audit panel ---- */
function DataSourcesPanel({ view }: { readonly view: Customer360 }) {
  const sources = view.data_sources ?? [];
  if (sources.length === 0) return null;
  const stateColor: Record<string, string> = {
    ok: '#6ff2a0', empty: '#7cc6ff', error: '#ff7b7b', skipped: '#ffaa00',
  };
  const okCount = sources.filter((s) => s.state === 'ok' || s.state === 'empty').length;
  return (
    <HudPanel
      title="Data sources"
      subtitle={`${okCount}/${sources.length} reachable · evidence of coverage`}
      accent="#7cc6ff"
    >
      {sources.map((s) => (
        <div
          key={`${s.database}:${s.name}`}
          title={s.error || ''}
          style={{
            display: 'grid',
            gridTemplateColumns: '12px 1fr auto auto',
            gap: 6, alignItems: 'center',
            padding: '3px 6px', fontSize: 11,
            borderLeft: `2px solid ${stateColor[s.state] || '#7cc6ff'}55`,
            fontFamily: 'var(--font-mono, monospace)',
          }}
        >
          <HudStatusLed color={stateColor[s.state] || '#7cc6ff'} />
          <span>{s.database}.{s.name}</span>
          <span style={{ opacity: 0.7 }}>{s.rows}r</span>
          <span style={{ opacity: 0.5 }}>{s.latency_ms ?? 0}ms</span>
        </div>
      ))}
    </HudPanel>
  );
}

/* ---- Candidate picker ----
   Rendered when a phone / email lookup matches more than one party.
   individual. Common on family plans where the same phone is shared
   across several accounts. Clicking a card re-submits the lookup
   with mode=id against the chosen individual. */
function CandidatePicker({
  candidates, query, onPick, loading,
}: {
  readonly candidates: IdentityCandidate[];
  readonly query: string;
  readonly onPick: (id: string) => void;
  readonly loading: boolean;
}) {
  return (
    <HudPanel
      title={`Multiple matches for "${query}" · pick one`}
      accent="#ffaa00"
      leading={<HudStatusLed color="#ffaa00" />}
      meta={<HudChip color="#ffaa00">{candidates.length} candidates</HudChip>}
    >
      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(min(240px, 100%), 1fr))',
        gap: 10,
        padding: 8,
      }}>
        {candidates.map((c) => (
          <button
            type="button"
            key={c.id}
            onClick={() => onPick(c.id)}
            disabled={loading}
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'flex-start',
              gap: 4,
              padding: 12,
              textAlign: 'left',
              background: 'rgba(255,170,0,0.06)',
              border: '1px solid #ffaa0044',
              borderRadius: 4,
              cursor: loading ? 'wait' : 'pointer',
              fontFamily: 'var(--font-mono, monospace)',
              color: 'inherit',
              opacity: loading ? 0.6 : 1,
            }}
          >
            <span style={{ fontSize: 9, opacity: 0.6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>
              {c.account_number ? `acct ${c.account_number}` : c.id}
            </span>
            <span style={{ fontSize: 14, color: '#ffaa00' }}>
              {c.full_name || `${c.given_name} ${c.family_name}`.trim() || (c.msisdn ? `SIM ${c.msisdn}` : '(no name)')}
            </span>
            {c.email && (
              <span style={{ fontSize: 10.5, opacity: 0.85, wordBreak: 'break-all' }}>
                {c.email}
              </span>
            )}
            {c.msisdn && c.account_number && (
              <span style={{ fontSize: 10, opacity: 0.75 }}>
                MSISDN {c.msisdn}
              </span>
            )}
            {c.source && (
              <span style={{ fontSize: 9, opacity: 0.5 }}>
                via {c.source === 'sim_view' ? 'SIM inventory' : 'contact medium'}
              </span>
            )}
            {c.created_at && !c.created_at.startsWith('0001') && (
              <span style={{ fontSize: 9, opacity: 0.55 }}>
                created {formatDate(c.created_at)}
              </span>
            )}
          </button>
        ))}
      </div>
    </HudPanel>
  );
}

/* ---- SIM Diagnostics panel ---------------------------------------------
   Phase 4 of docs/axiom/sim-diagnostics-plan.md. Reads
   view.sim_diagnostics — one IMSISource per IMSI returned by the
   backend's resolveIMSIs cascade, tagged with the winning phase.
   The panel's job is to make the cascade visible: which phase produced
   which IMSI, which phases didn't contribute, what to do when the
   cascade returns empty (BSS GAP case + [set override] affordance).

   Accent #b980ff violet — the "PII / diagnostic realm" colour, shared
   with the «redacted» chip in Axiom Explorer (Phase 0a). Distinct from
   the existing cyan/green/amber so ops doesn't confuse it with a
   warning banner (design-review D1).

   Information hierarchy (design-review D2): IMSI leads — the answer
   ops came for. MSISDN/ICCID demoted to subtitle. Phase-source row
   below as a tag-chip strip showing which phase contributed (filled)
   vs not (dim outline) (design-review D5).

   Empty-state: distinguish NOT PROVISIONED / SWAP IN FLIGHT /
   BSS GAP — OVERRIDE per design-review D3. Phase 4 only renders
   the BSS GAP case since swap-detection plumbing lands in Phase 5+. */

const SIM_DIAG_ACCENT = '#b980ff';

const CASCADE_PHASES = [
  { id: 'override',        label: 'override' },
  { id: 'product_path',    label: 'product' },
  { id: 'view_account',    label: 'view-acct' },
  { id: 'view_msisdn',     label: 'view-msisdn' },
  { id: 'view_subscriber', label: 'view-user' },
] as const;

function CorrelateProduct({ products, imsi }: {
  readonly products: readonly CustomerProduct[];
  readonly imsi: number;
}) {
  // The IMSISource record only carries the IMSI int. MSISDN, ICCID,
  // status come from the existing products[] slice when the same
  // SIM was returned by the product/jt_prod_rs_ref join. If we
  // can't correlate, render a subtle "—" instead of misleading
  // empty fields. Common in service-domain customers where the
  // cascade resolved an IMSI via the view but didn't surface the
  // matching product row.
  const imsiStr = String(imsi);
  const match = products.find((p) => p.imsi === imsiStr);
  if (!match) {
    return (
      <div style={{ fontSize: 10, opacity: 0.55 }}>
        no matching product row · IMSI from view-only path
      </div>
    );
  }
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, fontSize: 10, opacity: 0.85 }}>
      {match.msisdn && <span>MSISDN <b>{match.msisdn}</b></span>}
      {match.iccid && <span>ICCID <b>{match.iccid}</b></span>}
      {match.imei && <span>IMEI <b>{match.imei}</b></span>}
      {match.state && <HudChip color={productStateColor(match.state)}>{match.state}</HudChip>}
    </div>
  );
}

function PhaseChipRow({ winning }: { readonly winning: string }) {
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 6 }}>
      {CASCADE_PHASES.map((p) => {
        const filled = p.id === winning;
        return (
          <span
            key={p.id}
            title={filled ? `Phase ${p.id} resolved this IMSI` : `Phase ${p.id} did not contribute`}
            style={{
              padding: '1px 6px',
              fontSize: 9,
              letterSpacing: '0.06em',
              textTransform: 'uppercase',
              borderRadius: 3,
              border: `1px solid ${filled ? SIM_DIAG_ACCENT : 'rgba(185, 128, 255, 0.25)'}`,
              background: filled ? 'rgba(185, 128, 255, 0.18)' : 'transparent',
              color: filled ? SIM_DIAG_ACCENT : 'rgba(185, 128, 255, 0.55)',
              fontFamily: 'var(--font-mono, monospace)',
            }}
          >
            {filled ? '■' : '□'} {p.label}
          </span>
        );
      })}
    </div>
  );
}

function SimDiagnosticsRow({ src, products }: {
  readonly src: { imsi: number; source: string; resolved_at: string };
  readonly products: readonly CustomerProduct[];
}) {
  return (
    <div
      style={{
        padding: '8px 10px',
        marginBottom: 8,
        border: `1px solid rgba(185, 128, 255, 0.25)`,
        borderRadius: 5,
        background: 'rgba(185, 128, 255, 0.04)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
        <span style={{
          fontSize: 13,
          fontFamily: 'var(--font-mono, monospace)',
          fontVariantNumeric: 'tabular-nums',
          color: SIM_DIAG_ACCENT,
          letterSpacing: '0.04em',
        }}>
          IMSI <b>{String(src.imsi)}</b>
        </span>
        <button
          type="button"
          onClick={() => copyToClipboard(String(src.imsi))}
          title="Copy IMSI to clipboard"
          style={{
            padding: '1px 8px',
            fontSize: 9,
            background: 'transparent',
            color: SIM_DIAG_ACCENT,
            border: `1px solid rgba(185, 128, 255, 0.45)`,
            borderRadius: 3,
            cursor: 'pointer',
            fontFamily: 'var(--font-mono, monospace)',
          }}
        >
          copy
        </button>
      </div>
      <div style={{ marginTop: 4 }}>
        <CorrelateProduct products={products} imsi={src.imsi} />
      </div>
      <PhaseChipRow winning={src.source} />
    </div>
  );
}

function SimDiagnosticsEmptyState({ customerID, hasBilling }: {
  readonly customerID: string;
  readonly hasBilling: boolean;
}) {
  // Phase 4 renders the BSS-GAP case only. NOT PROVISIONED and
  // SWAP IN FLIGHT need backend signals (logistics.order, recon
  // disagreement) that arrive in later phases. Until then any
  // empty result with billing accounts present is "BSS GAP".
  const tag = hasBilling ? 'BSS GAP — OVERRIDE' : 'NO ACCOUNT';
  const explanation = hasBilling
    ? 'cascade returned empty, no override set. likely: service-domain customer outside view, or pre-provisioning.'
    : 'no billing account on this customer — cascade has nothing to walk.';
  const [showOverride, setShowOverride] = useState(false);
  return (
    <div
      style={{
        padding: '10px 12px',
        border: `1px solid rgba(185, 128, 255, 0.35)`,
        borderRadius: 5,
        background: 'rgba(185, 128, 255, 0.06)',
      }}
    >
      <div style={{
        display: 'inline-block',
        padding: '2px 8px',
        marginBottom: 6,
        fontSize: 9,
        letterSpacing: '0.1em',
        textTransform: 'uppercase',
        background: 'rgba(185, 128, 255, 0.18)',
        color: SIM_DIAG_ACCENT,
        border: `1px solid ${SIM_DIAG_ACCENT}`,
        borderRadius: 3,
        fontFamily: 'var(--font-mono, monospace)',
      }}>
        {tag}
      </div>
      <div style={{ fontSize: 11, opacity: 0.85, marginBottom: 6 }}>
        {explanation}
      </div>
      <PhaseChipRow winning="exhausted" />
      {hasBilling && (
        <div style={{ marginTop: 8 }}>
          <button
            type="button"
            onClick={() => setShowOverride(true)}
            disabled={!customerID}
            style={{
              padding: '4px 10px',
              fontSize: 10,
              letterSpacing: '0.06em',
              textTransform: 'uppercase',
              color: SIM_DIAG_ACCENT,
              background: 'transparent',
              border: `1px solid ${SIM_DIAG_ACCENT}`,
              borderRadius: 3,
              cursor: customerID ? 'pointer' : 'not-allowed',
              fontFamily: 'var(--font-mono, monospace)',
            }}
          >
            set override
          </button>
        </div>
      )}
      {showOverride && customerID && (
        <SimOverrideModal
          customerID={customerID}
          onClose={() => setShowOverride(false)}
        />
      )}
    </div>
  );
}

/* ---- Override modal (Phase 6) ----
   Backed by PUT /api/v1/customer/{id}/imsi-override which is gated on
   RAIN_SUPPORT_L2=true server-side. If the env flag is missing, the
   backend returns 403 — the modal surfaces the message verbatim so
   the operator knows it's a runtime gate, not a UI bug. */
function SimOverrideModal({ customerID, onClose }: {
  readonly customerID: string;
  readonly onClose: () => void;
}) {
  const [raw, setRaw] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<{ count: number } | null>(null);

  const submit = async () => {
    const imsis = raw.split(/[\s,;|]+/).map((s) => s.trim()).filter(Boolean);
    if (imsis.length === 0) {
      setError('paste at least one IMSI');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const result = await setIMSIOverride(customerID, imsis);
      if (!result) {
        setError('save failed — server returned no body. RAIN_SUPPORT_L2 likely off.');
      } else {
        setDone({ count: result.count });
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'unknown error');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      role="dialog"
      aria-label="Set IMSI override"
      style={{
        position: 'fixed', inset: 0, zIndex: 1000,
        background: 'rgba(0, 0, 0, 0.7)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
      }}
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(520px, 92vw)',
          padding: 20,
          background: 'rgba(10, 16, 30, 0.95)',
          border: `1px solid ${SIM_DIAG_ACCENT}`,
          borderRadius: 8,
          fontFamily: 'var(--font-mono, monospace)',
        }}
      >
        <div style={{ fontSize: 12, color: SIM_DIAG_ACCENT, letterSpacing: '0.1em', textTransform: 'uppercase', marginBottom: 10 }}>
          Set IMSI override · customer {customerID.slice(0, 8)}…
        </div>
        {done ? (
          <>
            <div style={{ fontSize: 11, color: '#6ff2a0', marginBottom: 10 }}>
              ✓ saved {done.count} IMSI{done.count === 1 ? '' : 's'}. Re-run the lookup to see them in the cascade.
            </div>
            <div style={{ textAlign: 'right' }}>
              <button onClick={onClose} type="button" style={modalBtnStyle()}>close</button>
            </div>
          </>
        ) : (
          <>
            <div style={{ fontSize: 10, opacity: 0.75, marginBottom: 8 }}>
              POPIA: every override write is audited. Paste IMSIs separated by space, comma, or pipe.
            </div>
            <textarea
              value={raw}
              onChange={(e) => setRaw(e.target.value)}
              rows={4}
              placeholder="655380004807362, 655380004791850"
              style={{
                width: '100%', padding: 8,
                background: 'rgba(0, 0, 0, 0.4)',
                color: '#cce6ff',
                border: `1px solid rgba(185, 128, 255, 0.35)`,
                borderRadius: 4, fontSize: 11,
                fontFamily: 'var(--font-mono, monospace)',
              }}
            />
            {error && (
              <div style={{ marginTop: 8, padding: 8, fontSize: 10, color: '#ff7b7b', background: 'rgba(255, 51, 85, 0.08)', border: '1px solid rgba(255, 51, 85, 0.3)', borderRadius: 4 }}>
                {error}
              </div>
            )}
            <div style={{ marginTop: 12, display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
              <button onClick={onClose} type="button" style={modalBtnStyle('cancel')}>cancel</button>
              <button onClick={submit} type="button" disabled={submitting || !raw.trim()} style={modalBtnStyle()}>
                {submitting ? 'saving…' : 'save override'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function modalBtnStyle(kind: 'confirm' | 'cancel' = 'confirm'): React.CSSProperties {
  return {
    padding: '5px 12px',
    fontSize: 10,
    letterSpacing: '0.08em',
    textTransform: 'uppercase',
    background: kind === 'confirm' ? 'rgba(185, 128, 255, 0.18)' : 'transparent',
    color: kind === 'confirm' ? SIM_DIAG_ACCENT : '#7cc6ff',
    border: `1px solid ${kind === 'confirm' ? SIM_DIAG_ACCENT : 'rgba(124, 198, 255, 0.4)'}`,
    borderRadius: 3,
    cursor: 'pointer',
    fontFamily: 'var(--font-mono, monospace)',
  };
}

function SimDiagnosticsPanel({ view }: { readonly view: Customer360 }) {
  const diagnostics = view.sim_diagnostics ?? [];
  const products = view.products ?? [];
  const billing = view.billing_accounts ?? [];
  // Show the panel when we have either diagnostics OR billing — the
  // empty-state is the whole point of the panel for service-domain
  // customers where the cascade missed.
  if (diagnostics.length === 0 && billing.length === 0) {
    return null;
  }
  return (
    <HudPanel
      title={`SIM Diagnostics${diagnostics.length > 0 ? ` · ${diagnostics.length}` : ''}`}
      accent={SIM_DIAG_ACCENT}
      leading={<HudStatusLed color={SIM_DIAG_ACCENT} />}
      meta={
        diagnostics.length > 0
          ? <HudChip color={SIM_DIAG_ACCENT}>cascade resolved</HudChip>
          : <HudChip color="#ffaa00">cascade empty</HudChip>
      }
    >
      {diagnostics.length === 0 ? (
        <SimDiagnosticsEmptyState
          customerID={view.identity.id || ''}
          hasBilling={billing.length > 0}
        />
      ) : (
        <div>
          {diagnostics.map((src, i) => (
            <SimDiagnosticsRow key={`${src.imsi}-${i}`} src={src} products={products} />
          ))}
        </div>
      )}
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

  const pickCandidate = useCallback(async (id: string) => {
    setLoading(true);
    setError(null);
    try {
      const result = await lookupByID(id, activeConnID || undefined);
      if (!result) {
        setError(`No customer found for id ${id}`);
      } else {
        setView(result);
      }
    } finally {
      setLoading(false);
    }
  }, [activeConnID]);

  const segments = useMemo(() => {
    if (!view) return undefined;
    const pays = view.payments ?? [];
    const success = pays.filter((p) => /success|paid/i.test(p.status)).length;
    const failed = pays.filter((p) => /fail|declined/i.test(p.status)).length;
    const other = Math.max(0, pays.length - success - failed);
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

      {view && view.candidates && view.candidates.length > 0 && (
        <CandidatePicker
          candidates={view.candidates}
          query={query}
          onPick={pickCandidate}
          loading={loading}
        />
      )}

      {view && (!view.candidates || view.candidates.length === 0) && (
        <>
          {/* v2 sticky command bar — identity chip, alert badges,
              quick actions. Action handlers wire to existing backend
              endpoints (SMS via chat queue, case via /tasks, etc). */}
          <CommandBar
            view={view}
            predictions={view.predictions}
            stage={view.journey_stage}
            onSendSMS={() => {
              const phone = (view.contacts ?? []).find((c) => c.phone)?.phone;
              if (phone) {
                // Opens the default dialer / SMS app so agents can
                // send from their own device. Future: POST templated
                // message into /api/v1/chat/send.
                window.location.href = `sms:${phone}`;
              }
            }}
            onCreateCase={() => {
              const name = view.identity.full_name || view.identity.id;
              const title = encodeURIComponent(`Case for ${name}`);
              window.open(`/tasks?create=${title}`, '_blank');
            }}
            onOfferBundle={() => {
              document.getElementById('nba-panel')?.scrollIntoView({ behavior: 'smooth' });
            }}
            onArrangeCallback={() => {
              const name = view.identity.full_name || view.identity.id;
              const title = encodeURIComponent(`Callback: ${name}`);
              window.open(`/tasks?create=${title}`, '_blank');
            }}
            onExport={() => {
              const blob = new Blob([JSON.stringify(view, null, 2)], { type: 'application/json' });
              const url = URL.createObjectURL(blob);
              const a = document.createElement('a');
              a.href = url;
              a.download = `customer-${view.identity.id || 'export'}.json`;
              a.click();
              URL.revokeObjectURL(url);
            }}
          />
          <div className={styles.resultGrid}>
            <div className={styles.resultLeft}>
              {/* v2 decisioning stack goes first — anchors the agent
                  on customer value, risk, and action. */}
              <PredictionStackPanel predictions={view.predictions} />
              <JourneyStagePanel stage={view.journey_stage} />
              <div id="nba-panel">
                <NBAPanel
                  customerID={view.identity.id}
                  recommendations={view.recommendations}
                />
              </div>
              <IdentityPanel view={view} />
              <BillingPanel view={view} />
              <BalancesPanel view={view} />
              <ProductsPanel view={view} />
              <SimDiagnosticsPanel view={view} />
              {/* CDRUsageOverviewPanel + CDRUsagePanel removed —
                  Athena was the only data source for them and we no
                  longer query AWS Athena for CDR data. Live usage
                  now sits in ExtrasColumn under Chargebacks via
                  UsageOverviewLivePanel sourced from the rain Axiom
                  HTTP API (api.sit.rain.co.za). */}
              <UsagePanel view={view} />
              {/* ContactsPanel moved into ExtrasColumn below
                  UsageOverviewLivePanel so the customer-summary
                  stack (subs → tickets → chargebacks → usage →
                  contacts) sits in one scroll-free column. */}
              <PaymentsPanel view={view} />
              <InvoicesPanel view={view} />
              <DeepLinksPanel view={view} />
            </div>
            <div className={styles.resultRight}>
              <DataSourcesPanel view={view} />
              <PromisesPanel view={view} />
              <TimelinePanel events={view.timeline} />
              <HeatmapPanel heatmap={view.payment_heatmap} />
              <NotificationsPanel view={view} />
              <NeighboursPanel view={view} />
              <ExtrasColumn view={view} />
            </div>
          </div>
        </>
      )}

      {!view && !error && configured && (
        <HudPanel
          title="Idle"
          accent="#7cc6ff"
          leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        >
          <div className={styles.idle}>
            <Banknote size={28} />
            <span>Start by searching for a customer above.</span>
          </div>
        </HudPanel>
      )}
    </div>
  );
}
