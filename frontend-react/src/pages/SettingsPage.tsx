import { useState } from 'react';
import { Database, ChevronDown, ChevronRight } from 'lucide-react';
import ChatSettingsSection from './ChatSettingsSection';
import ClickUpSettingsSection from './ClickUpSettingsSection';
import AthenaSettingsSection from './AthenaSettingsSection';
import DatabaseConnectionsSection from './DatabaseConnectionsSection';
import AxiomExplorerPage from './AxiomExplorerPage';
import hudStyles from '../theme/hud.module.css';

export default function SettingsPage() {
  // Axiom Explorer is heavy (schema crawls across 46 DBs), so keep it
  // collapsed by default — expand on demand.
  const [axiomOpen, setAxiomOpen] = useState(false);

  return (
    <div className={hudStyles.page}>
      <h2 className={hudStyles.pageTitle}>Settings</h2>
      <DatabaseConnectionsSection />
      <ClickUpSettingsSection />
      <AthenaSettingsSection />
      <ChatSettingsSection />

      <div style={{ marginTop: 20 }}>
        <button
          type="button"
          onClick={() => setAxiomOpen((v) => !v)}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            width: '100%',
            padding: '10px 14px',
            background: 'rgba(0, 240, 255, 0.06)',
            color: 'var(--ink, #e6f6ff)',
            border: '1px solid rgba(0, 240, 255, 0.25)',
            borderRadius: 4,
            cursor: 'pointer',
            fontFamily: 'inherit',
            fontSize: 13,
            textAlign: 'left',
          }}
        >
          {axiomOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          <Database size={14} color="#00f0ff" />
          <span style={{ fontWeight: 600 }}>Axiom Explorer</span>
          <span style={{ fontSize: 11, opacity: 0.7 }}>
            · read-only schema discovery across rain's 46 BSS databases
          </span>
        </button>
        {axiomOpen && (
          <div style={{ marginTop: 8 }}>
            <AxiomExplorerPage />
          </div>
        )}
      </div>
    </div>
  );
}
