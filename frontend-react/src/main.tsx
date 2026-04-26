import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import './theme/variables.css'
import './theme/global.css'
import './theme/glass.css'
import App from './App'

/* Route components are code-split. Each lazy() call becomes its
   own chunk so the initial bundle drops from ~620 kB to roughly
   the size of the entry + the first route. Pre-existing eager
   imports caused vite to pack all 22 pages into one chunk. */
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const AgentFleetPage = lazy(() => import('./pages/AgentFleetPage'))
const TaskBoardPage = lazy(() => import('./pages/TaskBoardPage'))
const ActivityFeedPage = lazy(() => import('./pages/ActivityFeedPage'))
const ToolsHubPage = lazy(() => import('./pages/ToolsHubPage'))
const SystemHealthPage = lazy(() => import('./pages/SystemHealthPage'))
const LogTerminalPage = lazy(() => import('./pages/LogTerminalPage'))
const CostAnalyticsPage = lazy(() => import('./pages/CostAnalyticsPage'))
const SecurityPage = lazy(() => import('./pages/SecurityPage'))
const ApprovalsPage = lazy(() => import('./pages/ApprovalsPage'))
const ProjectsPage = lazy(() => import('./pages/ProjectsPage'))
const PipelinesPage = lazy(() => import('./pages/PipelinesPage'))
const DocumentsPage = lazy(() => import('./pages/DocumentsPage'))
const OfficePage = lazy(() => import('./pages/OfficePage'))
const ChatPage = lazy(() => import('./pages/ChatPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const SkillsPage = lazy(() => import('./pages/SkillsPage'))
const ClickUpPage = lazy(() => import('./pages/ClickUpPage'))
const Customer360Page = lazy(() => import('./pages/Customer360Page'))
const AxiomExplorerPage = lazy(() => import('./pages/AxiomExplorerPage'))
const SalesPage = lazy(() => import('./pages/SalesPage'))
const ServicePage = lazy(() => import('./pages/ServicePage'))

/* Tiny inline fallback while a route chunk is loading. Stays
   inside the main layout via the Outlet so the chrome doesn't
   blink when navigating. */
function RouteFallback() {
  return (
    <div
      style={{
        padding: 24,
        opacity: 0.6,
        fontFamily: 'var(--font-mono, monospace)',
        fontSize: 12,
      }}
    >
      loading…
    </div>
  )
}

function withSuspense(node: React.ReactNode) {
  return <Suspense fallback={<RouteFallback />}>{node}</Suspense>
}

const router = createBrowserRouter([
  {
    path: '/',
    element: <App />,
    children: [
      { index: true, element: withSuspense(<DashboardPage />) },
      { path: 'chat', element: withSuspense(<ChatPage />) },
      { path: 'agents', element: withSuspense(<AgentFleetPage />) },
      { path: 'tasks', element: withSuspense(<TaskBoardPage />) },
      { path: 'feed', element: withSuspense(<ActivityFeedPage />) },
      { path: 'tools', element: withSuspense(<ToolsHubPage />) },
      { path: 'health', element: withSuspense(<SystemHealthPage />) },
      { path: 'logs', element: withSuspense(<LogTerminalPage />) },
      { path: 'costs', element: withSuspense(<CostAnalyticsPage />) },
      { path: 'security', element: withSuspense(<SecurityPage />) },
      { path: 'approvals', element: withSuspense(<ApprovalsPage />) },
      { path: 'projects', element: withSuspense(<ProjectsPage />) },
      { path: 'pipelines', element: withSuspense(<PipelinesPage />) },
      { path: 'documents', element: withSuspense(<DocumentsPage />) },
      { path: 'office', element: withSuspense(<OfficePage />) },
      { path: 'skills', element: withSuspense(<SkillsPage />) },
      { path: 'clickup', element: withSuspense(<ClickUpPage />) },
      { path: 'customer', element: withSuspense(<Customer360Page />) },
      { path: 'axiom', element: withSuspense(<AxiomExplorerPage />) },
      { path: 'sales', element: withSuspense(<SalesPage />) },
      { path: 'service', element: withSuspense(<ServicePage />) },
      { path: 'settings', element: withSuspense(<SettingsPage />) },
    ],
  },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
