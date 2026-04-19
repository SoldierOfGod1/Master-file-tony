# rain-mcp

An MCP server that exposes **rain's internal software-engineering docs**
(<https://software-engineering.docs.rain.co.za/>) to Claude Code — or any
other MCP client.

## What it gives you

Four tools:

| Tool | Purpose |
|---|---|
| `rain_search(query, limit=10)` | Ranked full-text search across all ~1,020 pages. Returns titles, paths and short snippets. |
| `rain_get_page(path)` | Fetch a single page by its relative path (e.g. `architecture/api-design/`), return markdown. |
| `rain_list_sections()` | List top-level sections (`architecture`, `backend`, `frontend`, `ai`, etc.) with page counts. |
| `rain_list_pages_in_section(section, filter?, limit?)` | Enumerate pages inside a section, optionally filtered by a title/path substring. |

## How it works

The docs site is a public MkDocs build. On first use rain-mcp fetches two
artefacts:

- `/sitemap.xml` — 1,020+ URLs.
- `/search/search_index.json` — the prebuilt MkDocs full-text search index.

Both are cached in-process for 30 minutes. Individual pages are fetched
on demand, HTML-stripped down to the article body, and returned as markdown.

No auth is required — the docs site itself is public (the internal tools
it *links to*, like Rancher and GitLab, are behind AzureAD, but rain-mcp
never touches those).

## Install

Requires Python 3.10+. From this directory:

```bash
# Using uv (recommended — fastest):
uvx --from . rain-mcp

# Or with pip:
pip install -e .
rain-mcp
```

Tested with the `mcp` SDK ≥ 1.0.

## Wire it up to Claude Code

The project's `.mcp.json` already includes a `rain-docs` entry — just flip
`"enabled": true`:

```json
"rain-docs": {
  "command": "uvx",
  "args": ["--from", "./rain-mcp", "rain-mcp"],
  "enabled": true
}
```

## Environment variables

| Var | Default | Purpose |
|---|---|---|
| `RAIN_DOCS_BASE_URL` | `https://software-engineering.docs.rain.co.za/` | Override to point at a local mirror or staging site. |
| `RAIN_MCP_LOG` | `INFO` | Python log level (`DEBUG` for wire traces). |

## Smoke test

The repo ships a tiny script that exercises every tool against the live
site — useful for confirming network reachability and cache behaviour:

```bash
python -m tests.smoke
```

It searches for `"api design"`, fetches the top hit, lists sections, and
lists the first 10 pages under `architecture`.

## License

MIT.
