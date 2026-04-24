# Plan — SIM Diagnostics panel (Customer360)

**Status:** Draft v2 — amended after eng-review findings. Pending design review + final eng re-run.
**Supersedes:** `docs/axiom/imsi-lookup-plan.md` (runbook demoted to internal reference)
**Author:** Baptista
**Owners:** Automation team · rain ops (consumer)
**Date:** 2026-04-24 · amended 2026-04-24
**Mode:** SCOPE_EXPANSION (accepted from /plan-ceo-review)

## Amendments (v2) — applied from /plan-eng-review findings

- **Phase 0 SHIPPED** at commit `561befe` — IMSI/MSISDN/ICCID/IMEI now redacted in `/api/v1/axiom/peek` with regression test (finding 3A).
- **Fail-closed audit** — audit write is now a blocking pre-condition for every lookup response; if the write fails, the lookup returns 500. POPIA audit must never be best-effort (finding 2A).
- **Return-shape change** — `resolveIMSIs(...) []int64` becomes `resolveIMSIs(...) []IMSISource` where `IMSISource = { IMSI int64, Source string, ResolvedAt time.Time, BillingAccountID string, MSISDN string, ICCID string }`. Callers updated; internal API only. Required for panel's `source` column (finding 1B).
- **Role gate is server-side session-claim**, not `X-Rain-Role` header. If the auth-middleware for role evaluation doesn't exist yet in the Go server, that's a blocking pre-req PR before scope item #5 (override UI) ships. Flagged as Known Unknown #4 (finding 3B).
- **`msisdn_hash` replaced** with `individual_id` on the audit row — `individual_id` is already the customer FK, so hashing MSISDN adds attack surface (rainbow tables) without adding information (finding 3C).
- **Cascade coverage probe** — before writing `fetchIMSIsSvc`, run 3 SIT probes against known service-domain customers (recent SIM swap, number port, SIM move) and commit findings to `docs/axiom/sim-cascade-coverage.md`. If phases 2-3 already cover the gap, `fetchIMSIsSvc` is dead code and item #2 collapses (finding 1A).
- **Dedupe key** — changed from `(billing_account_id, msisdn)` to `imsi` only, because `vw_service_account_state_latest` may not return msisdn/iccid alongside imsi (finding 4A). Panel still shows msisdn/iccid when phase-1 returned them; empty otherwise.
- **New metric** — `rain_sim_diagnostics_cascade_depth{winning_phase}` counter tracks which phase resolved each lookup (finding 8A).
- **Empty-state UI copy** — leads with a 3-word tag (`NOT PROVISIONED` / `SWAP IN FLIGHT` / `BSS GAP — OVERRIDE`), explanation secondary (finding 11A).
- **Explorer UI chip** — when `/axiom/peek` returns `«redacted»`, Axiom Explorer shows a "Redacted — PII" chip + tooltip pointing at the (future) unredact flow. Ships with Phase 0 frontend follow-up (finding 9A).
- **Stale-override detection** — when `IMSIOverride` is present, phases 1-3 still run async and log disagreement; panel chip "override ≠ live" surfaces drift. Low-priority, Phase 2 (finding 1C).

---

## Problem (reframed after CEO review)

The original plan proposed a Markdown runbook with 4 hand-rolled SQL queries to look up a customer's IMSI from an email. The system audit found that **this is already implemented** in production (`backend/internal/customer/lookup_prod.go` + `frontend-react/src/pages/Customer360Page.tsx` — Products panel renders `msisdn/iccid/imei/imsi` per SIM card).

The actual problem is **silent failures** in that existing lookup:

1. **Service-domain products** (SIM swaps, number ports, moves) route through `service.service.resource_ref` which has a different schema than `product.product.resource_ref`. `lookup_prod.go` walks the product side only, so these customers appear to have no SIM.
2. **Post-swap stale IMSI**: `resource_ref.value` persists the old ICCID after a SIM swap; the current IMSI lives on a new `resource.resource.sim` row. Query returns the old IMSI.
3. **Ops today work around #1 and #2 manually** by writing overrides into `IMSIOverride` (`lookup_prod.go:152–156`). The override count is the current canary for lookup-gap volume.
4. **IMSI/MSISDN/CMI-IMSI are exposed in raw form** on `/api/v1/axiom/peek` — not in the PII redaction set. POPIA / ICASA breach risk.

## Outcome

A rain ops user opens Customer360 for `baptista.manuel@rain.co.za`, sees the SIM Diagnostics panel with every IMSI for every product (including service-domain), and when one is missing sees *why* — with a one-click audited action to fill it in.

---

## In scope (locked after CEO review)

### 1. SIM Diagnostics panel (Customer360)
- New panel: **"SIM Diagnostics"**, accent **`#b980ff` violet** (distinct from existing `#00f0ff` cyan / `#6ff2a0` green / `#ffaa00` amber; matches the `«redacted»` chip so violet = PII/diagnostic realm).
- One row per IMSI on the account, leading with IMSI (the answer), demoting MSISDN/ICCID/CMI-IMSI to the subtitle line. Each row shows: `status` chip, `last_swap_at` pill, **cascade-source tag chips** (`override / product / view-account / view-user / service / recon` — filled when contributed, dim outline when not).
- Empty-state: when the account has billing accounts but no resolvable SIMs, show a 3-word tag (`NOT PROVISIONED` / `SWAP IN FLIGHT` / `BSS GAP — OVERRIDE`) plus a 2-line diagnostic and a `[set override]` affordance (see scope #4 + design D3).
- See UI panel sketch v2 further in this doc for the exact layout.

### 2. Service-domain fallback in `lookup_prod.go` (conditional on SIT probe)
**Prerequisite:** run 3 SIT probes against service-domain customers first and commit `docs/axiom/sim-cascade-coverage.md`. If phases 1-3 of the existing cascade (`fetchIMSIsViaProductPath` → `fetchIMSIsByAccount` via `vw_service_account_state_latest` → `fetchIMSIsByUserID`) already cover the service-domain case, this item collapses to "add `source` tagging only" and skips the new query.
- If probe shows a gap: extend with a parallel query against `service.service.service_order_item → service.service.resource_ref → service.service.service_ref_or_value`, insert as phase 1.5 in the cascade.
- Union via dedupe on `imsi` (key changed from `(billing_account_id, msisdn)` — view doesn't carry those reliably).
- Tag every result with `source ∈ {override, product, view-account, view-user, service, recon}` for the panel.

### 3. Post-swap IMSI reconciliation
- When a row's `iccid` returns multiple SIM rows in `resource.resource.sim`, prefer the row with the most recent `activated_at`.
- Cross-check against `resource.recon.msisdn_match(udm_imsi, ib_imsi)` — if they disagree, surface BOTH in the panel with a "swap detected" chip and a `last_swap_at` timestamp.

### 4. Audit log + POPIA redaction
- New table `imsi_lookup_audit(id, at, user_id, individual_id, billing_account_id, reason, response_code, imsi_count, winning_phase)` in the app SQLite. No `msisdn_hash` — `individual_id` is the FK and hashing MSISDN adds attack surface without information value.
- Every call to `resolveIMSIs` emits an audit row. Retention: 18 months. **Write is fail-closed** — if the audit row can't be persisted, the lookup returns 500 and logs `audit_write_failed`. POPIA audit is never best-effort.
- `/api/v1/axiom/peek` PII set gains `imsi`, `msisdn`, `iccid`, `imei` (covers `cmi_imsi`, `udm_imsi`, `ib_imsi`, `current_imsi`, `first_imsi` via substring match). **Shipped in Phase 0 at commit `561befe`.**
- Unredact for `support-l2` role ships with scope item #5. Gate is evaluated server-side from session claims, not a client header.

### 5. IMSI override self-service
- The existing `IMSIOverride` table stays but moves behind a role-gated UI button inside the SIM Diagnostics panel (replacing the current direct-DB workflow).
- Every override writes both an `imsi_override` row AND an `imsi_lookup_audit` row with `reason='manual_override'` and `winning_phase='override'`.
- **Role gate** — server-side session claim check for `support-l2`. Blocks on the auth-middleware pre-req if it doesn't exist (see Known Unknowns #4).
- **Stale-override detection** — every time an override is used, phases 1-3 of the cascade still run async. If any returns a different IMSI, emit `override_drift_detected` log + surface "override ≠ live" chip on the next panel load. Low-priority, ship in Phase 2.

---

## NOT in scope (explicitly deferred)

| Item | Why deferred | Where it goes |
|---|---|---|
| Runbook publication | Superseded by Approach B. Kept internally for L3 escalations only. | `docs/axiom/imsi-lookup-plan.md` stays in repo, marked SUPERSEDED |
| SIM-swap chain viewer (historical IMSIs per MSISDN) | Valuable, but needs its own `resource.recon.msisdn_match` query design. Phase 2. | TODOS.md |
| Generic "why is this empty?" framework | Needs cross-panel design decision (all Customer360 panels) — separate design session | TODOS.md (design session) |
| Cross-org / B2B wholesale MVNO | Different schema, different compliance posture | Out of scope indefinitely |
| Historic IMSIs beyond 90-day retention | Not an ops use case; POPIA says delete | N/A |

---

## Architecture

### Data flow (4-phase cascade with short-circuit)

`resolveIMSIs` in `lookup_prod.go` walks phases in order. Any phase that returns non-empty short-circuits the remainder (except override, which STILL runs the cascade async for drift detection — scope item #5).

```
                INPUT: email (from Customer360 search)
                        │
                        ▼
  ┌──────────────────────────────────────────────────────────┐
  │ lookup_prod.go: email → related_party_id → billing[]     │
  └──────────────────────────────────────────────────────────┘
                        │
                        ▼
  ┌──────────────────────────────────────────────────────────┐
  │ Phase 0  IMSIOverride (SQLite customer_imsi_overrides)   │
  │          ── present? ──▶ return + async cascade (drift)  │
  └──────────────────────────────────────────────────────────┘
                        │ (absent)
                        ▼
  ┌──────────────────────────────────────────────────────────┐
  │ Phase 1  fetchIMSIsViaProductPath                        │
  │          product.product → jt_prod_rs_ref → resource_ref │
  └──────────────────────────────────────────────────────────┘
                        │ (empty)
                        ▼
  ┌──────────────────────────────────────────────────────────┐
  │ Phase 1.5  (CONDITIONAL on Phase 1 SIT probe)            │
  │           fetchIMSIsSvc — service.service.* path         │
  │           skipped if probe shows cascade already covers  │
  └──────────────────────────────────────────────────────────┘
                        │ (empty)
                        ▼
  ┌──────────────────────────────────────────────────────────┐
  │ Phase 2  fetchIMSIsByAccount                             │
  │          public.vw_service_account_state_latest          │
  │          keyed on financial_account_id | account_number  │
  └──────────────────────────────────────────────────────────┘
                        │ (empty)
                        ▼
  ┌──────────────────────────────────────────────────────────┐
  │ Phase 3  fetchIMSIsByUserID                              │
  │          vw_service_account_state_latest via             │
  │          service_accounts.subscriber → user_id           │
  └──────────────────────────────────────────────────────────┘
                        │
                        ▼
        dedupe by IMSI (view doesn't carry msisdn/iccid reliably)
                        │
                        ▼
        resource.resource.sim  → status + cmi_imsi + activated_at
                        │
                        ▼
        resource.recon.msisdn_match  → swap reconciliation
                        │
                        ▼
        imsi_lookup_audit WRITE (fail-closed — block response on failure)
                        │
                        ▼
        SIM Diagnostics panel render with source tag chips
```

### SIM state machine (inlined — no longer references the SUPERSEDED runbook)

```
        ┌──────────────┐    activate    ┌──────────────┐
        │  RESERVED    │───────────────▶│   ACTIVE     │◀─┐
        └──────────────┘                └──────┬───────┘  │
                                               │          │ reactivate
                                   suspend     │          │
                                               ▼          │
                                        ┌──────────────┐  │
                                        │  SUSPENDED   │──┘
                                        └──────┬───────┘
                                               │ cancel / port-out
                                               ▼
                                        ┌──────────────┐
                                        │  CANCELLED   │── retained 90d ──▶  purged
                                        └──────────────┘
```

Swap event on ACTIVE or SUSPENDED creates a new `resource.resource.sim` row; the old row stays ACTIVE until `resource.recon.msisdn_match` reconciliation catches up (`udm_imsi ↔ ib_imsi`).

The plan's panel does NOT filter by `status` — it shows RESERVED/ACTIVE/SUSPENDED/CANCELLED alike so ops can see suspended/cancelled SIMs while troubleshooting payment or port disputes. `status` is rendered as a chip.

### UI panel sketch (v2, post design-review)

Accent: **`#b980ff`** (violet) — distinct from existing `#00f0ff` cyan / `#6ff2a0` green / `#ffaa00` amber. Violet reads as "diagnostic / PII realm" and shares the chip colour with the `«redacted»` marker for visual continuity.

Lead with IMSI (the answer), demote MSISDN (the input). Cascade source shown as tag chips — filled if phase contributed, dim outline if not.

```
┌───────────────────────────────────────────────────────────────────────┐
│  SIM Diagnostics · 2                                     #b980ff     │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │ IMSI  655 01 123 456 7890            [ACTIVE]  swap 2025-11  │    │
│  │ MSISDN 084 123 4567 · ICCID 89270... · CMI 655 01 123 456    │    │
│  │ ▢override  ▣product  ▣view  □service  ▣recon                 │    │
│  │                                            [⋯ copy as…]      │    │
│  └──────────────────────────────────────────────────────────────┘    │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │ [BSS GAP — OVERRIDE]    084 987 6543                         │    │
│  │ cascade returned empty, no override set.                     │    │
│  │ likely: service-domain customer outside view.                │    │
│  │ ▢override  ▢product  ▢view  ▢service  ▢recon                 │    │
│  │                                            [set override]   │    │
│  └──────────────────────────────────────────────────────────────┘    │
└───────────────────────────────────────────────────────────────────────┘

Empty-state tags (D3 spec):
  NOT PROVISIONED   (red)     SIM ordered, not activated — check logistics.order
  SWAP IN FLIGHT    (amber)   view returned old ICCID, recon disagrees — wait 10m or force
  BSS GAP — OVERRIDE (violet) cascade empty, no override — [set override]

Copy menu (D4):
  ⋯ → copy raw IMSI · copy as theStation URL · copy as psql where-clause
```

## Backend surface

- `GET /api/v1/customer/360?email=X` — already exists, extend response with `sim_diagnostics[]`.
- `POST /api/v1/customer/imsi-override` — role-gated (`support-l2`), writes `imsi_override` + `imsi_lookup_audit`.
- `GET /api/v1/customer/imsi-audit?individual_id=X` — admin-only, returns the audit trail for a customer.

## Failure modes (carried from eng review)

| Codepath | Failure | Rescued? | User sees | Audit? |
|---|---|---|---|---|
| Phase 0 `loadIMSIOverrides` SQLite read | ConnectionError | Y (empty override, cascade proceeds) | Silent fallback to cascade | N (no override found) |
| Phase 1 `fetchIMSIsViaProductPath` timeout | TimeoutError | Y (5s deadline) | Cascade proceeds to phase 1.5/2 | Y (phase=timeout_p1) |
| Phase 1.5 `fetchIMSIsSvc` timeout (conditional) | TimeoutError | Y (5s deadline) | Cascade proceeds to phase 2 | Y (phase=timeout_p1.5) |
| Phase 2 `fetchIMSIsByAccount` timeout | TimeoutError | Y (5s deadline) | Cascade proceeds to phase 3 | Y (phase=timeout_p2) |
| Phase 3 `fetchIMSIsByUserID` timeout | TimeoutError | Y (5s deadline) | Panel renders empty-state with `BSS GAP` tag | Y (phase=exhausted) |
| All 4 phases return empty | — | Y | Panel shows `BSS GAP — OVERRIDE` with `[set override]` | Y (phase=exhausted) |
| `resource.recon.msisdn_match` empty | — | Y | No "swap detected" chip (clean fall-through) | N |
| Axiom connection lost mid-walk | ConnectionError | Y (backoff × 1, then fail-closed audit log) | "degraded" chip + stale cache if any | Y |
| Override write conflict | UniqueConstraint | Y | Toast "Override already exists — edit instead" | Y |
| Audit write itself fails | SQLiteError | **N — fail-closed** | HTTP 500 on the 360 response | ❌ — and that's the point; never serve without audit |
| POPIA redaction bypass attempt | — | Middleware rejects at `/axiom/peek` | 403 | Y |

## Observability

- Metric: `rain_sim_diagnostics_lookups_total{source, status}` — count of lookups by source path.
- Metric: `rain_sim_diagnostics_cascade_depth{winning_phase}` — which phase (`override` / `p1_product` / `p1_5_service` / `p2_view_account` / `p3_view_user` / `exhausted`) resolved. Tells you when phase 1 starts silently drifting behind later phases.
- Metric: `rain_sim_diagnostics_gap_rate` — `exhausted` count ÷ total. Proxy for how many customers the cascade fails on.
- Metric: `rain_sim_diagnostics_override_drift_total` — counts `override_drift_detected` events (scope #5 async check).
- Metric: `rain_sim_diagnostics_audit_write_failed_total` — should always be near-zero; non-zero means the fail-closed path fired.
- Alert: `gap_rate > 2%` for 1h → page on-call (Axiom schema drift or BSS data quality issue).
- Alert: `cascade_depth{winning_phase="p3_view_user"}` spiking over baseline → phase 1/2 drifting behind phase 3.
- Alert: `audit_write_failed_total > 0` for 5m → page on-call immediately (POPIA audit broken, lookups are 500-ing).
- Dashboard: "SIM Diagnostics" — gap rate over time, override-creation rate, cascade-depth histogram, drift events.

## Rollout

0. **Phase 0 — SHIPPED (commit `561befe`):** `/axiom/peek` redacts IMSI/MSISDN/ICCID/IMEI. POPIA blocker cleared.
0a. **Phase 0a (frontend follow-up):** Axiom Explorer shows "Redacted — PII" chip on redacted cells.
1. **Phase 1 — SIT cascade probe:** run 3 probes, commit `docs/axiom/sim-cascade-coverage.md`. Gate for decisions in Phase 2.
2. **Phase 2 — backend resolution:** `fetchIMSIsSvc` (if probe shows gap), `IMSISource` return shape, `sim_diagnostics[]` field on `/api/v1/customer/360`. Behind `sim_diagnostics_v2` feature flag, off by default. Verified against `baptista.manuel@rain.co.za`.
3. **Phase 3 — audit log:** `imsi_lookup_audit` table + writer with fail-closed semantics. Ship behind same flag.
4. **Phase 4 — UI panel:** SIM Diagnostics panel in Customer360, empty-state copy with 3-word tags. Flag-gated.
5. **Phase 5 — role gate pre-req:** confirm or add server-side role-claim evaluation. **Blocks Phase 6 if missing.**
6. **Phase 6 — override self-service UI:** role-gated, audited, with async drift detection. Flag-gated.
7. **Phase 7 — flag flip:** enable `sim_diagnostics_v2` globally. Deprecate manual psql path. `docs/axiom/imsi-lookup-plan.md` stays SUPERSEDED.

Rollback per phase: flag off for 2-6; 0 is forward-only (compliance) but trivial to extend the fragment list.
Migrations are additive (new tables only), zero alter-table on existing.

## Tests

### Scenario fixtures (SIT)
- `email_happy` — single billing, single ACTIVE product, product-side resolves.
- `email_service_domain` — swap customer, product-side empty, service-side populates.
- `email_swap_stale` — recent swap, product-side returns old ICCID, recon disagrees.
- `email_suspended` — product with `status='SUSPENDED'`, panel still shows.
- `email_no_account` — email exists, no billing account — empty state clean.
- `email_multi_household` — rainOne family plan, 4 billing accounts, 6 SIMs.
- `email_override_active` — IMSIOverride row exists, panel shows `source='override'`.
- `email_permission_denied` — non-`support-l2` role cannot see unredacted IMSI.

### Automated
- Backend: table-driven Go tests for `fetchIMSIsByAccount` ∪ `fetchIMSIsSvc`, mocked Axiom fixtures.
- Frontend: Vitest render tests for the three panel states (populated, partial, empty-with-diagnostic).
- E2E: Playwright journey — search → Customer360 → SIM Diagnostics visible → override flow.
- POPIA: test that `/api/v1/axiom/peek` returns `«redacted»` (the literal string used by `schema.go`) for `imsi/msisdn/iccid/imei/cmi_imsi/udm_imsi/ib_imsi/current_imsi/first_imsi` when caller lacks role. Regression test already shipped at `schema_test.go` in commit `561befe`; integration test covers the full `/api/v1/axiom/peek` round-trip.

---

## Implementation order (worktree-parallelisable)

| Lane | Steps | Depends on |
|---|---|---|
| A | ✅ Phase 0 shipped (commit `561befe`) + Phase 0a chip | — |
| B | Phase 1 SIT probe + cascade-coverage doc | — |
| C | Phase 2 backend (`IMSISource` shape, `fetchIMSIsSvc` conditional, 360 response) | B |
| D | Phase 3 audit table + fail-closed writer | — |
| E | Phase 4 UI panel (SIM Diagnostics) | C, D |
| F | Phase 5 role-gate pre-req check (auth middleware if needed) | — |
| G | Phase 6 override self-service UI | E, F |

Lanes B, D, F are independent — parallel worktrees. C gates on B. E gates on C+D. G gates on E+F.

## Known unknowns

1. **SIT cascade coverage** — do phases 1-3 already cover service-domain customers? Probe before writing `fetchIMSIsSvc`. If yes, scope item #2 collapses.
2. **Audit retention** — is 18 months correct for POPIA + rain ops policy? Needs InfoSec sign-off.
3. **SIT fixtures** — need 3 known customers (service-domain, post-swap, override-active) with stable emails for regression tests.
4. **Role-evaluation mechanism** — does the Go server have server-side session claims today, or do we need an auth-middleware PR first? Blocks scope item #5 until resolved.
5. **`«redacted»` chip copy** — sign-off on Explorer UI wording from ops so they don't read it as "broken".

---

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 1 | CLEAR | mode: SCOPE_EXPANSION, 6 proposals, 4 accepted, 2 deferred |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 v2 | AMENDED | 12 issues in v1; all folded into plan v2; Phase 0 SHIPPED at `561befe` |
| Design Review | `/plan-design-review` | UI/UX gaps | 1 | ISSUES_OPEN | 10 findings (2 critical: D3 empty-state copy, D10 redacted chip; 2 high: D1 accent collision, D2 info hierarchy) — all folded into v2 panel sketch |

**UNRESOLVED (after v2 amendments):**
- SIT cascade probe must run before Phase 2 (Known Unknown #1)
- Role-gate middleware audit must confirm server-side claim evaluation exists (Known Unknown #4)
- InfoSec sign-off on 18-month audit retention (Known Unknown #2)
- Ops copy sign-off on `«redacted»` chip wording (Known Unknown #5)

**VERDICT:** Phase 0 CLEARED and SHIPPED (`561befe`). Plan v2 incorporates all eng + design findings. Blocks before Phase 2 start: SIT probe + role-middleware audit. Design review cleared at panel-sketch level; needs re-confirmation once Phase 4 component lands.

### Design review findings (v1)
- **D1 HIGH** · accent `#ffd166` too close to `#ffaa00` warn — moved to `#b980ff` violet
- **D2 HIGH** · info hierarchy led with MSISDN not IMSI — flipped, IMSI leads
- **D3 CRITICAL** · empty-state copy spec'd with 3 tags + colours (`NOT PROVISIONED` / `SWAP IN FLIGHT` / `BSS GAP — OVERRIDE`)
- **D4 MEDIUM** · `[copy IMSI]` → kebab with theStation URL + psql-where variants
- **D5 MEDIUM** · cascade source as tag-chip row (filled vs outlined per phase)
- **D6 MEDIUM** · swap-date is clickable (history inline in Phase 5+)
- **D7 LOW** · `[set override]` disabled+tooltip for non-`support-l2` role
- **D8 LOW** · 320px/480px breakpoints for card + menu collapse
- **D9 MEDIUM** · `aria-label` on `HudStatusLed`, `font-variant-numeric: tabular-nums` on IMSI
- **D10 CRITICAL** · Redacted-PII chip spec for Axiom Explorer — needed THIS WEEK (Phase 0 shipped, chip doesn't exist yet)

### Eng review findings (v1)
1. **1A** — SIT-verify 4-phase cascade covers service-domain before writing `fetchIMSIsSvc` (may be redundant).
2. **1B** — `resolveIMSIs` return `[]IMSISource` not `[]int64` so panel can show resolution source.
3. **1C** — IMSIOverride short-circuit needs async observability pass for stale-override detection.
4. **2A** — **CRITICAL** — audit write fail-closed, not best-effort.
5. **3A** — **CRITICAL** — `schema.go:341` add `imsi/msisdn/iccid/imei` — ship Phase 0 alone.
6. **3B** — role gate must be server-side session claim, not `X-Rain-Role` header.
7. **3C** — `msisdn_hash` needs HMAC+salt or drop in favour of `individual_id`.
8. **4A** — dedupe key assumes msisdn/iccid present from view — confirm or change key to IMSI-only.
9. **6A** — **CRITICAL** — Phase 0 POPIA regression test blocks merge.
10. **8A** — add `rain_sim_diagnostics_cascade_depth{winning_phase}` metric.
11. **9A** — Explorer UI needs `«redacted»` chip copy after Phase 0.
12. **11A** — empty-state panel copy leads with a 3-word tag (`NOT PROVISIONED` / `SWAP IN FLIGHT` / `BSS GAP — OVERRIDE`).

### Eng review findings (summary)
1. **1A** — SIT-verify 4-phase cascade covers service-domain before writing `fetchIMSIsSvc` (may be redundant).
2. **1B** — `resolveIMSIs` return `[]IMSISource` not `[]int64` so panel can show resolution source.
3. **1C** — IMSIOverride short-circuit needs async observability pass for stale-override detection.
4. **2A** — **CRITICAL** — audit write fail-closed, not best-effort.
5. **3A** — **CRITICAL** — `schema.go:341` add `imsi/msisdn/iccid/imei` — ship Phase 0 alone.
6. **3B** — role gate must be server-side session claim, not `X-Rain-Role` header.
7. **3C** — `msisdn_hash` needs HMAC+salt or drop in favour of `individual_id`.
8. **4A** — dedupe key assumes msisdn/iccid present from view — confirm or change key to IMSI-only.
9. **6A** — **CRITICAL** — Phase 0 POPIA regression test blocks merge.
10. **8A** — add `rain_sim_diagnostics_cascade_depth{winning_phase}` metric.
11. **9A** — Explorer UI needs `«redacted»` chip copy after Phase 0.
12. **11A** — empty-state panel copy leads with a 3-word tag (`NOT PROVISIONED` / `SWAP IN FLIGHT` / `BSS GAP — OVERRIDE`).
