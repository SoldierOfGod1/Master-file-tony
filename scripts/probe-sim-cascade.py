#!/usr/bin/env python3
"""
Probe the existing 4-phase IMSI cascade in `lookup_prod.go` against a list
of known SIT customer emails. Goal: determine whether scope item #2 of
docs/axiom/sim-diagnostics-plan.md (a new `fetchIMSIsSvc` service-domain
fallback) is needed, or whether the existing cascade already covers
service-domain customers (SIM-swapped, number-ported, SIM-moved).

This is the Phase 1 gate before writing any backend code.

Usage:
    python scripts/probe-sim-cascade.py \\
        --emails sit-probe-emails.txt \\
        --out docs/axiom/sim-cascade-coverage.md

    # or pipe emails in
    cat emails.txt | python scripts/probe-sim-cascade.py --emails - --out ...

The emails file must contain one customer email per line, prefixed with
the scenario tag in square brackets:

    [swap]  a.customer@example.com
    [port]  b.customer@example.com
    [move]  c.customer@example.com
    [happy] baptista.manuel@rain.co.za

Tags are free-form but recommended: `swap`, `port`, `move`, `multi-sim`,
`suspended`, `happy`, `override-active`.

Run while the backend is up on :8080 AND has a live Axiom connection
configured. Read-only — every call goes through `/api/v1/customer/360`
which the backend enforces as read-only per the existing audit path.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

BASE = "http://127.0.0.1:8080/api/v1"
SCENARIO_RE = re.compile(r"^\s*\[([^\]]+)\]\s*(.+?)\s*$")

# SAFETY: refuse to run if the backend's primary connection isn't SIT-like.
# Rain has multiple Axiom clusters — prod, SIT-BSS, SIT-cluster. The primary
# is what `/api/v1/axiom/*` and `/api/v1/customer/360` route to. Running this
# probe against prod would emit real customer IMSIs into the coverage report,
# which then gets committed to git. That's the POPIA breach pattern we're
# trying to prevent. Override with --i-know-what-i-am-doing for unusual setups.
_SIT_HOST_HINTS = ("sit", "-sit.", ".sit.", "staging", "-stg.", ".stg.")
_PROD_HOST_HINTS = ("prod", "-prod.", ".prod.", ".rain.co.za")


@dataclass(frozen=True)
class ProbeInput:
    scenario: str
    email: str


@dataclass(frozen=True)
class ProbeResult:
    scenario: str
    email: str
    status: str  # "ok" | "no-360" | "no-account" | "no-products" | "no-imsi" | "error"
    billing_accounts: int
    products_total: int
    products_with_imsi: int
    imsis_returned: list[int]
    msisdns_returned: list[str]
    has_override: bool
    http_code: int
    error: str | None


def _get(path: str, **params: str) -> dict:
    qs = "&".join(
        f"{k}={urllib.parse.quote(str(v))}" for k, v in params.items() if v
    )
    url = f"{BASE}/{path}" + (f"?{qs}" if qs else "")
    try:
        with urllib.request.urlopen(url, timeout=60) as r:
            body = json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"HTTP {e.code} for {url}: {e.read().decode()[:200]}")
    if not body.get("success", True):
        raise RuntimeError(f"ERR {url}: {body.get('error')}")
    return body["data"]


def parse_emails(source: Iterable[str]) -> list[ProbeInput]:
    out: list[ProbeInput] = []
    for line_no, raw in enumerate(source, start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        m = SCENARIO_RE.match(line)
        if m:
            scenario, email = m.group(1), m.group(2)
        else:
            scenario, email = "untagged", line
        out.append(ProbeInput(scenario=scenario, email=email))
    return out


def probe(p: ProbeInput) -> ProbeResult:
    try:
        data = _get("customer/360", email=p.email)
    except RuntimeError as e:
        return ProbeResult(
            scenario=p.scenario, email=p.email, status="error",
            billing_accounts=0, products_total=0, products_with_imsi=0,
            imsis_returned=[], msisdns_returned=[], has_override=False,
            http_code=0, error=str(e),
        )

    billing = data.get("billing_accounts") or []
    products = data.get("products") or []
    overrides = data.get("imsi_overrides") or []

    imsis = [p["imsi"] for p in products if p.get("imsi")]
    msisdns = [p["msisdn"] for p in products if p.get("msisdn")]

    if not billing:
        status = "no-account"
    elif not products:
        status = "no-products"
    elif not imsis:
        status = "no-imsi"
    else:
        status = "ok"

    return ProbeResult(
        scenario=p.scenario, email=p.email, status=status,
        billing_accounts=len(billing),
        products_total=len(products),
        products_with_imsi=len(imsis),
        imsis_returned=imsis,
        msisdns_returned=msisdns,
        has_override=len(overrides) > 0,
        http_code=200,
        error=None,
    )


def render_report(results: list[ProbeResult]) -> str:
    now = dt.datetime.utcnow().strftime("%Y-%m-%d %H:%M UTC")
    by_scenario: dict[str, list[ProbeResult]] = {}
    for r in results:
        by_scenario.setdefault(r.scenario, []).append(r)

    covered = sum(1 for r in results if r.status == "ok")
    gap = sum(1 for r in results if r.status in ("no-imsi", "no-products"))

    verdict = _verdict(results)

    lines: list[str] = []
    lines.append("# SIM cascade coverage probe")
    lines.append("")
    lines.append(f"**Generated:** {now}")
    lines.append(f"**Probes run:** {len(results)} · **IMSI returned:** {covered} · **Coverage gap:** {gap}")
    lines.append("")
    lines.append(f"**Verdict:** {verdict}")
    lines.append("")
    lines.append("## Results by scenario")
    lines.append("")

    for scenario, rs in sorted(by_scenario.items()):
        lines.append(f"### `[{scenario}]`")
        lines.append("")
        lines.append("| Email | Status | Billing | Products | w/ IMSI | Override? | IMSIs | MSISDNs |")
        lines.append("|---|---|---:|---:|---:|---|---|---|")
        for r in rs:
            imsi_cell = ", ".join(str(i) for i in r.imsis_returned[:3]) or "—"
            if len(r.imsis_returned) > 3:
                imsi_cell += f" (+{len(r.imsis_returned) - 3})"
            msisdn_cell = ", ".join(r.msisdns_returned[:3]) or "—"
            override = "Y" if r.has_override else "—"
            status = r.status
            if r.error:
                status = f"error: {r.error[:80]}"
            lines.append(
                f"| `{r.email}` | {status} | {r.billing_accounts} | {r.products_total} | "
                f"{r.products_with_imsi} | {override} | `{imsi_cell}` | `{msisdn_cell}` |"
            )
        lines.append("")

    lines.append("## Implications for scope item #2 (`fetchIMSIsSvc`)")
    lines.append("")
    lines.append(_implications(results))
    lines.append("")
    lines.append("## Raw JSON")
    lines.append("")
    lines.append("```json")
    lines.append(json.dumps([r.__dict__ for r in results], indent=2))
    lines.append("```")
    return "\n".join(lines) + "\n"


def _verdict(results: list[ProbeResult]) -> str:
    if not results:
        return "NO DATA — provide at least 3 SIT probe inputs"
    non_happy_scenarios = [r for r in results if r.scenario not in ("happy", "untagged")]
    if not non_happy_scenarios:
        return "INSUFFICIENT — happy-path only; add swap / port / move / suspended probes"
    gap = [r for r in non_happy_scenarios if r.status in ("no-imsi", "no-products", "no-account")]
    if not gap:
        return (
            "CASCADE COVERS SERVICE-DOMAIN — scope item #2 (`fetchIMSIsSvc`) "
            "collapses to `source` tagging only; no new query needed."
        )
    if len(gap) == len(non_happy_scenarios):
        return (
            "CASCADE MISSES ALL SERVICE-DOMAIN — scope item #2 required; "
            "implement `fetchIMSIsSvc` as phase 1.5."
        )
    return (
        f"CASCADE PARTIAL — {len(gap)}/{len(non_happy_scenarios)} service-domain "
        f"probes returned no IMSI. scope item #2 recommended but not strictly needed "
        f"for all paths."
    )


def _implications(results: list[ProbeResult]) -> str:
    non_happy = [r for r in results if r.scenario not in ("happy", "untagged")]
    gap = [r for r in non_happy if r.status in ("no-imsi", "no-products", "no-account")]
    ok = [r for r in non_happy if r.status == "ok"]

    bullets: list[str] = []
    if ok:
        bullets.append(
            f"- {len(ok)} service-domain probes resolved an IMSI via the current cascade. "
            "Implies phases 0-3 already walk into service-domain data via "
            "`vw_service_account_state_latest`."
        )
    if gap:
        bullets.append(
            f"- {len(gap)} service-domain probes returned no IMSI. Implies the view does NOT "
            "cover those customers. Next step: inspect slog at phase-exhausted log lines for "
            "each email and confirm the specific failure path. Then add `fetchIMSIsSvc`."
        )
    if not bullets:
        bullets.append(
            "- No service-domain probes ran. Cannot conclude on scope item #2 coverage. "
            "Re-run with `[swap]`/`[port]`/`[move]` scenarios."
        )
    return "\n".join(bullets)


def _assert_sit_primary(force: bool) -> None:
    """Refuse to run unless the backend's primary Axiom connection is SIT-like."""
    try:
        data = _get("connections")
    except RuntimeError as e:
        print(
            f"cannot verify cluster ({e}). Refusing to run against an unknown "
            f"target. Re-run with --i-know-what-i-am-doing if you've verified manually.",
            file=sys.stderr,
        )
        if not force:
            raise SystemExit(3)
        return

    primary = next((c for c in data if c.get("is_primary")), None)
    if not primary:
        print("no primary connection configured; refusing to probe.", file=sys.stderr)
        if not force:
            raise SystemExit(3)
        return

    host = (primary.get("host") or "").lower()
    label = primary.get("label") or primary.get("id") or "<unknown>"
    looks_sit = any(h in host for h in _SIT_HOST_HINTS)
    looks_prod = any(h in host for h in _PROD_HOST_HINTS) and not looks_sit

    print(f"backend primary: {label}  host={host}", file=sys.stderr)

    if looks_prod:
        print(
            "\n!!! primary looks like PROD !!!\n"
            f"   host: {host}\n"
            "Running this probe would emit real customer IMSIs into the\n"
            "coverage report. That is a POPIA incident waiting to happen.\n"
            "Switch the backend's primary connection to a SIT cluster and\n"
            "re-run. To override (rare — e.g. a SIT host that contains\n"
            "'prod' for some reason), pass --i-know-what-i-am-doing.\n",
            file=sys.stderr,
        )
        if not force:
            raise SystemExit(3)
        return

    if not looks_sit:
        print(
            f"primary host ({host}) doesn't match known SIT naming. "
            f"Refusing to run. Pass --i-know-what-i-am-doing if correct.",
            file=sys.stderr,
        )
        if not force:
            raise SystemExit(3)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--emails", required=True, help="Path to emails file, or '-' for stdin")
    ap.add_argument("--out", required=True, help="Output markdown path (typically docs/axiom/sim-cascade-coverage.md)")
    ap.add_argument(
        "--i-know-what-i-am-doing",
        dest="force",
        action="store_true",
        help="Bypass the SIT-only primary-connection guard (rare; POPIA risk).",
    )
    args = ap.parse_args()

    _assert_sit_primary(args.force)

    if args.emails == "-":
        source = sys.stdin
    else:
        source = Path(args.emails).open("r", encoding="utf-8")

    try:
        inputs = parse_emails(source)
    finally:
        if args.emails != "-":
            source.close()

    if not inputs:
        print("no emails to probe (file empty or all commented)", file=sys.stderr)
        return 2

    print(f"probing {len(inputs)} SIT customer(s) via {BASE}/customer/360 ...", file=sys.stderr)

    results: list[ProbeResult] = []
    for i, p in enumerate(inputs, start=1):
        print(f"  [{i}/{len(inputs)}] [{p.scenario}] {p.email} ...", end="", file=sys.stderr, flush=True)
        r = probe(p)
        results.append(r)
        print(f" {r.status}", file=sys.stderr)

    report = render_report(results)
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(report, encoding="utf-8")
    print(f"wrote {out_path}", file=sys.stderr)
    print(f"verdict: {_verdict(results)}", file=sys.stderr)
    return 0 if all(r.status != "error" for r in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())
