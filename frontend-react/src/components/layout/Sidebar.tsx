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
  DollarSign,
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
} from 'lucide-react';
import styles from './Sidebar.module.css';

interface NavItem {
  readonly path: string;
  readonly icon: React.ReactNode;
  readonly label: string;
}

const NAV_ITEMS: readonly NavItem[] = [
  { path: '/', icon: <LayoutDashboard size={18} />, label: 'Dashboard' },
  { path: '/chat', icon: <MessageSquare size={18} />, label: 'Chat' },
  { path: '/customer', icon: <UserSearch size={18} />, label: 'Customer 360' },
  { path: '/agents', icon: <Users size={18} />, label: 'Agent Fleet' },
  { path: '/tasks', icon: <KanbanSquare size={18} />, label: 'Task Board' },
  { path: '/feed', icon: <Activity size={18} />, label: 'Activity Feed' },
  { path: '/tools', icon: <Wrench size={18} />, label: 'Tools Hub' },
  { path: '/health', icon: <HeartPulse size={18} />, label: 'System Health' },
  { path: '/logs', icon: <Terminal size={18} />, label: 'Log Terminal' },
  { path: '/costs', icon: <DollarSign size={18} />, label: 'Cost Analytics' },
  { path: '/security', icon: <Shield size={18} />, label: 'Security' },
  { path: '/approvals', icon: <CheckSquare size={18} />, label: 'Approvals' },
  { path: '/projects', icon: <FolderKanban size={18} />, label: 'Projects' },
  { path: '/pipelines', icon: <GitBranch size={18} />, label: 'Pipelines' },
  { path: '/documents', icon: <FileText size={18} />, label: 'Documents' },
  { path: '/office', icon: <Building2 size={18} />, label: 'Agent Office' },
  { path: '/skills', icon: <Sparkles size={18} />, label: 'Skills + MCP' },
  { path: '/clickup', icon: <ListTodo size={18} />, label: 'ClickUp' },
] as const;

export default function Sidebar() {
  return (
    <aside className={styles.sidebar}>
      <div className={styles.brand}>
        <span className={styles.logo}>SOG</span>
        <span className={styles.brandText}>Command Centre</span>
      </div>

      <nav className={styles.nav}>
        {NAV_ITEMS.map((item) => (
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
