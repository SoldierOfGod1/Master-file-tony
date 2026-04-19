/* ============================================================
   ToolsHubPage — HUD grid of tool panels with a detail modal.
   ============================================================ */

import { useState, useCallback } from 'react';
import {
  MessageCircle,
  CreditCard,
  Radio,
  TrendingUp,
  Shield,
  Wallet,
  ScanLine,
  MapPin,
  Filter,
  Wrench,
  type LucideIcon,
} from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import Modal from '../components/shared/Modal';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import type { Tool } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './ToolsHubPage.module.css';

const ICON_MAP: Record<string, LucideIcon> = {
  chat: MessageCircle,
  card: CreditCard,
  antenna: Radio,
  trending: TrendingUp,
  shield: Shield,
  wallet: Wallet,
  scan: ScanLine,
  map: MapPin,
  funnel: Filter,
};

const resolveIcon = (k: string): LucideIcon =>
  ICON_MAP[k.toLowerCase()] ?? Wrench;

/* Map tool.status → an accent colour + LED tint used by the panel. */
interface StatusPal {
  readonly color: string;
  readonly label: string;
}
const STATUS: Record<string, StatusPal> = {
  active:  { color: '#6ff2a0', label: 'Active' },
  online:  { color: '#6ff2a0', label: 'Online' },
  planned: { color: '#ffaa00', label: 'Planned' },
  pending: { color: '#ffaa00', label: 'Pending' },
};
const palFor = (s: string): StatusPal =>
  STATUS[s.toLowerCase()] ?? { color: '#7cc6ff', label: s };

function ToolPanel({ tool, onClick }: {
  readonly tool: Tool;
  readonly onClick: (tool: Tool) => void;
}) {
  const pal = palFor(tool.status);
  const Icon = resolveIcon(tool.icon);
  return (
    <HudPanel
      title={tool.name}
      accent={pal.color}
      leading={<HudStatusLed color={pal.color} />}
      meta={<Icon size={11} />}
      onClick={() => onClick(tool)}
    >
      <div className={styles.body}>
        <p className={styles.desc}>{tool.desc}</p>
        <div className={styles.statusLine}>
          <HudChip color={pal.color}>{pal.label}</HudChip>
          {tool.agents.length > 0 && (
            <span className={styles.agentsHint}>
              {tool.agents.length} agent{tool.agents.length === 1 ? '' : 's'}
            </span>
          )}
        </div>
      </div>
    </HudPanel>
  );
}

function ToolDetail({ tool }: { readonly tool: Tool }) {
  const pal = palFor(tool.status);
  return (
    <div>
      <div className={styles.modalSection}>
        <div className={styles.modalSectionTitle}>Description</div>
        <p className={styles.modalText}>{tool.detail || tool.desc}</p>
      </div>
      <div className={styles.modalSection}>
        <div className={styles.modalSectionTitle}>Assigned Agents</div>
        <div className={styles.tagList}>
          {tool.agents.length > 0
            ? tool.agents.map((a) => <HudChip key={a} color="#00f0ff">{a}</HudChip>)
            : <span className={styles.modalText}>None assigned</span>}
        </div>
      </div>
      <div className={styles.modalSection}>
        <div className={styles.modalSectionTitle}>Rain Systems</div>
        <div className={styles.tagList}>
          {tool.systems.length > 0
            ? tool.systems.map((s) => <HudChip key={s} color="#ff7de0">{s}</HudChip>)
            : <span className={styles.modalText}>None linked</span>}
        </div>
      </div>
      <div className={styles.modalSection}>
        <div className={styles.modalSectionTitle}>Status</div>
        <HudChip color={pal.color}>{pal.label}</HudChip>
      </div>
      <button type="button" className={styles.requestBtn}>
        Request Implementation
      </button>
    </div>
  );
}

export default function ToolsHubPage() {
  const { state } = useCommandCentre();
  const [selected, setSelected] = useState<Tool | null>(null);

  const handleOpen = useCallback((tool: Tool) => setSelected(tool), []);
  const handleClose = useCallback(() => setSelected(null), []);

  const activeCount = state.tools.filter(
    (t) => t.status.toLowerCase() === 'active' || t.status.toLowerCase() === 'online',
  ).length;
  const ratio = state.tools.length === 0 ? 0 : activeCount / state.tools.length;

  const byStatus: Record<string, number> = {};
  for (const t of state.tools) {
    const k = t.status.toLowerCase();
    byStatus[k] = (byStatus[k] ?? 0) + 1;
  }
  const segments = Object.entries(byStatus).map(([k, v]) => ({
    label: STATUS[k]?.label ?? k,
    value: v,
    color: STATUS[k]?.color ?? '#7cc6ff',
  }));

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Tools Hub"
        subtitle={`${state.tools.length} tools · ${activeCount} active`}
        gaugeValue={ratio}
        gaugeReadout={`${activeCount}/${state.tools.length}`}
        gaugeLabel="ACTIVE"
        gaugeColor="#6ff2a0"
        segments={segments}
      />

      <div className={hudStyles.grid}>
        {state.tools.map((tool) => (
          <ToolPanel key={tool.id} tool={tool} onClick={handleOpen} />
        ))}
        {state.tools.length === 0 && (
          <span className={styles.empty}>No tools loaded yet.</span>
        )}
      </div>

      <Modal
        isOpen={selected !== null}
        onClose={handleClose}
        title={selected?.name}
      >
        {selected && <ToolDetail tool={selected} />}
      </Modal>
    </div>
  );
}
