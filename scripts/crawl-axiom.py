#!/usr/bin/env python3
"""
Crawl the live Axiom prod cluster via the command-centre backend and dump
a structured catalogue to docs/axiom/.

Run while the backend is up on :8080.

Produces three artefacts:
  1. axiom-prod-catalogue.json   — full machine-readable catalogue
  2. axiom-prod-summary.md       — human-readable top-level map
  3. axiom-prod-columns.json     — flat (db, schema, table, column) for search

The crawler is READ-ONLY. It only hits /axiom/databases, /axiom/schemas,
/axiom/tables, /axiom/columns — all of which the backend enforces as
read-only system queries.
"""

import json
import os
import sys
import time
import urllib.request
import urllib.error
from pathlib import Path
from collections import defaultdict

BASE = "http://127.0.0.1:8080/api/v1/axiom"
OUT = Path(__file__).resolve().parent.parent / "docs" / "axiom"
OUT.mkdir(parents=True, exist_ok=True)

# DBs worth deep-crawling. We skip admin/toolbelt DBs (postgres, pghero,
# hoppscotch, test, sftpgo) to keep the catalogue focused on BSS data.
PRIORITY_DBS = [
    "customer", "payment", "party", "service", "account", "product",
    "snowflake", "rica", "subscription", "communication", "resource",
    "logistics", "prepay", "shopping", "porting", "risk", "raingo",
    "geographic", "prompt", "stock", "raindrop", "document", "trouble",
    "promotion", "quote", "digital", "entity",
]

# Per-table row-estimate floor — we skip columns for tables with fewer
# rows to keep the catalogue slim. Override via env.
MIN_ROWS_FOR_COLUMNS = int(os.environ.get("MIN_ROWS", "1"))
# Per-schema cap on tables we column-probe.
MAX_TABLES_PER_SCHEMA = int(os.environ.get("MAX_TABLES", "50"))

def get(path, **params):
    qs = "&".join(f"{k}={urllib.parse.quote(str(v))}" for k, v in params.items() if v)
    url = f"{BASE}/{path}" + (f"?{qs}" if qs else "")
    try:
        with urllib.request.urlopen(url, timeout=60) as r:
            body = json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        raise SystemExit(f"HTTP {e.code} for {url}: {e.read().decode()}")
    if not body.get("success", True):
        raise SystemExit(f"ERR {url}: {body.get('error')}")
    return body["data"]


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    print("→ enumerating databases on the primary cluster…")
    dbs = get("databases")
    db_names = [d["name"] for d in dbs]
    print(f"  found {len(dbs)} databases, {sum(x['size_mb'] for x in dbs)/1024:.1f} GB total")

    catalogue = {
        "cluster": "axiom-prod",
        "discovered_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "databases": [],
    }
    flat_columns = []

    crawl = [d for d in dbs if d["name"] in PRIORITY_DBS]
    # Sort by declared priority
    crawl.sort(key=lambda d: PRIORITY_DBS.index(d["name"]))
    print(f"→ deep-crawling {len(crawl)} priority databases")

    for i, db in enumerate(crawl, 1):
        db_name = db["name"]
        print(f"  [{i}/{len(crawl)}] {db_name} ({db['size_mb']}MB)")
        db_entry = {
            "name": db_name,
            "size_mb": db["size_mb"],
            "owner": db["owner"],
            "schemas": [],
        }

        try:
            schemas = get("schemas", db=db_name) or []
        except SystemExit as e:
            print(f"      ! {e}")
            db_entry["error"] = str(e)
            catalogue["databases"].append(db_entry)
            continue

        for sch in schemas:
            sch_name = sch["name"]
            # Skip unimportant system-ish schemas
            if sch_name in ("partman", "foreign_schema", "pg_toast"):
                continue
            try:
                tables = get("tables", db=db_name, schema=sch_name) or []
            except SystemExit:
                continue
            # Top N tables by row estimate
            tables.sort(key=lambda t: -t.get("row_estimate", 0))
            tables_slice = tables[:MAX_TABLES_PER_SCHEMA]
            sch_entry = {
                "name": sch_name,
                "owner": sch.get("owner", ""),
                "table_count": sch["table_count"],
                "tables": [],
            }
            for t in tables_slice:
                t_name = t["name"]
                t_entry = {
                    "name": t_name,
                    "type": t["type"],
                    "row_estimate": t["row_estimate"],
                    "likely_domain": t.get("likely_domain", ""),
                    "columns": [],
                }
                if t["row_estimate"] >= MIN_ROWS_FOR_COLUMNS:
                    try:
                        cols = get("columns", db=db_name, schema=sch_name, table=t_name) or []
                    except SystemExit:
                        cols = []
                    for c in cols:
                        t_entry["columns"].append({
                            "name": c["name"],
                            "data_type": c["data_type"],
                            "nullable": c["nullable"],
                        })
                        flat_columns.append({
                            "db": db_name,
                            "schema": sch_name,
                            "table": t_name,
                            "column": c["name"],
                            "data_type": c["data_type"],
                            "nullable": c["nullable"],
                            "row_estimate": t["row_estimate"],
                        })
                sch_entry["tables"].append(t_entry)
            db_entry["schemas"].append(sch_entry)
        catalogue["databases"].append(db_entry)

    # Write machine-readable
    (OUT / "axiom-prod-catalogue.json").write_text(
        json.dumps(catalogue, indent=2), encoding="utf-8",
    )
    (OUT / "axiom-prod-columns.json").write_text(
        json.dumps(flat_columns, indent=1), encoding="utf-8",
    )
    print(f"✓ wrote {OUT / 'axiom-prod-catalogue.json'}")
    print(f"  {len(flat_columns):,} column rows across "
          f"{sum(len(d['schemas']) for d in catalogue['databases'])} schemas")

    # Write summary markdown
    md = ["# Axiom Prod Catalogue",
          f"",
          f"Cluster: `axiom-prod-pg-cluster.rain.co.za:5433`  ",
          f"Discovered: `{catalogue['discovered_at']}`  ",
          f"Databases crawled: **{len(catalogue['databases'])}** of {len(dbs)} total  ",
          f"Columns indexed: **{len(flat_columns):,}**",
          f"",
          f"## Top-level map",
          f"",
          f"| DB | Size | Schemas | Tables crawled |",
          f"|---|---|---|---|"]
    for d in sorted(catalogue["databases"], key=lambda x: -x["size_mb"]):
        size = f"{d['size_mb']/1024:.1f} GB" if d["size_mb"] >= 1024 else f"{d['size_mb']} MB"
        tcount = sum(len(s["tables"]) for s in d.get("schemas", []))
        md.append(f"| `{d['name']}` | {size} | {len(d.get('schemas', []))} | {tcount} |")

    md.append("")
    md.append("## Heaviest tables across the cluster (by row estimate)")
    md.append("")
    md.append("| Rows | Table |")
    md.append("|---:|---|")
    all_tables = []
    for d in catalogue["databases"]:
        for s in d.get("schemas", []):
            for t in s.get("tables", []):
                all_tables.append((t["row_estimate"], f"`{d['name']}.{s['name']}.{t['name']}`"))
    for rows, name in sorted(all_tables, reverse=True)[:25]:
        md.append(f"| {rows:,} | {name} |")

    md.append("")
    md.append("## Likely cross-DB join keys (by column-name frequency)")
    md.append("")
    # Gather counts of popular id-ish column names
    name_counts = defaultdict(int)
    name_dbs = defaultdict(set)
    for c in flat_columns:
        cn = c["column"].lower()
        if cn.endswith("_id") or cn in ("msisdn", "email", "username", "id"):
            name_counts[cn] += 1
            name_dbs[cn].add(c["db"])
    md.append("| column | occurrences | databases present |")
    md.append("|---|---:|---|")
    for name, cnt in sorted(name_counts.items(), key=lambda x: -x[1])[:30]:
        md.append(f"| `{name}` | {cnt} | {len(name_dbs[name])} |")

    (OUT / "axiom-prod-summary.md").write_text("\n".join(md), encoding="utf-8")
    print(f"✓ wrote {OUT / 'axiom-prod-summary.md'}")


if __name__ == "__main__":
    main()
