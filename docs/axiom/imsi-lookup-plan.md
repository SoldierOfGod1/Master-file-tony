# Plan — IMSI lookup from customer email (Axiom Prod)

**Status:** SUPERSEDED — see `docs/axiom/sim-diagnostics-plan.md`. This runbook remains internal-only for L3 escalation scenarios where the UI pathway is unavailable. Do not publish as the ops answer.
**Original status:** Draft — pending eng review
**Author:** Baptista (automation specialist)
**Target:** Add a read-only IMSI-from-email lookup to the rain ops toolkit, reusable for any rain customer
**Example subject:** `baptista.manuel@rain.co.za`
**Data classification:** PII (email, MSISDN, IMSI, ICCID) + customer identity — POPIA applies

---

## Goal

Given a rain customer's email address, return all active IMSIs attached to their products, along with MSISDN, ICCID, product name, and SIM status. Read-only.

## Data path (TM Forum SID chain)

```
email (party.party.contact_medium)
  → individual_id
    → related_party_id (party.party.related_party)
      → billing_account_id (account.account.billing_account)
        → product_id (product.product.product)   # the "product item"
          → product_offering_id                  # offering + category
          → jt_prod_rs_ref                       # junction
            → resource_ref_id (product.product.resource_ref)
              → resource_ref.value (iccid | msisdn)
                → resource.resource.sim.imsi     # canonical IMSI
```

Cross-DB: `party`, `account`, `product`, `resource` (4 databases, 4 queries — no single SQL statement).

## Proposed SQL (read-only, LIMIT everywhere)

### Step 1 — party DB: email → related_party_id
```sql
-- \c party
SELECT rp.id AS related_party_id,
       i.id  AS individual_id,
       cm.email_address
FROM   party.contact_medium cm
JOIN   party.individual      i  ON i.id = cm.individual_id
JOIN   party.related_party   rp ON rp.individual_id = i.id
WHERE  cm.email_address = :email
  AND  cm.medium_type   = 'email'
LIMIT  50;
```

### Step 2 — account DB: related_party_id → billing_account_id
```sql
-- \c account
SELECT id AS billing_account_id, state, account_type
FROM   account.billing_account
WHERE  related_party_id = :related_party_id
LIMIT  50;
```

### Step 3 — product DB: billing → product item → resource_ref
```sql
-- \c product
SELECT p.id  AS product_id,
       p.product_offering_id,
       p.product_specification_id,
       p.iccid,
       p.imei,
       p.status,
       rr.name   AS ref_name,
       rr.value  AS ref_value
FROM   product.product        p
JOIN   product.jt_prod_rs_ref j  ON j.product_id = p.id
JOIN   product.resource_ref   rr ON rr.id = j.resource_ref_id
WHERE  p.billing_account_id = :billing_account_id
  AND  p.status = 'ACTIVE'
LIMIT  200;
```

### Step 4 — resource DB: iccid|msisdn → IMSI
```sql
-- \c resource
SELECT iccid, msisdn, imsi, cmi_imsi, status, activated_at, on_hss, plmn
FROM   resource.sim
WHERE  iccid  = ANY(:iccids)
   OR  msisdn = ANY(:msisdns)
LIMIT  200;
```

## Where this will run

Option A — `/axiom/*` Explorer endpoints (preferred): `GET /api/v1/axiom/peek` with the `db` param switched per step. PII redaction is already enforced server-side.

Option B — direct psql via `axiom-prod-pg-cluster.rain.co.za:5433` with my LDAP password. Read-only role. Faster for ad-hoc.

## Open questions (for eng review)

1. Should we query `product.product.product` directly or treat `p.iccid` as authoritative and skip `jt_prod_rs_ref` when it's populated?
2. What's the right way to handle multi-SIM customers (rainOne, multiple phone products)?
3. Do we need to join `service.service.resource_ref` for products that route via service-domain instead?
4. Should `status = 'ACTIVE'` filter be configurable (some flows want historic IMSIs)?
5. Handle SIM swaps — if `resource_ref.value = iccid_old` but the SIM was swapped, the current IMSI lives on a different `resource.resource.sim` row. How do we follow the swap chain?

---

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | ISSUES_OPEN | 10 issues, 2 critical gaps |
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | not run (ops runbook, not product change) |
| Design Review | `/plan-design-review` | UI/UX | 0 | — | n/a (no UI) |
| DX Review | `/plan-devex-review` | Developer experience | 0 | — | n/a |
| Outside Voice | `/codex plan-review` | 2nd AI opinion | 0 | — | skipped |

**UNRESOLVED:** 10 open issues (1A-1F, 2A-2B, 4A-4B). Critical: service-domain blind spot (1A), SIM-swap staleness (1E), missing IMSI/MSISDN redaction on `/axiom/peek` (TODO #1 — blocks runbook publish).

**VERDICT:** NOT CLEARED — eng review found 2 critical correctness gaps + 1 critical security prerequisite. Address TODO #1 (redaction set) and issues 1A + 1E before publishing runbook. Once resolved, re-run `/plan-eng-review` for CLEAR status.
