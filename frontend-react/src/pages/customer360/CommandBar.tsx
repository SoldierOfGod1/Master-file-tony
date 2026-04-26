/* CommandBar — sticky top ribbon that combines identity chip,
   alert badges, and quick-action buttons. Replaces the bare
   lookup strip from v1. The lookup form stays on the page
   (below the bar) for discoverability — the bar is action-first. */

import { useMemo } from 'react';
import {
  User, AlertTriangle, Flame, Activity, Download, Phone,
  MessageSquare, PlusCircle, Calendar,
} from 'lucide-react';
import { HudChip, HudStatusLed } from '../../components/shared/HudChip';
import type {
  Customer360, CustomerPredictions, CustomerJourneyStage,
} from '../../types/api';

function fmtRandShort(v: number): string {
  if (v >= 1_000_000) return `R${(v / 1_000_000).toFixed(1)}M`;
  if (v >= 1_000) return `R${(v / 1_000).toFixed(1)}K`;
  return `R${v.toFixed(0)}`;
}

function badgeBtn(color: string): React.CSSProperties {
  return {
    display: 'inline-flex', alignItems: 'center', gap: 4,
    padding: '4px 9px', fontSize: 10,
    color, background: 'transparent',
    border: `1px solid ${color}66`, borderRadius: 4,
    textTransform: 'uppercase', letterSpacing: '0.06em',
    cursor: 'pointer', fontFamily: 'inherit',
  };
}

export default function CommandBar({
  view,
  predictions,
  stage,
  onSendSMS,
  onCreateCase,
  onOfferBundle,
  onArrangeCallback,
  onExport,
}: {
  readonly view: Customer360;
  readonly predictions?: CustomerPredictions | null;
  readonly stage?: CustomerJourneyStage | null;
  readonly onSendSMS?: () => void;
  readonly onCreateCase?: () => void;
  readonly onOfferBundle?: () => void;
  readonly onArrangeCallback?: () => void;
  readonly onExport?: () => void;
}) {
  const msisdn = useMemo(() => {
    const first = (view.contacts ?? []).find((c) => c.phone);
    return first?.phone ?? '';
  }, [view.contacts]);

  const accountCount = (view.billing_accounts ?? []).length;
  const churnPct = predictions ? Math.round(predictions.churn_30d * 100) : 0;
  const payPct = predictions ? Math.round(predictions.payment_default_30d * 100) : 0;
  const stageName = stage?.stage ?? 'Unknown';
  const stageColour =
    stageName === 'Recovery' ? '#ff3355' :
    stageName === 'Friction' ? '#ffaa00' :
    stageName === 'Retention' ? '#ff7de0' :
    stageName === 'Loyalty' ? '#c488ff' :
    stageName === 'Growth' ? '#6ff2a0' :
    '#7cc6ff';

  return (
    <div
      /* Flex-wrap instead of a fixed 3-column grid so the bar
         keeps working on a 1366 laptop and below: the three
         clusters (identity / badges / actions) wrap onto a
         second row when horizontal space is tight instead of
         squashing into each other. */
      style={{
        position: 'sticky', top: 0, zIndex: 50,
        background: 'rgba(5, 8, 16, 0.92)',
        backdropFilter: 'blur(10px)',
        borderBottom: '1px solid rgba(0, 240, 255, 0.18)',
        padding: '10px 14px',
        display: 'flex',
        flexWrap: 'wrap',
        gap: 12, alignItems: 'center',
        marginBottom: 12,
      }}
    >
      {/* Identity chip */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flex: '1 1 240px', minWidth: 0 }}>
        <div style={{
          width: 36, height: 36, borderRadius: 4,
          background: 'rgba(0,240,255,0.08)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          border: '1px solid rgba(0,240,255,0.35)',
        }}>
          <User size={18} color="#00f0ff" />
        </div>
        <div>
          <div style={{
            fontSize: 14, color: '#00f0ff',
            fontFamily: 'var(--font-display, Orbitron, monospace)',
          }}>
            {view.identity.full_name || '(unnamed customer)'}
          </div>
          <div style={{ fontSize: 10, opacity: 0.75, fontFamily: 'var(--font-mono, monospace)' }}>
            {msisdn && <span>{msisdn}</span>}
            {msisdn && accountCount > 0 && <span style={{ margin: '0 5px' }}>·</span>}
            {accountCount > 0 && <span>{accountCount} account{accountCount === 1 ? '' : 's'}</span>}
            {view.account_age.human_friendly && (
              <>
                <span style={{ margin: '0 5px' }}>·</span>
                <span>tenure {view.account_age.human_friendly}</span>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Alert badges */}
      <div style={{ display: 'flex', gap: 5, flexWrap: 'wrap', alignItems: 'center', flex: '1 1 280px', minWidth: 0 }}>
        <HudChip color={stageColour}>
          <Activity size={10} /> {stageName.toUpperCase()}
        </HudChip>
        {churnPct >= 40 && (
          <HudChip color={churnPct >= 70 ? '#ff3355' : '#ffaa00'}>
            <Flame size={10} /> CHURN {churnPct}%
          </HudChip>
        )}
        {payPct >= 40 && (
          <HudChip color={payPct >= 70 ? '#ff3355' : '#ffaa00'}>
            <AlertTriangle size={10} /> PAY {payPct}%
          </HudChip>
        )}
        {predictions && predictions.ltv_12m_expected > 0 && (
          <HudChip color="#6ff2a0">LTV {fmtRandShort(predictions.ltv_12m_expected)}</HudChip>
        )}
        <HudStatusLed color={stageColour} animate={stageName === 'Recovery' || churnPct >= 70} />
      </div>

      {/* Quick actions */}
      <div style={{ display: 'flex', gap: 5, flexWrap: 'wrap', flex: '0 1 auto' }}>
        {onSendSMS && (
          <button type="button" onClick={onSendSMS} style={badgeBtn('#7cc6ff')}>
            <MessageSquare size={10} /> SMS
          </button>
        )}
        {onCreateCase && (
          <button type="button" onClick={onCreateCase} style={badgeBtn('#ffaa00')}>
            <PlusCircle size={10} /> case
          </button>
        )}
        {onOfferBundle && (
          <button type="button" onClick={onOfferBundle} style={badgeBtn('#6ff2a0')}>
            <Flame size={10} /> offer
          </button>
        )}
        {onArrangeCallback && (
          <button type="button" onClick={onArrangeCallback} style={badgeBtn('#ff7de0')}>
            <Calendar size={10} /> callback
          </button>
        )}
        {onExport && (
          <button type="button" onClick={onExport} style={badgeBtn('#c488ff')}>
            <Download size={10} /> export
          </button>
        )}
      </div>
    </div>
  );
}

export { Phone };
