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
  // Environment URLs — populated either by the default seed (5 known
  // SIT URLs) or by the user via the edit form. Used by the Projects
  // tab's Current / SIT / Production view toggle.
  sitUrl?: string;
  prodUrl?: string;
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
}

export interface CustomerBillingAccount {
  id: string;
  name: string;
  state: string;
  account_type: string;
  payment_status: string;
  credit_limit: number;
  payment_day: number;
  financial_account_id?: string;
  updated_at: string;
}

export interface CustomerBalance {
  balance_type: string;
  amount: number;
  last_invoice_amount?: number;
  valid_from?: string;
  valid_to?: string;
}

export interface CustomerInvoice {
  invoice_number: string;
  invoice_date: string;
  due_date: string;
  amount: number;
  balance: number;
  status: string;
  source: string;
}

export interface CustomerPromise {
  id: string;
  status: string;
  total_amount: number;
  total_allocated: number;
  balance: number;
  number_of_payments: number;
  installment_amount: number;
  payment_frequency: string;
  valid_from?: string;
  valid_to?: string;
}

export interface CustomerNotification {
  channel: string;
  msisdn?: string;
  status?: string;
  message?: string;
  inserted_at: string;
}

export interface CustomerDataSource {
  name: string;
  database: string;
  state: 'ok' | 'empty' | 'error' | 'skipped';
  rows: number;
  error?: string;
  latency_ms?: number;
}

export interface CustomerProduct {
  id: string;
  family: 'mobile' | 'loop' | '101' | 'other' | string;
  product_line?: string;
  image_url?: string;
  name: string;
  category?: string;
  service_type?: string;
  state?: string;
  start_date?: string;
  end_date?: string;
  has_started?: boolean;
  is_bundle?: boolean;
  parent_id?: string;
  colour_variant?: string;
  msisdn?: string;
  imei?: string;
  iccid?: string;
  imsi?: string;
  master_policy?: string;
  account_number?: string;
}

export interface CustomerUsageSnapshot {
  msisdn: string;
  imsi?: string;
  imei?: string;
  policy_name: string;
  quota: string;
  load: string;
  quota_status?: string;
  service_name?: string;
  ip_address?: string;
  state?: string;
  updated_at?: string;
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
  billing_accounts?: CustomerBillingAccount[];
  balances?: CustomerBalance[];
  invoices?: CustomerInvoice[];
  promises?: CustomerPromise[];
  recent_notifications?: CustomerNotification[];
  products?: CustomerProduct[];
  usage?: CustomerUsageSnapshot[];
  cdr_usage?: CustomerCDRUsage[];
  data_sources?: CustomerDataSource[];
  candidates?: IdentityCandidate[];

  // v2 decisioning layer
  predictions?: CustomerPredictions;
  journey_stage?: CustomerJourneyStage;
  recommendations?: CustomerRecommendation[];

  // Phase 2 of docs/axiom/sim-diagnostics-plan.md.
  // One IMSISource per IMSI returned by resolveIMSIs, tagged with
  // which cascade phase produced it. Read by the SIM Diagnostics
  // panel to render its phase tag-chip row.
  sim_diagnostics?: IMSISource[];
}

// IMSISource is one row of the SIM Diagnostics panel feed. The
// `source` string is one of the cascade-phase identifiers that
// the backend's resolveIMSIs returns (override / product_path /
// view_account / view_msisdn / view_subscriber). Stable contract —
// the panel maps these to chip labels. ResolvedAt is ISO 8601 UTC.
export interface IMSISource {
  imsi: number;
  source: string;
  resolved_at: string;
}

export interface CustomerPredictions {
  churn_30d: number;
  churn_60d: number;
  churn_90d: number;
  payment_default_30d: number;
  ltv_12m_expected: number;
  upsell_propensity: number;
  confidence: number;
  reason_codes: string[];
  model_version: string;
  computed_at: string;
}

export interface CustomerJourneyStage {
  stage: string; // Onboarding | Activation | Growth | Friction | Retention | Recovery | Loyalty
  entered_at?: string;
  triggering_events?: string[];
}

export interface CustomerRecommendation {
  id: string;
  customer_id: string;
  type: string; // retention_offer | collections_action | upsell | service_action
  title: string;
  description?: string;
  channel: string;
  priority_rank: number;
  expected_value: number;
  cost_estimate: number;
  reason_codes: string[];
  status: string; // presented | accepted | dismissed | snoozed
  created_at: string;
}

export interface CustomerCDRUsage {
  date: string;
  account_code: string;
  billing_account: string;
  imei: string;
  imsi: string;
  msisdn: string;
  usage_gb: number;
}

export interface IdentityCandidate {
  id: string;
  full_name: string;
  given_name: string;
  family_name: string;
  email: string;
  created_at: string;
  account_number?: string;
  msisdn?: string;
  source?: string;
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
