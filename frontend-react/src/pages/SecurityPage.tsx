/* ============================================================
   SecurityPage — HUD trust score + alert breakdown.
   ============================================================ */

import { useMemo } from 'react';
import {
  ShieldCheck,
  AlertTriangle,
  AlertCircle,
  Info,
  BookOpen,
  Clock,
} from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import HudGauge from '../components/shared/HudGauge';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import hudStyles from '../theme/hud.module.css';
import styles from './SecurityPage.module.css';

function trustPalette(score: number): { color: string; status: string } {
  if (score >= 80) return { color: '#6ff2a0', status: 'Healthy' };
  if (score >= 50) return { color: '#ffaa00', status: 'Caution' };
  return { color: '#ff3355', status: 'Critical' };
}

export default function SecurityPage() {
  const { state } = useCommandCentre();
  const { security } = state;

  const trustScore = security?.trustScore ?? 0;
  const { color, status } = useMemo(() => trustPalette(trustScore), [trustScore]);

  const critical = security?.critical ?? 0;
  const warning = security?.warning ?? 0;
  const info = security?.info ?? 0;
  const totalAlerts = critical + warning + info;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Security Overview"
        subtitle={`Trust score ${trustScore}/100 · ${status} · ${totalAlerts} active alerts`}
        gaugeValue={trustScore / 100}
        gaugeReadout={`${trustScore}`}
        gaugeLabel="TRUST"
        gaugeColor={color}
        segments={[
          { label: 'Critical', value: critical, color: '#ff3355' },
          { label: 'Warning', value: warning, color: '#ffaa00' },
          { label: 'Info', value: info, color: '#7cc6ff' },
        ]}
      />

      <div className={styles.topRow}>
        <HudPanel
          title="Trust Score"
          accent={color}
          leading={<HudStatusLed color={color} />}
          meta={<ShieldCheck size={11} />}
          footer={<>// ruleset confidence · composite of alert severity + scan freshness</>}
        >
          <div className={styles.gaugeWrap}>
            <HudGauge
              value={trustScore / 100}
              readout={`${trustScore}`}
              label={status.toUpperCase()}
              color={color}
              size={200}
            />
          </div>
        </HudPanel>

        <HudPanel
          title="Alert Inventory"
          accent="#ff3355"
          leading={<HudStatusLed color={critical > 0 ? '#ff3355' : '#6ff2a0'} />}
          meta={<>{totalAlerts} active</>}
        >
          <div className={styles.alertList}>
            <div className={styles.alertRow}>
              <AlertCircle size={14} style={{ color: '#ff3355' }} />
              <span className={styles.alertLabel}>Critical</span>
              <HudChip color="#ff3355">{critical}</HudChip>
            </div>
            <div className={styles.alertRow}>
              <AlertTriangle size={14} style={{ color: '#ffaa00' }} />
              <span className={styles.alertLabel}>Warning</span>
              <HudChip color="#ffaa00">{warning}</HudChip>
            </div>
            <div className={styles.alertRow}>
              <Info size={14} style={{ color: '#7cc6ff' }} />
              <span className={styles.alertLabel}>Info</span>
              <HudChip color="#7cc6ff">{info}</HudChip>
            </div>
          </div>
        </HudPanel>
      </div>

      <div className={styles.bottomRow}>
        <HudPanel
          title="Active Rules"
          accent="#00f0ff"
          leading={<HudStatusLed color="#6ff2a0" />}
          meta={<BookOpen size={10} />}
        >
          <div className={styles.bigStat}>
            <span className={styles.bigValue}>{security?.rulesActive ?? 0}</span>
            <span className={styles.bigLabel}>Rules enforced</span>
          </div>
        </HudPanel>

        <HudPanel
          title="Last Scan"
          accent="#7cc6ff"
          leading={<HudStatusLed color="#7cc6ff" />}
          meta={<Clock size={10} />}
        >
          <div className={styles.bigStat}>
            <span className={styles.bigValueMono}>{security?.lastScan ?? 'Never'}</span>
            <span className={styles.bigLabel}>Scheduled · hourly</span>
          </div>
        </HudPanel>
      </div>
    </div>
  );
}
