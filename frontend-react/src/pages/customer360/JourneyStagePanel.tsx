/* JourneyStagePanel — left-rail v2 component. Shows the derived
   lifecycle stage with a colour + the triggering events. Colour
   maps onto the tone of each stage (recovery = red, loyalty =
   green, etc.). */

import { Milestone } from 'lucide-react';
import HudPanel from '../../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../../components/shared/HudChip';
import type { CustomerJourneyStage } from '../../types/api';

const STAGE_COLOUR: Record<string, string> = {
  Onboarding: '#7cc6ff',
  Activation: '#00f0ff',
  Growth:     '#6ff2a0',
  Friction:   '#ffaa00',
  Retention:  '#ff7de0',
  Recovery:   '#ff3355',
  Loyalty:    '#c488ff',
};

function fmtDate(iso?: string): string {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleDateString('en-ZA', {
      day: '2-digit', month: 'short', year: 'numeric',
    });
  } catch {
    return iso;
  }
}

export default function JourneyStagePanel({
  stage,
}: {
  readonly stage: CustomerJourneyStage | null | undefined;
}) {
  if (!stage) {
    return (
      <HudPanel
        title="Journey Stage"
        accent="#7cc6ff"
        leading={<HudStatusLed color="#7cc6ff" animate={false} />}
      >
        <div style={{ fontSize: 11, opacity: 0.65, padding: 8 }}>
          // stage resolves on lookup
        </div>
      </HudPanel>
    );
  }
  const colour = STAGE_COLOUR[stage.stage] ?? '#7cc6ff';
  return (
    <HudPanel
      title="Journey Stage"
      accent={colour}
      icon={<Milestone size={12} />}
      leading={<HudStatusLed color={colour} animate={stage.stage === 'Recovery'} />}
      meta={<HudChip color={colour}>{stage.stage.toUpperCase()}</HudChip>}
    >
      <div style={{ padding: '4px 8px' }}>
        <div style={{
          fontFamily: 'var(--font-display, Orbitron, monospace)',
          fontSize: 22, color: colour,
          textShadow: `0 0 10px ${colour}55`,
        }}>
          {stage.stage}
        </div>
        <div style={{ fontSize: 9, opacity: 0.6, marginTop: 2 }}>
          entered {fmtDate(stage.entered_at)}
        </div>
      </div>
      {stage.triggering_events && stage.triggering_events.length > 0 && (
        <div style={{
          padding: '6px 8px', marginTop: 6,
          borderTop: '1px solid rgba(124,198,255,0.15)',
          fontSize: 10, lineHeight: 1.4,
        }}>
          <div style={{
            fontSize: 9, textTransform: 'uppercase', letterSpacing: '0.08em',
            opacity: 0.65, marginBottom: 3,
          }}>
            triggering events
          </div>
          {stage.triggering_events.map((e, i) => (
            <div key={i} style={{ opacity: 0.85 }}>· {e}</div>
          ))}
        </div>
      )}
    </HudPanel>
  );
}
