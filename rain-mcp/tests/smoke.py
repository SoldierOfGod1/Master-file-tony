"""End-to-end smoke test.

Hits the real docs site. Skipped in environments without network. Exits 0 on
success so CI can chain on it.
"""

from __future__ import annotations

import sys
import textwrap

from rain_mcp.client import RainDocsClient
from rain_mcp.search import search as search_docs


def main() -> int:
    client = RainDocsClient()

    print("-> loading sitemap...")
    pages = client.sitemap()
    print(f"   {len(pages)} pages")
    assert len(pages) > 100, "sitemap suspiciously small"

    print("-> loading search index...")
    docs = client.search_index()
    print(f"   {len(docs)} indexed docs")
    assert len(docs) > 50, "search index suspiciously small"

    print("-> searching 'api design'...")
    hits = search_docs(docs, "api design", limit=5)
    print(f"   {len(hits)} hits")
    for h in hits:
        snippet = textwrap.shorten(h.snippet, width=100, placeholder="...")
        print(f"     * [{h.score:>4.1f}] {h.title}  ({h.location})")
        print(f"       {snippet}")
    assert hits, "search returned zero hits for a common query"

    # Fetch the top hit.
    top = hits[0]
    path = top.location or "index.html"
    print(f"-> fetching top hit: {path}")
    title, body = client.fetch_page(path)
    print(f"   title={title!r}  body_chars={len(body)}")
    assert len(body) > 100, "page body too short; HTML parser probably failed"

    print("-> listing sections...")
    from collections import Counter
    sections = Counter(p.section for p in pages)
    for sec, n in sections.most_common(5):
        print(f"   {sec}: {n} pages")

    print("-> OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
