import ChatSettingsSection from './ChatSettingsSection';
import ClickUpSettingsSection from './ClickUpSettingsSection';
import DatabaseConnectionsSection from './DatabaseConnectionsSection';
import hudStyles from '../theme/hud.module.css';

export default function SettingsPage() {
  return (
    <div className={hudStyles.page}>
      <h2 className={hudStyles.pageTitle}>Settings</h2>
      <DatabaseConnectionsSection />
      <ClickUpSettingsSection />
      <ChatSettingsSection />
    </div>
  );
}
