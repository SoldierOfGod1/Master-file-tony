#!/usr/bin/env python3
"""
Crawl the rain ClickHouse cluster directly via the HTTP interface and
dump a structured catalogue to docs/darknoc/.

Mirrors the shape of crawl-axiom.py so the operator has the same
artefacts available for ClickHouse — Cybertron and the chat tool
read clickhouse-prod-columns.json to compose queries that don't
hallucinate column names.

The crawl runs against the connection registered in the command-centre
backend as `clickhouse-prod` (driver=clickhouse). Falls back to env
vars when the backend is offline:

    CLICKHOUSE_HOST          host (default localhost)
    CLICKHOUSE_PORT          port (default 8443 for https, 8123 for http)
    CLICKHOUSE_DATABASE      default db (default 'default')
    CLICKHOUSE_USER          username (default 'default')
    CLICKHOUSE_PASSWORD      password (no default)
    CLICKHOUSE_INSECURE      'true' to use http instead of https

The crawler is READ-ONLY. It only runs SHOW DATABASES / TABLES and
SELECT against system.columns.

Produces three artefacts:
  1. clickhouse-prod-catalogue.json  full catalogue (db -> table -> columns)
  2. clickhouse-prod-summary.md      human-readable map
  3. clickhouse-prod-columns.json    flat (db, table, column) for search
"""

from __future__ import annotations

import json
import logging
import os
import sys
import urllib.parse
import urllib.request
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

logger = logging.getLogger("crawl-clickhouse")

OUT = Path(__file__).resolve().parent.parent / "docs" / "darknoc"
OUT.mkdir(parents=True, exist_ok=True)

# System databases we never want in the catalogue. ClickHouse exposes
# `system`, `INFORMATION_SCHEMA`, `information_schema`, `default` —
# the first three are noise; `default` we keep because rain telemetry
# may live there.
SYSTEM_DBS = {"system", "INFORMATION_SCHEMA", "information_schema"}


@dataclass(frozen=True)
class Conn:
    """Connection settings resolved from backend connections row or env."""

    host: str
    port: str
    database: str
    user: str
    password: str
    secure: bool

    def url(self, db: str | None = None) -> str:
        scheme = "https" if self.secure else "http"
        target_db = db or self.database
        qs = f"database={urllib.parse.quote(target_db)}" if target_db else ""
        return f"{scheme}://{self.host}:{self.port}/?{qs}"


def resolve_connection() -> Conn:
    """
    Try the running backend's /api/v1/connections first; fall back to
    env vars. Backend path keeps the password out of shell history.
    """

    backend_url = "http://127.0.0.1:8080/api/v1/connections"

    def to_conn(c: dict) -> Conn:
        insecure = (c.get("ssl_mode") or "").lower() == "disable"
        return Conn(
            host=c.get("host") or "",
            port=c.get("port") or ("8123" if insecure else "8443"),
            database=c.get("database") or "default",
            user=c.get("user") or "",
            password=c.get("password") or "",
            secure=not insecure,
        )

    try:
        with urllib.request.urlopen(backend_url, timeout=5) as r:
            body = json.loads(r.read().decode("utf-8"))
        if body.get("success"):
            rows = [c for c in body.get("data", []) if c.get("driver") == "clickhouse"]
            # Prefer the configured ID; fall back to any clickhouse row
            # (matches the auto-discovery in internal/darknoc/clickhouse.go
            # so the operator doesn't have to rename their existing row).
            for preferred in ("clickhouse-prod", "clickhouse-main", "clickhouse-primary"):
                for c in rows:
                    if c.get("id") == preferred:
                        # The list endpoint returns a masked password
                        # (`••••XXXX`). The crawler needs the real
                        # password to connect — fall through to env
                        # vars when the masked value is detected.
                        if (c.get("password") or "").startswith("•"):
                            logger.warning(
                                "backend connection %s has masked password "
                                "in API response — set CLICKHOUSE_PASSWORD env var "
                                "to authorise the crawler",
                                c.get("id"),
                            )
                            break
                        return to_conn(c)
            if rows and not (rows[0].get("password") or "").startswith("•"):
                return to_conn(rows[0])
    except Exception as exc:
        logger.warning("backend lookup failed (%s) — falling back to env vars", exc)

    insecure = (os.environ.get("CLICKHOUSE_INSECURE") or "").lower() == "true"
    return Conn(
        host=os.environ.get("CLICKHOUSE_HOST", "localhost"),
        port=os.environ.get("CLICKHOUSE_PORT", "8123" if insecure else "8443"),
        database=os.environ.get("CLICKHOUSE_DATABASE", "default"),
        user=os.environ.get("CLICKHOUSE_USER", "default"),
        password=os.environ.get("CLICKHOUSE_PASSWORD", ""),
        secure=not insecure,
    )


def query(conn: Conn, sql: str, db: str | None = None) -> list[dict]:
    """POST a SQL query to ClickHouse via HTTP, parse JSONEachRow."""

    sql = sql.rstrip().rstrip(";") + " FORMAT JSONEachRow"
    req = urllib.request.Request(
        conn.url(db),
        data=sql.encode("utf-8"),
        method="POST",
        headers={"Content-Type": "text/plain; charset=utf-8"},
    )
    if conn.user:
        creds = f"{conn.user}:{conn.password}".encode("utf-8")
        import base64
        req.add_header("Authorization", b"Basic " + base64.b64encode(creds))
    with urllib.request.urlopen(req, timeout=30) as r:
        body = r.read().decode("utf-8")
    rows: list[dict] = []
    for line in body.splitlines():
        line = line.strip()
        if not line:
            continue
        rows.append(json.loads(line))
    return rows


def list_databases(conn: Conn) -> list[str]:
    rows = query(conn, "SELECT name FROM system.databases ORDER BY name")
    return [r["name"] for r in rows if r["name"] not in SYSTEM_DBS]


def list_tables(conn: Conn, db: str) -> list[dict]:
    sql = (
        "SELECT name, engine, total_rows, total_bytes "
        f"FROM system.tables WHERE database = '{db}' "
        "ORDER BY name"
    )
    return query(conn, sql)


def list_columns(conn: Conn, db: str, table: str) -> list[dict]:
    sql = (
        "SELECT name, type, default_kind, comment "
        f"FROM system.columns WHERE database = '{db}' AND table = '{table}' "
        "ORDER BY position"
    )
    return query(conn, sql)


def main() -> int:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    conn = resolve_connection()
    if not conn.host or not conn.user:
        logger.error("ClickHouse connection unresolved — set CLICKHOUSE_* env vars or register clickhouse-prod in Settings.")
        return 1
    logger.info("crawling %s as %s (db=%s, secure=%s)", conn.host, conn.user, conn.database, conn.secure)

    catalogue: dict = {"host": conn.host, "databases": {}}
    flat_columns: list[dict] = []

    try:
        dbs = list_databases(conn)
    except Exception as exc:
        logger.error("list databases failed: %s", exc)
        return 2

    logger.info("found %d databases", len(dbs))

    for db in dbs:
        try:
            tables = list_tables(conn, db)
        except Exception as exc:
            logger.warning("list tables for %s failed: %s", db, exc)
            continue
        catalogue["databases"][db] = {"tables": {}}
        for t in tables:
            tname = t["name"]
            try:
                cols = list_columns(conn, db, tname)
            except Exception as exc:
                logger.warning("columns for %s.%s failed: %s", db, tname, exc)
                cols = []
            catalogue["databases"][db]["tables"][tname] = {
                "engine": t.get("engine"),
                "rows": t.get("total_rows"),
                "bytes": t.get("total_bytes"),
                "columns": cols,
            }
            for c in cols:
                flat_columns.append(
                    {
                        "database": db,
                        "table": tname,
                        "column": c["name"],
                        "type": c["type"],
                        "default": c.get("default_kind") or "",
                        "comment": c.get("comment") or "",
                    }
                )
        logger.info("%s: %d tables", db, len(tables))

    cat_path = OUT / "clickhouse-prod-catalogue.json"
    cat_path.write_text(json.dumps(catalogue, indent=2, default=str), encoding="utf-8")
    logger.info("wrote %s", cat_path)

    cols_path = OUT / "clickhouse-prod-columns.json"
    cols_path.write_text(json.dumps(flat_columns, indent=2, default=str), encoding="utf-8")
    logger.info("wrote %s (%d entries)", cols_path, len(flat_columns))

    summary_lines: list[str] = ["# ClickHouse · clickhouse-prod catalogue\n"]
    summary_lines.append(f"Host: `{conn.host}` · default database `{conn.database}`\n")
    summary_lines.append(f"\n{len(catalogue['databases'])} databases discovered.\n")
    by_db: dict[str, int] = defaultdict(int)
    for db, body in catalogue["databases"].items():
        by_db[db] = len(body["tables"])
    for db in sorted(by_db, key=lambda k: -by_db[k]):
        summary_lines.append(f"## {db} — {by_db[db]} tables\n")
        for tname, t in sorted(catalogue["databases"][db]["tables"].items()):
            rows = t.get("rows") or "?"
            cols = len(t.get("columns") or [])
            summary_lines.append(f"- `{tname}` · {rows} rows · {cols} columns")
        summary_lines.append("")
    summary_path = OUT / "clickhouse-prod-summary.md"
    summary_path.write_text("\n".join(summary_lines), encoding="utf-8")
    logger.info("wrote %s", summary_path)

    return 0


if __name__ == "__main__":
    sys.exit(main())
