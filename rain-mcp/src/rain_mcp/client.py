"""HTTP client that talks to software-engineering.docs.rain.co.za.

The docs site is an MkDocs build, so three things matter:
  * /sitemap.xml lists every page URL.
  * /search/search_index.json holds the prebuilt full-text search index.
  * Page HTML lives at the same URL as the sitemap entry.

This module caches each of those artefacts in-process for `TTL_SECONDS` so
repeated MCP tool calls don't hammer the origin. The cache is per-process so
it goes away when the server exits — that's fine for a short-lived MCP.
"""

from __future__ import annotations

import json
import logging
import re
import time
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from typing import Optional
from urllib.parse import urljoin, urlparse

import httpx
from bs4 import BeautifulSoup

BASE_URL = "https://software-engineering.docs.rain.co.za/"
USER_AGENT = "rain-mcp/0.1 (+https://github.com/rain/rain-mcp)"
TTL_SECONDS = 60 * 30  # 30 minutes

log = logging.getLogger("rain_mcp.client")


@dataclass(frozen=True)
class Page:
    """A single indexed page from the docs site."""

    title: str
    path: str  # relative path, e.g. "architecture/api-design/"
    url: str   # absolute URL
    section: str  # top-level folder, e.g. "architecture"

    def to_dict(self) -> dict[str, str]:
        return {
            "title": self.title,
            "path": self.path,
            "url": self.url,
            "section": self.section,
        }


@dataclass
class _CacheEntry:
    value: object
    expires_at: float


class RainDocsClient:
    """Synchronous client for rain's docs site. Safe to use from MCP tools."""

    def __init__(self, base_url: str = BASE_URL, timeout: float = 15.0) -> None:
        self.base_url = base_url.rstrip("/") + "/"
        self._http = httpx.Client(
            timeout=timeout,
            headers={"User-Agent": USER_AGENT, "Accept": "text/html,application/xhtml+xml"},
            follow_redirects=True,
        )
        self._cache: dict[str, _CacheEntry] = {}

    # ---- cache plumbing ---------------------------------------------------

    def _cache_get(self, key: str) -> Optional[object]:
        entry = self._cache.get(key)
        if entry is None or entry.expires_at < time.time():
            return None
        return entry.value

    def _cache_set(self, key: str, value: object, ttl: float = TTL_SECONDS) -> None:
        self._cache[key] = _CacheEntry(value=value, expires_at=time.time() + ttl)

    # ---- sitemap ----------------------------------------------------------

    def sitemap(self) -> list[Page]:
        """Return every page in the sitemap. Cached."""
        cached = self._cache_get("sitemap")
        if cached is not None:
            return cached  # type: ignore[return-value]

        resp = self._http.get(urljoin(self.base_url, "sitemap.xml"))
        resp.raise_for_status()

        # MkDocs' sitemap is a standard `urlset`. Strip XML namespace for
        # simpler parsing — the structure is always <url><loc>...</loc></url>.
        text = re.sub(r'\s+xmlns="[^"]+"', "", resp.text, count=1)
        root = ET.fromstring(text)

        pages: list[Page] = []
        for url_el in root.findall("url"):
            loc_el = url_el.find("loc")
            if loc_el is None or not loc_el.text:
                continue
            url = loc_el.text.strip()
            path = urlparse(url).path.lstrip("/")
            section = path.split("/", 1)[0] if "/" in path else path.rstrip("/")
            # Title defaults to the last path segment until we've fetched the
            # page. The search index later fills in the real titles.
            nice = path.strip("/").split("/")[-1] or "index"
            title = nice.replace("-", " ").replace("_", " ").title() or "Home"
            pages.append(Page(title=title, path=path, url=url, section=section or "root"))

        self._cache_set("sitemap", pages)
        log.info("sitemap loaded: %d pages", len(pages))
        return pages

    # ---- search index -----------------------------------------------------

    def search_index(self) -> list[dict[str, str]]:
        """Return the raw MkDocs search index. Each entry has `title`,
        `location` and `text`. Cached."""
        cached = self._cache_get("search_index")
        if cached is not None:
            return cached  # type: ignore[return-value]

        resp = self._http.get(urljoin(self.base_url, "search/search_index.json"))
        resp.raise_for_status()
        data = resp.json()
        docs = data.get("docs") or []
        # Normalise so downstream code can rely on keys existing.
        out: list[dict[str, str]] = []
        for d in docs:
            out.append({
                "title": (d.get("title") or "").strip() or "(untitled)",
                "location": d.get("location") or "",
                "text": d.get("text") or "",
            })
        self._cache_set("search_index", out)
        log.info("search index loaded: %d docs", len(out))
        return out

    # ---- page fetch -------------------------------------------------------

    def fetch_page(self, path: str) -> tuple[str, str]:
        """Fetch a single page and return (title, markdown-ish text).

        We don't try to round-trip to perfect markdown — we strip the MkDocs
        chrome (nav, header, footer) and return the article body with its
        headings preserved. That's enough for Claude to answer questions.
        """
        path = path.strip("/")
        url = urljoin(self.base_url, path + ("/" if not path.endswith("/") else ""))

        cache_key = f"page:{url}"
        cached = self._cache_get(cache_key)
        if cached is not None:
            return cached  # type: ignore[return-value]

        resp = self._http.get(url)
        resp.raise_for_status()

        soup = BeautifulSoup(resp.text, "lxml")

        # MkDocs Material wraps article content in <article class="md-content__inner">
        article = soup.select_one("article.md-content__inner") or soup.select_one("article") or soup.body
        if article is None:
            raise ValueError(f"no article body found at {url}")

        title_el = soup.select_one("h1") or article.select_one("h1")
        title = (title_el.get_text(" ", strip=True) if title_el else path) or path

        # Drop non-content chrome: nav, "edit this page" button, "back to top",
        # search box, comments etc.
        for bad in article.select(
            "nav, .md-source-file, .md-feedback, .md-content__button, .md-nav, "
            ".md-sidebar, form[data-md-component='search'], a.md-content__button"
        ):
            bad.decompose()

        text = _html_to_markdown(article)
        result = (title, text)
        self._cache_set(cache_key, result)
        return result

    def close(self) -> None:
        self._http.close()


def _html_to_markdown(article) -> str:  # type: ignore[no-untyped-def]
    """Very small HTML→markdown converter sufficient for doc answers.

    Preserves headings, paragraphs, lists, links, and inline code. Tables
    become pipe-format. Anything we don't recognise falls through as text.
    Keeping this in-tree means no dependency on `markdownify` or similar.
    """
    out: list[str] = []

    for node in article.descendants:
        if getattr(node, "name", None) is None:
            continue

    def render(el) -> None:  # type: ignore[no-untyped-def]
        name = el.name
        if name in ("script", "style"):
            return
        if name and re.fullmatch(r"h[1-6]", name):
            level = int(name[1])
            out.append(("#" * level) + " " + el.get_text(" ", strip=True))
            out.append("")
        elif name == "p":
            text = el.get_text(" ", strip=True)
            if text:
                out.append(text)
                out.append("")
        elif name in ("ul", "ol"):
            for i, li in enumerate(el.find_all("li", recursive=False), start=1):
                marker = "-" if name == "ul" else f"{i}."
                out.append(f"{marker} {li.get_text(' ', strip=True)}")
            out.append("")
        elif name == "pre":
            out.append("```")
            out.append(el.get_text("\n", strip=True))
            out.append("```")
            out.append("")
        elif name == "blockquote":
            for line in el.get_text("\n", strip=True).splitlines():
                out.append(f"> {line}")
            out.append("")
        elif name == "table":
            headers = [th.get_text(" ", strip=True) for th in el.select("thead th")]
            if headers:
                out.append("| " + " | ".join(headers) + " |")
                out.append("|" + "|".join(["---"] * len(headers)) + "|")
            for row in el.select("tbody tr"):
                cells = [td.get_text(" ", strip=True) for td in row.find_all("td")]
                if cells:
                    out.append("| " + " | ".join(cells) + " |")
            out.append("")

    # Walk only direct children recursively; we want section-level rendering.
    for child in article.children:
        if getattr(child, "name", None):
            render(child)
            # Walk sub-sections inside divs that wrap content.
            if child.name == "div":
                for sub in child.find_all(re.compile(r"^(h[1-6]|p|ul|ol|pre|blockquote|table)$"), recursive=True):
                    render(sub)

    md = "\n".join(out).strip()
    # Collapse runs of blank lines.
    md = re.sub(r"\n{3,}", "\n\n", md)
    return md
