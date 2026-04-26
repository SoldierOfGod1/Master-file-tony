/* ============================================================
   SOLDIER OF GOD — Budgets API (Phase B3 + D-series follow-up)
   ============================================================ */

import { apiGet, apiPut } from './client';

/* Mirror of chat.BudgetState from backend/internal/chat/budget.go.
   Verdict drives the colour band in the UI:
     ok      under 80% — green
     warn    80-99%   — amber
     blocked >=100%   — red */
export interface BudgetState {
  readonly user_id: string;
  readonly week_start: string;
  readonly spent_zar: number;
  readonly cap_zar: number;
  readonly pct_spent: number;
  readonly verdict: 'ok' | 'warn' | 'blocked';
}

export async function listBudgets(): Promise<BudgetState[]> {
  const data = await apiGet<BudgetState[]>('/budgets');
  return data ?? [];
}

export async function getBudget(userID: string): Promise<BudgetState | null> {
  return apiGet<BudgetState>(`/budgets/${encodeURIComponent(userID)}`);
}

export async function setBudgetCap(
  userID: string,
  weeklyZARCap: number,
): Promise<{ user_id: string; weekly_zar_cap: number } | null> {
  return apiPut<{ user_id: string; weekly_zar_cap: number }>(
    `/budgets/${encodeURIComponent(userID)}`,
    { weekly_zar_cap: weeklyZARCap },
  );
}
