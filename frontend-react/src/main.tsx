import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import './theme/variables.css'
import './theme/global.css'
import './theme/glass.css'
import App from './App'
import DashboardPage from './pages/DashboardPage'
import AgentFleetPage from './pages/AgentFleetPage'
import TaskBoardPage from './pages/TaskBoardPage'
import ActivityFeedPage from './pages/ActivityFeedPage'
import ToolsHubPage from './pages/ToolsHubPage'
import SystemHealthPage from './pages/SystemHealthPage'
import LogTerminalPage from './pages/LogTerminalPage'
import CostAnalyticsPage from './pages/CostAnalyticsPage'
import SecurityPage from './pages/SecurityPage'
import ApprovalsPage from './pages/ApprovalsPage'
import ProjectsPage from './pages/ProjectsPage'
import PipelinesPage from './pages/PipelinesPage'
import DocumentsPage from './pages/DocumentsPage'
import OfficePage from './pages/OfficePage'
import ChatPage from './pages/ChatPage'
import SettingsPage from './pages/SettingsPage'
import SkillsPage from './pages/SkillsPage'
import ClickUpPage from './pages/ClickUpPage'
import Customer360Page from './pages/Customer360Page'
import AxiomExplorerPage from './pages/AxiomExplorerPage'
import SalesPage from './pages/SalesPage'
import ServicePage from './pages/ServicePage'

const router = createBrowserRouter([
  {
    path: '/',
    element: <App />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'chat', element: <ChatPage /> },
      { path: 'agents', element: <AgentFleetPage /> },
      { path: 'tasks', element: <TaskBoardPage /> },
      { path: 'feed', element: <ActivityFeedPage /> },
      { path: 'tools', element: <ToolsHubPage /> },
      { path: 'health', element: <SystemHealthPage /> },
      { path: 'logs', element: <LogTerminalPage /> },
      { path: 'costs', element: <CostAnalyticsPage /> },
      { path: 'security', element: <SecurityPage /> },
      { path: 'approvals', element: <ApprovalsPage /> },
      { path: 'projects', element: <ProjectsPage /> },
      { path: 'pipelines', element: <PipelinesPage /> },
      { path: 'documents', element: <DocumentsPage /> },
      { path: 'office', element: <OfficePage /> },
      { path: 'skills', element: <SkillsPage /> },
      { path: 'clickup', element: <ClickUpPage /> },
      { path: 'customer', element: <Customer360Page /> },
      { path: 'axiom', element: <AxiomExplorerPage /> },
      { path: 'sales', element: <SalesPage /> },
      { path: 'service', element: <ServicePage /> },
      { path: 'settings', element: <SettingsPage /> },
    ],
  },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
