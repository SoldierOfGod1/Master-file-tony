/* ============================================================
   SOLDIER OF GOD — API Type Definitions
   Derived from Go backend handler response shapes
   ============================================================ */

// ----- Agents -----

export interface Agent {
  id: string;
  name: string;
  model: string;
  maxInstances: number | null;
  status: string;
  task: string;
  role: string;
}

// ----- Tasks -----

export interface Task {
  id: string;
  title: string;
  agent: string;
  priority: string;
  column: string;
  time: string;
}

// ----- KPIs -----

export interface KpiEntry {
  value: number;
  max?: number;
  trend: 'up' | 'down' | 'flat';
}

export interface KPIs {
  activeAgents: KpiEntry;
  tasksInFlight: KpiEntry;
  tokensToday: KpiEntry;
  costToday: KpiEntry;
  uptime: KpiEntry;
  errorRate: KpiEntry;
}

// ----- Live Feed -----

export interface FeedEvent {
  time: string;
  type: string;
  agent: string;
  message: string;
}

// ----- Tools -----

export interface Tool {
  id: string;
  name: string;
  icon: string;
  desc: string;
  detail: string;
  agents: string[];
  systems: string[];
  status: string;
}

// ----- Health -----

export interface HealthMetrics {
  cpu: number;
  memory: number;
  network: number;
}

// ----- Logs -----

export interface LogEntry {
  ts: string;
  level: string;
  agent: string;
  msg: string;
}

// ----- Costs -----

export interface CostModel {
  name: string;
  value: number;
  color: string;
}

export interface CostData {
  models: CostModel[];
  daily: number[];
  total: number;
}

// ----- Security -----

export interface SecurityState {
  trustScore: number;
  critical: number;
  warning: number;
  info: number;
  rulesActive: number;
  lastScan: string;
}

// ----- Approvals -----

export interface Approval {
  id: string;
  type: string;
  title: string;
  description: string;
  requester: string;
  status: string;
  priority: string;
  reviewer?: string;
  reviewComment?: string;
  createdAt: string;
}

// ----- Projects -----

export interface ProjectComponent {
  role: string; // "core" | "frontend" | "backend" | "infra" | etc.
  path: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  status: string;
  priority: string;
  owner: string;
  progress: number;
  createdAt: string;
  // Project → ClickUp sync fields (added by the two-way sync work)
  localPath?: string;
  components?: ProjectComponent[];
  hasFrontend?: boolean;
  hasBackend?: boolean;
  clickupTaskId?: string;
  clickupUrl?: string;
  clickupLastSync?: string;
}

// ----- Customer 360 -----

export interface CustomerIdentity {
  id: string;
  full_name: string;
  given_name: string;
  family_name: string;
  email: string;
  status: string;
  created_at: string;
}

export interface CustomerContact {
  email: string;
  phone: string;
  street_number: string;
  street_name: string;
  suburb: string;
  city: string;
  province: string;
  postal_code: string;
  preferred: boolean;
  updated_at: string;
}

export interface CustomerPayment {
  id: string;
  amount: number;
  channel: string;
  status: string;
  payment_date: string;
}

export interface CustomerSubscription {
  id: string;
  name: string;
  status: string;
  started_at: string;
  price: number;
}

export interface CustomerTicket {
  id: string;
  subject: string;
  status: string;
  created_at: string;
}

export interface CustomerChargeback {
  id: string;
  amount: number;
  reason: string;
  status: string;
  created_at: string;
}

export interface CustomerRiskScore {
  value: number;
  band: 'low' | 'medium' | 'high';
  reason: string;
}

export interface CustomerAccountAge {
  days: number;
  human_friendly: string;
  since: string;
}

export interface CustomerTimelineEvent {
  at: string;
  type: string;
  label: string;
  detail?: string;
}

export interface CustomerNeighbour {
  id: string;
  full_name: string;
}

export interface CustomerDeepLinks {
  station: string;
  athena: string;
  raingo: string;
}

export interface Customer360 {
  identity: CustomerIdentity;
  contacts: CustomerContact[];
  payments: CustomerPayment[];
  subscriptions: CustomerSubscription[];
  tickets: CustomerTicket[];
  chargebacks: CustomerChargeback[];
  risk_score: CustomerRiskScore;
  lifetime_value: number;
  account_age: CustomerAccountAge;
  days_since_last_payment: number;
  timeline: CustomerTimelineEvent[];
  payment_heatmap: number[];
  neighbours: CustomerNeighbour[];
  deep_links: CustomerDeepLinks;
  looked_up_by: string;
  looked_up_at: string;
  churn_risk: string;
}

export interface CustomerConfig {
  configured: boolean;
  host: string;
  user: string;
  database: string;
}

/** Canonical status order across Projects + ClickUp. Keep in sync with
 *  backend/internal/clickup/client.go → ProjectStatuses. */
export const PROJECT_STATUSES = [
  'To Do',
  'In Progress',
  'SIT',
  'QA',
  'PPD',
  'QA Fail',
  'Blocker',
  'SIT Pass',
  'PPD Pass',
  'Completed',
] as const;
export type ProjectStatus = (typeof PROJECT_STATUSES)[number];

// ----- Pipelines -----

export interface PipelineStage {
  name: string;
  status: string;
}

export interface Pipeline {
  id: string;
  projectId: string;
  name: string;
  type: string;
  status: string;
  trigger: string;
  branch: string;
  stages: PipelineStage[];
  durationMs: number;
  createdAt: string;
}

// ----- Documents -----

export interface Document {
  id: string;
  projectId: string;
  title: string;
  type: string;
  content?: string;
  version: number;
  author: string;
  createdAt: string;
}

// ----- Agent Office -----

export interface AgentOfficeState {
  id: string;
  name?: string;
  x: number;
  y: number;
  zone: string;
  activity: string;
  mood: string;
  currentFile: string;
  lastAction: string;
}

export interface OfficeZone {
  id: string;
  name: string;
  x: number;
  y: number;
  w: number;
  h: number;
  color: string;
}

export interface OfficeData {
  agents: AgentOfficeState[];
  zones: OfficeZone[];
}

// ----- Chat / Conversations -----

export interface Conversation {
  id: string;
  title: string;
  projectDir: string;
  source: string;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface Message {
  id: number;
  conversationId: string;
  role: string;
  content: string;
  source: string;
  metadata: Record<string, unknown>;
  createdAt: string;
}

export interface ChatConfig {
  discordToken: string;
  discordUserId: string;
  pinConfigured: boolean;
  defaultProjectDir: string;
  pinTimeoutMinutes: number;
}
