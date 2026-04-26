/* NBAPanel — left-rail v2 component. Ranked recommendations with
   Accept / Dismiss / Snooze buttons. Each action POSTs to the
   backend which writes an audit row + flips the rec's status so
   the 7-day cooldown excludes it from future lookups. Client-side
   we optimistically remove the accepted/dismissed/snoozed card. */

import { useCallback, useState } from 'react';
import {
  Sparkles, CheckCircle2, XCircle, Clock, ArrowRight,
  Phone, MessageSquare, User,
} from 'lucide-react';
import HudPanel from '../../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../../components/shared/HudChip';
import { recordRecommendationAction } from '../../api/customer';
import type { CustomerRecommendation } from '../../types/api';

const TYPE_COLOUR: Record<string, string> = {
  retention_offer:    '#ff7de0',
  collections_action: '#ffaa00',
  upsell:             '#6ff2a0',
  service_action:     '#7cc6ff',
};

const CHANNEL_ICON: Record<string, React.ReactNode> = {
  sms:   <MessageSquare size={10} />,
  call:  <Phone size={10} />,
  email: <MessageSquare size={10} />,
  agent: <User size={10} />,
};

function fmtRand(v: number): string {
  if (v >= 1_000) return `R${(v / 1_000).toFixed(1)}K`;
  return `R${v.toFixed(0)}`;
}

function actionBtn(color: string, disabled: boolean): React.CSSProperties {
  return {
    display: 'inline-flex', alignItems: 'center', gap: 3,
    padding: '3px 7px', fontSize: 9,
    color, background: 'transparent',
    border: `1px solid ${color}66`, borderRadius: 3,
    textTransform: 'uppercase', letterSpacing: '0.06em',
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.4 : 1,
  };
}

export default function NBAPanel({
  customerID,
  recommendations,
}: {
  readonly customerID: string;
  readonly recommendations: CustomerRecommendation[] | null | undefined;
}) {
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState<string | null>(null);

  const handleAction = useCallback(
    async (recID: string, action: 'accept' | 'dismiss' | 'snooze') => {
      if (busy) return;
      setBusy(recID);
      try {
        await recordRecommendationAction(customerID, recID, { action });
        setDismissed((prev) => new Set(prev).add(recID));
      } finally {
        setBusy(null);
      }
    },
    [busy, customerID],
  );

  const recs = (recommendations ?? []).filter((r) => !dismissed.has(r.id) && r.status === 'presented');
  if (recs.length === 0) {
    return (
      <HudPanel
        title="Next Best Action"
        accent="#6ff2a0"
        icon={<Sparkles size={12} />}
        leading={<HudStatusLed color="#6ff2a0" animate={false} />}
        meta={<HudChip color="#7cc6ff">none</HudChip>}
      >
        <div style={{ fontSize: 11, opacity: 0.65, padding: 8 }}>
          // no actions recommended right now
        </div>
      </HudPanel>
    );
  }

  const topColour = TYPE_COLOUR[recs[0].type] ?? '#00f0ff';
  return (
    <HudPanel
      title="Next Best Action"
      accent={topColour}
      icon={<Sparkles size={12} />}
      leading={<HudStatusLed color={topColour} />}
      meta={<HudChip color={topColour}>{recs.length}</HudChip>}
    >
      <div style={{ display: 'grid', gap: 6, padding: 4 }}>
        {recs.map((r) => {
          const colour = TYPE_COLOUR[r.type] ?? '#7cc6ff';
          const disabled = busy === r.id;
          return (
            <div
              key={r.id}
              style={{
                padding: '8px 10px',
                borderLeft: `3px solid ${colour}`,
                background: 'rgba(0,240,255,0.03)',
                fontSize: 11, lineHeight: 1.45,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 6 }}>
                <span style={{ color: colour, fontSize: 12 }}>
                  <span style={{ fontSize: 9, opacity: 0.6, marginRight: 5 }}>#{r.priority_rank}</span>
                  {r.title}
                </span>
                <HudChip color={colour}>{fmtRand(r.expected_value)}</HudChip>
              </div>
              {r.description && (
                <div style={{ fontSize: 10, opacity: 0.75, marginTop: 2 }}>{r.description}</div>
              )}
              <div style={{ display: 'flex', gap: 5, fontSize: 9, opacity: 0.7, marginTop: 3, alignItems: 'center' }}>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 3 }}>
                  {CHANNEL_ICON[r.channel] ?? null} {r.channel}
                </span>
                <span>·</span>
                <span>cost {fmtRand(r.cost_estimate)}</span>
              </div>
              {r.reason_codes.length > 0 && (
                <div style={{ fontSize: 10, opacity: 0.7, marginTop: 3 }}>
                  <ArrowRight size={9} style={{ verticalAlign: 'middle' }} /> {r.reason_codes.join(' · ')}
                </div>
              )}
              <div style={{ display: 'flex', gap: 5, marginTop: 6 }}>
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => void handleAction(r.id, 'accept')}
                  style={actionBtn('#6ff2a0', disabled)}
                >
                  <CheckCircle2 size={10} /> accept
                </button>
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => void handleAction(r.id, 'snooze')}
                  style={actionBtn('#ffaa00', disabled)}
                >
                  <Clock size={10} /> snooze
                </button>
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => void handleAction(r.id, 'dismiss')}
                  style={actionBtn('#ff7b7b', disabled)}
                >
                  <XCircle size={10} /> dismiss
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </HudPanel>
  );
}
