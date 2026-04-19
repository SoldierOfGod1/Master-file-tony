"""MCP server wiring.

Exposes four tools over stdio:

- `rain_search`               — full-text search across the whole docs site.
- `rain_get_page`             — fetch a specific page, return markdown.
- `rain_list_sections`        — list the top-level sections on the site.
- `rain_list_pages_in_section`— enumerate pages under a section (or filtered).

The client is constructed lazily on first tool call so startup is fast and
the MCP registration happens even if the network is briefly unavailable.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import sys
from collections import Counter
from typing import Any

from mcp.server import Server, NotificationOptions
from mcp.server.stdio import stdio_server
from mcp.server.models import InitializationOptions
from mcp.types import TextContent, Tool

from rain_mcp.client import RainDocsClient, BASE_URL
from rain_mcp.search import search as search_docs

# Logs go to stderr so stdout stays clean for the MCP protocol.
logging.basicConfig(
    level=os.environ.get("RAIN_MCP_LOG", "INFO"),
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    stream=sys.stderr,
)
log = logging.getLogger("rain_mcp")

app: Server = Server("rain-docs")
_client: RainDocsClient | None = None


def _get_client() -> RainDocsClient:
    """Construct the HTTP client on first use. Thread-safe enough for stdio
    servers which are single-threaded."""
    global _client
    if _client is None:
        base = os.environ.get("RAIN_DOCS_BASE_URL", BASE_URL)
        _client = RainDocsClient(base_url=base)
        log.info("rain docs client bound to %s", base)
    return _client


# ---- Tool definitions ----------------------------------------------------

TOOLS: list[Tool] = [
    Tool(
        name="rain_search",
        description=(
            "Search rain's internal software engineering documentation "
            "(software-engineering.docs.rain.co.za). Returns a ranked list of "
            "matching pages with titles, URL paths and short snippets. Follow "
            "up with `rain_get_page` to read the full content of any hit."
        ),
        inputSchema={
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": "Natural-language or keyword query.",
                },
                "limit": {
                    "type": "integer",
                    "description": "Maximum number of hits to return (default 10, max 30).",
                    "default": 10,
                    "minimum": 1,
                    "maximum": 30,
                },
            },
            "required": ["query"],
        },
    ),
    Tool(
        name="rain_get_page",
        description=(
            "Fetch one page from rain's engineering docs and return its body "
            "as markdown. `path` is the relative URL path from the search "
            "hit's `location` field, e.g. 'architecture/api-design/'."
        ),
        inputSchema={
            "type": "object",
            "properties": {
                "path": {
                    "type": "string",
                    "description": "Site-relative path, e.g. 'architecture/api-design/'.",
                },
            },
            "required": ["path"],
        },
    ),
    Tool(
        name="rain_list_sections",
        description=(
            "List the top-level sections on the rain docs site with a page "
            "count for each. Use this to orient yourself before searching."
        ),
        inputSchema={"type": "object", "properties": {}},
    ),
    Tool(
        name="rain_list_pages_in_section",
        description=(
            "List every page within a given top-level section, optionally "
            "filtered by a substring in the title or path."
        ),
        inputSchema={
            "type": "object",
            "properties": {
                "section": {
                    "type": "string",
                    "description": "Section slug (e.g. 'architecture', 'backend', 'frontend').",
                },
                "filter": {
                    "type": "string",
                    "description": "Optional case-insensitive substring filter applied to title + path.",
                },
                "limit": {
                    "type": "integer",
                    "description": "Max entries to return (default 50, max 500).",
                    "default": 50,
                    "minimum": 1,
                    "maximum": 500,
                },
            },
            "required": ["section"],
        },
    ),
]


@app.list_tools()
async def list_tools() -> list[Tool]:  # noqa: D401 — MCP contract
    return TOOLS


@app.call_tool()
async def call_tool(name: str, arguments: dict[str, Any]) -> list[TextContent]:  # noqa: D401
    client = _get_client()

    try:
        if name == "rain_search":
            return await asyncio.to_thread(_tool_search, client, arguments)
        if name == "rain_get_page":
            return await asyncio.to_thread(_tool_get_page, client, arguments)
        if name == "rain_list_sections":
            return await asyncio.to_thread(_tool_list_sections, client)
        if name == "rain_list_pages_in_section":
            return await asyncio.to_thread(_tool_list_pages, client, arguments)
        return [TextContent(type="text", text=f"unknown tool: {name}")]
    except Exception as err:  # surface errors cleanly to the caller
        log.exception("tool %s failed", name)
        return [TextContent(type="text", text=f"error: {err}")]


# ---- Tool implementations -------------------------------------------------

def _tool_search(client: RainDocsClient, args: dict[str, Any]) -> list[TextContent]:
    query = str(args.get("query", "")).strip()
    if not query:
        return [TextContent(type="text", text="error: query is required")]
    limit = min(max(int(args.get("limit", 10)), 1), 30)

    docs = client.search_index()
    hits = search_docs(docs, query, limit=limit)
    if not hits:
        return [TextContent(
            type="text",
            text=f"No matches for {query!r} across {len(docs)} pages.",
        )]

    lines = [f"{len(hits)} hits across {len(docs)} pages for {query!r}:", ""]
    for h in hits:
        lines.append(f"- **{h.title}** — `{h.location}` (score {round(h.score, 2)})")
        if h.snippet:
            lines.append(f"  > {h.snippet}")
    lines.append("")
    lines.append("Follow up with `rain_get_page(path=...)` to read any hit in full.")
    return [TextContent(type="text", text="\n".join(lines))]


def _tool_get_page(client: RainDocsClient, args: dict[str, Any]) -> list[TextContent]:
    path = str(args.get("path", "")).strip()
    if not path:
        return [TextContent(type="text", text="error: path is required")]
    title, body = client.fetch_page(path)
    header = f"# {title}\n\n_path: `{path}`_\n\n"
    return [TextContent(type="text", text=header + body)]


def _tool_list_sections(client: RainDocsClient) -> list[TextContent]:
    pages = client.sitemap()
    counts = Counter(p.section for p in pages)
    lines = [f"{len(counts)} top-level sections across {len(pages)} pages:", ""]
    for section, n in counts.most_common():
        lines.append(f"- `{section}` — {n} page{'s' if n != 1 else ''}")
    return [TextContent(type="text", text="\n".join(lines))]


def _tool_list_pages(client: RainDocsClient, args: dict[str, Any]) -> list[TextContent]:
    section = str(args.get("section", "")).strip().strip("/")
    if not section:
        return [TextContent(type="text", text="error: section is required")]
    needle = str(args.get("filter") or "").strip().lower()
    limit = min(max(int(args.get("limit", 50)), 1), 500)

    pages = [p for p in client.sitemap() if p.section == section]
    if needle:
        pages = [
            p for p in pages
            if needle in p.title.lower() or needle in p.path.lower()
        ]

    if not pages:
        return [TextContent(
            type="text",
            text=f"No pages match section='{section}' filter='{needle}'.",
        )]

    pages_sorted = sorted(pages, key=lambda p: p.path)[:limit]
    header = (
        f"{len(pages_sorted)} pages in `{section}`"
        + (f" (filtered by {needle!r})" if needle else "")
        + (f" (showing first {limit})" if len(pages) > limit else "")
        + ":\n"
    )
    body = "\n".join(f"- **{p.title}** — `{p.path}`" for p in pages_sorted)
    return [TextContent(type="text", text=header + "\n" + body)]


# ---- Entry point ----------------------------------------------------------

async def _run() -> None:
    async with stdio_server() as (read, write):
        await app.run(
            read,
            write,
            InitializationOptions(
                server_name="rain-docs",
                server_version="0.1.0",
                capabilities=app.get_capabilities(
                    notification_options=NotificationOptions(),
                    experimental_capabilities={},
                ),
            ),
        )


def main() -> None:
    """Synchronous entry point used by `rain-mcp` script and `python -m`."""
    try:
        asyncio.run(_run())
    except KeyboardInterrupt:
        log.info("rain-mcp stopped")


if __name__ == "__main__":
    main()


# Re-export for test convenience.
__all__ = ["app", "main", "_tool_search", "_tool_get_page", "_tool_list_sections", "_tool_list_pages"]


# Silence a private-import complaint from Python if `json` is unused at import.
_ = json
