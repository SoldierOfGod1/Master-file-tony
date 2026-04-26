/* PredictionStackPanel — left-rail v2 component. Shows the four
   rules-based scores (churn 30/60/90, payment default, LTV12m,
   upsell) with a ribbon of reason codes underneath. When a future
   ML model drops in, only the `model_version` chip changes; the
   layout stays. */

import { AlertTriangle, Banknote, TrendingUp } from 'lucide-react';
import HudPanel from '../../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../../components/shared/HudChip';
import type { CustomerPredictions } from '../../types/api';

const RISK_COLOUR = (pct: number): string =>
  pct >= 0.7 ? '#ff3355'
  : pct >= 0.4 ? '#ffaa00'
  : pct >= 0.2 ? '#ffe08a'
  : '#6ff2a0';

function fmtPct(v: number): string {
  return `${(v * 100).toFixed(2)}%`;
}

function fmtRand(v: number): string {
  if (v >= 1_000_000) return `R${(v / 1_000_000).toFixed(2)}M`;
  if (v >= 1_000) return `R${(v / 1_000).toFixed(1)}K`;
  return `R${v.toFixed(0)}`;
}

function MiniBar({ value, color }: { readonly value: number; readonly color: string }) {
  return (
    <div style={{
      height: 4, borderRadius: 2, background: 'rgba(124,198,255,0.15)',
      overflow: 'hidden', marginTop: 3,
    }}>
      <div style={{
        width: `${Math.min(100, Math.max(0, value * 100))}%`,
        height: '100%', background: color, boxShadow: `0 0 4px ${color}`,
      }} />
    </div>
  );
}

function Row({
  label, value, color, suffix, tooltip,
}: {
  readonly label: string;
  readonly value: number;
  readonly color: string;
  readonly suffix?: string;
  readonly tooltip?: string;
}) {
  return (
    <div style={{ padding: '4px 6px' }} title={tooltip}>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10.5 }}>
        <span style={{ opacity: 0.85, display: 'inline-flex', alignItems: 'center', gap: 3 }}>
          {label}
          {/* Small help indicator mirrors the native tooltip; hover
              the row (or this glyph) on desktop to see the rule. */}
          {tooltip && (
            <span
              style={{
                fontSize: 8, opacity: 0.5,
                border: '1px solid currentColor',
                borderRadius: '50%',
                width: 11, height: 11,
                display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                fontFamily: 'var(--font-mono, monospace)',
                cursor: 'help',
              }}
              aria-label="how this is computed"
            >?</span>
          )}
        </span>
        <span style={{ color, fontFamily: 'var(--font-display, Orbitron, monospace)' }}>
          {suffix === 'R' ? fmtRand(value) : fmtPct(value)}
        </span>
      </div>
      {suffix !== 'R' && <MiniBar value={value} color={color} />}
    </div>
  );
}

// RULE_DOCS — human-readable explanations for each rules-based
// score. These mirror the predicates in backend/internal/customer/
// predictions.go so anyone hovering a tile sees what actually
// fired. Kept as a constant at module scope so the strings are
// stable across renders.
const RULE_DOCS = {
  churn30:
    'Churn 30-day (rules_v1). Weighted sum of:\n' +
    ' · 50%+ payment failure ratio → +0.35\n' +
    ' · 30%+ payment failure ratio → +0.20\n' +
    ' · no payment in 60+ days → +0.30\n' +
    ' · no payment in 30+ days → +0.15\n' +
    ' · broken promise-to-pay → +0.25\n' +
    ' · 3+ tickets in last 30d → +0.15\n' +
    ' · billing account suspended → +0.20\n' +
    ' · no recent usage data → +0.10\n' +
    'Capped at 100%. Reason codes below list which fired.',
  churn60:
    'Churn 60-day. Monotonically ≥ churn 30d (rule: churn30 + 0.08, capped). ' +
    'A real ML model would estimate the 60d window directly; rules_v1 just ' +
    'stretches the short-term signal.',
  churn90:
    'Churn 90-day. Monotonically ≥ churn 30d (rule: churn30 + 0.15, capped).',
  paymentDefault:
    'Payment default 30d (rules_v1). Weighted sum of:\n' +
    ' · payment failure ratio × 0.5\n' +
    ' · broken promise-to-pay → +0.30\n' +
    ' · outstanding balance > 1.5× last invoice → +0.20\n' +
    ' · outstanding balance > 0.9× last invoice → +0.10\n' +
    ' · days since last payment > 45 → +0.15\n' +
    'Capped at 100%.',
  ltv:
    'LTV 12-month expected (rules_v1). Formula:\n' +
    '    avg_monthly_revenue × 12 × survival\n' +
    'where:\n' +
    ' · avg_monthly_revenue = sum of successful payments last 90d ÷ 3\n' +
    ' · survival = 1 - (0.7 × churn30 + 0.3 × churn90)\n' +
    '   (floored at 5% so a very-at-risk customer still shows some value)\n' +
    'R0 means no successful payments logged in the last 90 days.',
  upsell:
    'Upsell propensity (rules_v1). Weighted sum of:\n' +
    ' · low churn (<25%) AND clean payment history (3+ successful) → +0.35\n' +
    ' · quota status "near" or "depleted" on any SIM → +0.20\n' +
    ' · tenure > 365 days → +0.10\n' +
    'Capped at 100%. 0% typically means the customer is at-risk ' +
    '(upsell suppressed) or has no payment history yet.',
} as const;

export default function PredictionStackPanel({
  predictions,
}: {
  readonly predictions: CustomerPredictions | null | undefined;
}) {
  if (!predictions) {
    return (
      <HudPanel
        title="Prediction Stack"
        accent="#7cc6ff"
        leading={<HudStatusLed color="#7cc6ff" animate={false} />}
        meta={<HudChip color="#7cc6ff">no scores yet</HudChip>}
      >
        <div style={{ fontSize: 11, opacity: 0.65, padding: 8 }}>
          // predictions populate on lookup
        </div>
      </HudPanel>
    );
  }
  const accent = RISK_COLOUR(predictions.churn_30d);
  return (
    <HudPanel
      title="Prediction Stack"
      accent={accent}
      icon={<TrendingUp size={12} />}
      leading={<HudStatusLed color={accent} animate={predictions.churn_30d >= 0.5} />}
      meta={
        <HudChip color="#7cc6ff">
          {predictions.model_version} · conf {fmtPct(predictions.confidence)}
        </HudChip>
      }
    >
      <Row label="Churn · 30d" value={predictions.churn_30d} color={RISK_COLOUR(predictions.churn_30d)} tooltip={RULE_DOCS.churn30} />
      <Row label="Churn · 60d" value={predictions.churn_60d} color={RISK_COLOUR(predictions.churn_60d)} tooltip={RULE_DOCS.churn60} />
      <Row label="Churn · 90d" value={predictions.churn_90d} color={RISK_COLOUR(predictions.churn_90d)} tooltip={RULE_DOCS.churn90} />
      <Row label="Payment default · 30d" value={predictions.payment_default_30d} color={RISK_COLOUR(predictions.payment_default_30d)} tooltip={RULE_DOCS.paymentDefault} />
      <Row label="LTV · 12m expected" value={predictions.ltv_12m_expected} color="#6ff2a0" suffix="R" tooltip={RULE_DOCS.ltv} />
      <Row label="Upsell propensity" value={predictions.upsell_propensity} color="#c488ff" tooltip={RULE_DOCS.upsell} />
      {predictions.reason_codes.length > 0 && (
        <div style={{
          padding: '6px 8px', marginTop: 6,
          borderTop: '1px solid rgba(124,198,255,0.15)',
          fontSize: 10, lineHeight: 1.4,
        }}>
          <div style={{
            fontSize: 9, textTransform: 'uppercase', letterSpacing: '0.08em',
            opacity: 0.65, marginBottom: 3,
          }}>
            <AlertTriangle size={9} style={{ verticalAlign: 'middle' }} /> reason codes
          </div>
          {predictions.reason_codes.slice(0, 6).map((r, i) => (
            <div key={i} style={{ opacity: 0.8 }}>· {r}</div>
          ))}
        </div>
      )}
    </HudPanel>
  );
}

export { fmtRand as formatRandShort, fmtPct as formatPercent };
export { Banknote };
