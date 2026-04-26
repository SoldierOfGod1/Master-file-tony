/* ============================================================
   PatternsTile — D2 follow-up
   Cross-user aggregate ops telemetry. Privacy-safe by design:
   counts only, k-anonymity ≥3, RAIN_SUPPORT_L2 gated.
   ============================================================ */

import { useCallback, useEffect, useState } from 'react';
import HudPanel from '../components/shared/HudPanel';
import { HudChip } from '../components/shared/HudChip';
import {
  getPatternsAggregate,
  type PatternsAggregate,
} from '../api/patterns';

function Spark({ days }: { readonly days: ReadonlyArray<{ count: number }> }) {
  if (days.length === 0) {
    return <span style={{ opacity: 0.5, fontSize: 11 }}>no activity in window</span>;
  }
  const max = Math.max(1, ...days.map((d) => d.count));
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'flex-end',
        gap: 2,
        height: 32,
        marginTop: 4,
      }}
      title={days.map((d) => `${d.count}`).join(' · ')}
    >
      {days.map((d, i) => (
        <div
          key={i}
          style={{
            flex: 1,
            background: '#00f0ff',
            opacity: 0.65,
            height: `${Math.max(2, (d.count / max) * 100)}%`,
            borderRadius: 1,
          }}
        />
      ))}
    </div>
  );
}

export default function PatternsTile() {
  const [data, setData] = useState<PatternsAggregate | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    const result = await getPatternsAggregate();
    setLoading(false);
    if (!result) {
      setErr('aggregate disabled — set RAIN_SUPPORT_L2=true on the backend');
      return;
    }
    setErr(null);
    setData(result);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <HudPanel
      title="Cross-user patterns"
      accent="#b980ff"
      meta={
        data && (
          <HudChip color="#b980ff">
            k≥3 anon · counts only
          </HudChip>
        )
      }
    >
      <div style={{ padding: 12, fontFamily: 'var(--font-mono, monospace)', fontSize: 12 }}>
        {loading && <div style={{ opacity: 0.6 }}>loading…</div>}
        {err && (
          <div
            style={{
              color: '#ffaa00',
              padding: 8,
              border: '1px solid rgba(255, 170, 0, 0.4)',
              background: 'rgba(255, 170, 0, 0.06)',
              borderRadius: 4,
            }}
          >
            {err}
          </div>
        )}
        {data && (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <div>
                <div style={{ opacity: 0.6, fontSize: 10, letterSpacing: '0.06em', textTransform: 'uppercase' }}>
                  Active users · 7d
                </div>
                <div style={{ fontSize: 22, color: '#b980ff', marginTop: 2 }}>
                  {data.active_users_7d_suppressed ? '<3' : data.active_users_7d}
                </div>
                {data.active_users_7d_suppressed && (
                  <div style={{ fontSize: 9, opacity: 0.6, marginTop: 2 }}>
                    suppressed under k-anon
                  </div>
                )}
              </div>

              <div>
                <div style={{ opacity: 0.6, fontSize: 10, letterSpacing: '0.06em', textTransform: 'uppercase' }}>
                  Conversations · 30d
                </div>
                <Spark days={data.conversations_by_day} />
              </div>
            </div>

            <div style={{ marginTop: 14 }}>
              <div style={{ opacity: 0.6, fontSize: 10, letterSpacing: '0.06em', textTransform: 'uppercase', marginBottom: 4 }}>
                Memory entries by kind
              </div>
              {data.memory_by_kind.length === 0 ? (
                <div style={{ opacity: 0.5 }}>no entries yet</div>
              ) : (
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                  {data.memory_by_kind.map((k) => (
                    <HudChip key={k.kind} color="#7cc6ff">
                      {k.kind} · {k.count}
                    </HudChip>
                  ))}
                </div>
              )}
            </div>

            <div style={{ marginTop: 14 }}>
              <div style={{ opacity: 0.6, fontSize: 10, letterSpacing: '0.06em', textTransform: 'uppercase', marginBottom: 4 }}>
                Trending stems · k-anon ≥3
              </div>
              {data.top_keyword_stems.length === 0 ? (
                <div style={{ opacity: 0.5 }}>
                  no terms cleared k-anon yet — needs ≥3 distinct users sharing a stem
                </div>
              ) : (
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                  {data.top_keyword_stems.map((s) => (
                    <HudChip key={s.stem} color="#6ff2a0">
                      {s.stem} · {s.occurrences}× / {s.user_buckets}u
                    </HudChip>
                  ))}
                </div>
              )}
            </div>

            <div style={{ marginTop: 14, fontSize: 9, opacity: 0.5 }}>
              generated {new Date(data.generated_at).toLocaleString()}
            </div>
          </>
        )}
      </div>
    </HudPanel>
  );
}
