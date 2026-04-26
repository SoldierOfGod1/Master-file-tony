import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  MessageSquare,
  Users,
  KanbanSquare,
  Activity,
  Settings,
  HeartPulse,
  Terminal,
  Banknote,
  Shield,
  CheckSquare,
  FolderKanban,
  GitBranch,
  FileText,
  Building2,
  Wrench,
  Sparkles,
  ListTodo,
  UserSearch,
  TrendingUp,
} from 'lucide-react';
import styles from './Sidebar.module.css';

interface NavItem {
  readonly path: string;
  readonly icon: React.ReactNode;
  readonly label: string;
}

interface NavGroup {
  readonly label: string;       // section header; omitted for the Overview group
  readonly items: readonly NavItem[];
}

// Hand-curated operational layout. A-Z sort was scattering related
// tabs (ClickUp between Chat and Cost; Security next to rain Sales).
// Ordered by how often an operator reaches for each section during
// a shift: Overview → Customer ops → Build/deploy → Fleet →
// Observability. Each group has a thin header so the structure is
// scannable at a glance.
const NAV_GROUPS: readonly NavGroup[] = [
  {
    label: '',
    items: [
      { path: '/',       icon: <LayoutDashboard size={18} />, label: 'Dashboard' },
      { path: '/chat',   icon: <MessageSquare size={18} />,  label: 'Chat' },
    ],
  },
  {
    label: 'Customer Ops',
    items: [
      { path: '/customer',  icon: <UserSearch size={18} />,    label: 'Customer 360' },
      { path: '/sales',     icon: <TrendingUp size={18} />,    label: 'rain Sales' },
      { path: '/service',   icon: <Activity size={18} />,      label: 'rain Service' },
      { path: '/clickup',   icon: <ListTodo size={18} />,      label: 'ClickUp' },
      { path: '/tasks',     icon: <KanbanSquare size={18} />,  label: 'Task Board' },
      { path: '/approvals', icon: <CheckSquare size={18} />,   label: 'Approvals' },
      { path: '/feed',      icon: <Activity size={18} />,      label: 'Activity Feed' },
    ],
  },
  {
    label: 'Build & Deploy',
    items: [
      { path: '/projects',  icon: <FolderKanban size={18} />, label: 'Projects' },
      { path: '/pipelines', icon: <GitBranch size={18} />,    label: 'Pipelines' },
      { path: '/documents', icon: <FileText size={18} />,     label: 'Documents' },
    ],
  },
  {
    label: 'Agent Fleet',
    items: [
      { path: '/agents', icon: <Users size={18} />,      label: 'Agent Fleet' },
      { path: '/office', icon: <Building2 size={18} />,  label: 'Agent Office' },
      { path: '/tools',  icon: <Wrench size={18} />,     label: 'Tools Hub' },
      { path: '/skills', icon: <Sparkles size={18} />,   label: 'Skills + MCP' },
    ],
  },
  {
    label: 'Observability',
    items: [
      { path: '/health',   icon: <HeartPulse size={18} />, label: 'System Health' },
      { path: '/logs',     icon: <Terminal size={18} />,   label: 'Log Terminal' },
      { path: '/costs',    icon: <Banknote size={18} />,   label: 'Cost Analytics' },
      { path: '/security', icon: <Shield size={18} />,     label: 'Security' },
    ],
  },
];

export default function Sidebar() {
  return (
    <aside className={styles.sidebar}>
      <div className={styles.brand}>
        <img src="/rain-logo.svg" alt="rain" className={styles.logo} />
        <span className={styles.brandText}>Command Centre</span>
      </div>

      <nav className={styles.nav}>
        {NAV_GROUPS.map((group, gi) => (
          <div key={gi} style={{ marginBottom: 10 }}>
            {group.label && (
              <div
                style={{
                  fontSize: 9,
                  textTransform: 'uppercase',
                  letterSpacing: '0.14em',
                  opacity: 0.5,
                  padding: '10px 14px 4px',
                }}
              >
                {group.label}
              </div>
            )}
            {group.items.map((item) => (
              <NavLink
                key={item.path}
                to={item.path}
                end={item.path === '/'}
                className={({ isActive }) =>
                  `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
                }
              >
                <span className={styles.navIcon}>{item.icon}</span>
                <span className={styles.navLabel}>{item.label}</span>
              </NavLink>
            ))}
          </div>
        ))}
      </nav>

      <div className={styles.footer}>
        <NavLink
          to="/settings"
          className={({ isActive }) =>
            `${styles.settingsBtn} ${isActive ? styles.settingsBtnActive : ''}`
          }
        >
          <Settings size={16} />
          <span>Settings</span>
        </NavLink>
      </div>
    </aside>
  );
}
