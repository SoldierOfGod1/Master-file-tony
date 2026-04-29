/* ============================================================
   SOLDIER OF GOD — Command Centre Global State Context
   Provides centralised state, WebSocket integration, and
   parallel data fetching for the entire dashboard.
   ============================================================ */

import {
  createContext,
  useContext,
  useEffect,
  useReducer,
  useCallback,
  useRef,
  type ReactNode,
  type Dispatch,
} from 'react';

import type {
  Agent,
  Task,
  KPIs,
  FeedEvent,
  Tool,
  HealthMetrics,
  LogEntry,
  CostData,
  SecurityState,
  Approval,
  Project,
  Pipeline,
  Document,
  OfficeData,
} from '../types/api';
import type { WebSocketStatus } from '../types/websocket';

import { listAgents } from '../api/agents';
import { listTasks } from '../api/tasks';
import { getKPIs } from '../api/kpis';
import { listFeed } from '../api/feed';
import { listTools } from '../api/tools';
import { getHealthMetrics } from '../api/health';
import { listLogs } from '../api/logs';
import { getCosts } from '../api/costs';
import { getSecurity } from '../api/security';
import { listApprovals } from '../api/approvals';
import { listProjects } from '../api/projects';
import { listPipelines } from '../api/pipelines';
import { listDocuments } from '../api/documents';
import { getOffice } from '../api/office';
import { useWebSocket } from '../hooks/useWebSocket';

// ---- State Shape ----

export interface CommandCentreState {
  agents: Agent[];
  tasks: Task[];
  kpis: KPIs | null;
  feed: FeedEvent[];
  tools: Tool[];
  health: HealthMetrics | null;
  logs: LogEntry[];
  costs: CostData | null;
  security: SecurityState | null;
  approvals: Approval[];
  projects: Project[];
  pipelines: Pipeline[];
  documents: Document[];
  office: OfficeData | null;
  gatewayStatus: WebSocketStatus;
}

const initialState: CommandCentreState = {
  agents: [],
  tasks: [],
  kpis: null,
  feed: [],
  tools: [],
  health: null,
  logs: [],
  costs: null,
  security: null,
  approvals: [],
  projects: [],
  pipelines: [],
  documents: [],
  office: null,
  gatewayStatus: 'disconnected',
};

// ---- Actions ----

type Action =
  | { type: 'SET_AGENTS'; payload: Agent[] }
  | { type: 'SET_TASKS'; payload: Task[] }
  | { type: 'SET_KPIS'; payload: KPIs | null }
  | { type: 'SET_FEED'; payload: FeedEvent[] }
  | { type: 'SET_TOOLS'; payload: Tool[] }
  | { type: 'SET_HEALTH'; payload: HealthMetrics | null }
  | { type: 'SET_LOGS'; payload: LogEntry[] }
  | { type: 'SET_COSTS'; payload: CostData | null }
  | { type: 'SET_SECURITY'; payload: SecurityState | null }
  | { type: 'SET_APPROVALS'; payload: Approval[] }
  | { type: 'SET_PROJECTS'; payload: Project[] }
  | { type: 'SET_PIPELINES'; payload: Pipeline[] }
  | { type: 'SET_DOCUMENTS'; payload: Document[] }
  | { type: 'SET_OFFICE'; payload: OfficeData | null }
  | { type: 'SET_GATEWAY_STATUS'; payload: WebSocketStatus };

function reducer(state: CommandCentreState, action: Action): CommandCentreState {
  switch (action.type) {
    case 'SET_AGENTS':
      return { ...state, agents: action.payload };
    case 'SET_TASKS':
      return { ...state, tasks: action.payload };
    case 'SET_KPIS':
      return { ...state, kpis: action.payload };
    case 'SET_FEED':
      return { ...state, feed: action.payload };
    case 'SET_TOOLS':
      return { ...state, tools: action.payload };
    case 'SET_HEALTH':
      return { ...state, health: action.payload };
    case 'SET_LOGS':
      return { ...state, logs: action.payload };
    case 'SET_COSTS':
      return { ...state, costs: action.payload };
    case 'SET_SECURITY':
      return { ...state, security: action.payload };
    case 'SET_APPROVALS':
      return { ...state, approvals: action.payload };
    case 'SET_PROJECTS':
      return { ...state, projects: action.payload };
    case 'SET_PIPELINES':
      return { ...state, pipelines: action.payload };
    case 'SET_DOCUMENTS':
      return { ...state, documents: action.payload };
    case 'SET_OFFICE':
      return { ...state, office: action.payload };
    case 'SET_GATEWAY_STATUS':
      return { ...state, gatewayStatus: action.payload };
    default:
      return state;
  }
}

// ---- Context ----

interface CommandCentreContextValue {
  state: CommandCentreState;
  dispatch: Dispatch<Action>;
  refreshAll: () => Promise<void>;
}

const CommandCentreContext = createContext<CommandCentreContextValue | null>(null);

// ---- WebSocket URL ----

function getWebSocketUrl(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}/ws`;
}

// ---- Provider ----

interface ProviderProps {
  children: ReactNode;
}

export function CommandCentreProvider({ children }: ProviderProps) {
  const [state, dispatch] = useReducer(reducer, initialState);
  const dispatchRef = useRef(dispatch);
  dispatchRef.current = dispatch;

  // Parallel fetch of all endpoints
  const refreshAll = useCallback(async () => {
    const [
      agents,
      tasks,
      kpis,
      feed,
      tools,
      health,
      logs,
      costs,
      security,
      approvals,
      projects,
      pipelines,
      documents,
      office,
    ] = await Promise.all([
      listAgents(),
      listTasks(),
      getKPIs(),
      listFeed(),
      listTools(),
      getHealthMetrics(),
      listLogs(),
      getCosts(),
      getSecurity(),
      listApprovals(),
      listProjects(),
      listPipelines(),
      listDocuments(),
      getOffice(),
    ]);

    const d = dispatchRef.current;
    d({ type: 'SET_AGENTS', payload: agents });
    d({ type: 'SET_TASKS', payload: tasks });
    d({ type: 'SET_KPIS', payload: kpis });
    d({ type: 'SET_FEED', payload: feed });
    d({ type: 'SET_TOOLS', payload: tools });
    d({ type: 'SET_HEALTH', payload: health });
    d({ type: 'SET_LOGS', payload: logs });
    d({ type: 'SET_COSTS', payload: costs });
    d({ type: 'SET_SECURITY', payload: security });
    d({ type: 'SET_APPROVALS', payload: approvals });
    d({ type: 'SET_PROJECTS', payload: projects });
    d({ type: 'SET_PIPELINES', payload: pipelines });
    d({ type: 'SET_DOCUMENTS', payload: documents });
    d({ type: 'SET_OFFICE', payload: office });
  }, []);

  // Initial data load
  useEffect(() => {
    void refreshAll();
  }, [refreshAll]);

  // WebSocket connection
  const wsUrl = getWebSocketUrl();
  const { status: wsStatus, lastMessage } = useWebSocket(wsUrl);

  // Sync gateway status
  useEffect(() => {
    dispatch({ type: 'SET_GATEWAY_STATUS', payload: wsStatus });
  }, [wsStatus]);

  // Dispatch incoming WebSocket messages to the correct slice
  useEffect(() => {
    if (!lastMessage) return;

    const { type, payload } = lastMessage;

    switch (type) {
      case 'agents':
        dispatch({ type: 'SET_AGENTS', payload: payload as Agent[] });
        break;
      case 'tasks':
        dispatch({ type: 'SET_TASKS', payload: payload as Task[] });
        break;
      case 'kpis':
        dispatch({ type: 'SET_KPIS', payload: payload as KPIs });
        break;
      case 'feed':
        dispatch({ type: 'SET_FEED', payload: payload as FeedEvent[] });
        break;
      case 'tools':
        dispatch({ type: 'SET_TOOLS', payload: payload as Tool[] });
        break;
      case 'health':
        dispatch({ type: 'SET_HEALTH', payload: payload as HealthMetrics });
        break;
      case 'logs':
        dispatch({ type: 'SET_LOGS', payload: payload as LogEntry[] });
        break;
      case 'costs':
        dispatch({ type: 'SET_COSTS', payload: payload as CostData });
        break;
      case 'security':
        dispatch({ type: 'SET_SECURITY', payload: payload as SecurityState });
        break;
      case 'approvals':
        dispatch({ type: 'SET_APPROVALS', payload: payload as Approval[] });
        break;
      case 'projects':
        dispatch({ type: 'SET_PROJECTS', payload: payload as Project[] });
        break;
      case 'pipelines':
        dispatch({ type: 'SET_PIPELINES', payload: payload as Pipeline[] });
        break;
      case 'documents':
        dispatch({ type: 'SET_DOCUMENTS', payload: payload as Document[] });
        break;
      case 'office':
        dispatch({ type: 'SET_OFFICE', payload: payload as OfficeData });
        break;
      default:
        // Unrecognised message types are NO-OPs.
        //
        // The backend bus emits a long tail of typed events the
        // frontend doesn't currently listen for: project.update,
        // project.delete, agent.status, customer (feed.Publish),
        // chat.stream/complete/error, alert.resolve, etc. Falling
        // through to refreshAll() here turned every one of those into
        // a 15-endpoint parallel re-fetch — including slow Axiom
        // queries — which froze the UI on every tab switch and on
        // every backend-side mutation. Add explicit cases above when
        // a payload is genuinely needed; otherwise the page-level
        // useAutoRefresh hooks already pick up the new state.
        break;
    }
  }, [lastMessage]);

  const value: CommandCentreContextValue = {
    state,
    dispatch,
    refreshAll,
  };

  return (
    <CommandCentreContext.Provider value={value}>
      {children}
    </CommandCentreContext.Provider>
  );
}

// ---- Consumer Hook ----

export function useCommandCentre(): CommandCentreContextValue {
  const ctx = useContext(CommandCentreContext);
  if (!ctx) {
    throw new Error(
      'useCommandCentre must be used within a <CommandCentreProvider>',
    );
  }
  return ctx;
}
