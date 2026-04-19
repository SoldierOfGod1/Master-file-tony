"""Simple keyword search over the MkDocs search index.

MkDocs ships a lunr-compatible index, but lunr.py is heavy. Instead we do a
light TF-scoring + fuzzy substring match over the plain-text `text` field of
each indexed document. Good enough for "find me the page about X" queries;
Claude can follow up with `rain_get_page` for the full contents.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Iterable

# A small stopword set keeps short-query scoring sane. Anything domain-specific
# (rain, api, tmf, axiom) is INTENTIONALLY not here — we want those to rank.
_STOPWORDS = frozenset(
    "the a an and or of in on at to for is are was were be been being this that these those "
    "how what why when where which who with by from as it its into than then".split()
)

_TOKEN_RE = re.compile(r"[A-Za-z0-9]+")


def _tokenise(text: str) -> list[str]:
    return [t.lower() for t in _TOKEN_RE.findall(text)]


def _filter_query_tokens(tokens: Iterable[str]) -> list[str]:
    return [t for t in tokens if t and t not in _STOPWORDS and len(t) > 1]


@dataclass(frozen=True)
class SearchHit:
    title: str
    location: str  # MkDocs-relative path (e.g. "architecture/api-design/")
    score: float
    snippet: str   # short excerpt around the best match

    def to_dict(self) -> dict[str, object]:
        return {
            "title": self.title,
            "location": self.location,
            "score": round(self.score, 3),
            "snippet": self.snippet,
        }


def search(docs: list[dict[str, str]], query: str, limit: int = 10) -> list[SearchHit]:
    """Return the top `limit` hits from `docs` (MkDocs search_index.json docs)
    for `query`. Scoring is a simple bag-of-words frequency count plus a
    title-match boost. Good enough for the MCP's 'find me the doc about X'
    use case; the caller follows up with fetch_page for the full read.
    """
    raw_tokens = _tokenise(query)
    q_tokens = _filter_query_tokens(raw_tokens)
    if not q_tokens:
        # Fall back to the raw tokens — someone might have typed a one-word
        # stopword-like query and we shouldn't return zero results.
        q_tokens = raw_tokens

    if not q_tokens:
        return []

    hits: list[SearchHit] = []
    for d in docs:
        title = d.get("title") or ""
        body = d.get("text") or ""
        loc = d.get("location") or ""
        if not (title or body):
            continue

        body_lower = body.lower()
        title_lower = title.lower()

        # Score: term frequency in body + big boost for each term in title.
        score = 0.0
        matched_any = False
        for t in q_tokens:
            body_hits = body_lower.count(t)
            if body_hits:
                matched_any = True
                score += 1.0 + (body_hits - 1) * 0.1
            if t in title_lower:
                matched_any = True
                score += 5.0
            if t in loc.lower():
                score += 1.5

        if not matched_any:
            continue

        # Exact-phrase bonus — rewards "customer 360 view" over just matching
        # three scattered words somewhere on the page.
        phrase = query.strip().lower()
        if phrase and (phrase in body_lower or phrase in title_lower):
            score += 3.0

        hits.append(SearchHit(
            title=title,
            location=loc,
            score=score,
            snippet=_build_snippet(body, q_tokens),
        ))

    hits.sort(key=lambda h: h.score, reverse=True)
    return hits[:limit]


def _build_snippet(body: str, query_tokens: list[str], window: int = 160) -> str:
    """Return ~one-line excerpt around the first occurrence of a query token."""
    if not body:
        return ""
    body_lower = body.lower()
    best = -1
    for t in query_tokens:
        idx = body_lower.find(t)
        if idx >= 0 and (best < 0 or idx < best):
            best = idx
    if best < 0:
        # No token found — return the opening of the body.
        return body[:window].strip() + ("…" if len(body) > window else "")

    start = max(0, best - window // 2)
    end = min(len(body), start + window)
    excerpt = body[start:end].strip()
    if start > 0:
        excerpt = "…" + excerpt
    if end < len(body):
        excerpt = excerpt + "…"
    # Squash whitespace / line breaks for single-line display.
    return re.sub(r"\s+", " ", excerpt)
