#!/usr/bin/env python3
"""Validate local Markdown/HTML links without network access or extra packages."""
from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
MARKDOWN = [
    ROOT / "README.md",
    ROOT / "AGENTS.md",
    ROOT / "android" / "README.md",
    *sorted((ROOT / "docs").rglob("*.md")),
    *sorted((ROOT / "examples").rglob("*.md")),
]
LINKS = re.compile(r"!?\[[^\]]*\]\(\s*(<[^>]+>|[^)\s]+)(?:\s+['\"][^)\"]+['\"])?\s*\)")
HTML_IMAGES = re.compile(r"<(?:img|a)\b[^>]*(?:src|href)=['\"]([^'\"]+)['\"]", re.IGNORECASE)
CODE_BLOCKS = re.compile(r"(?ms)^ {0,3}(?:\x60{3,}|~{3,}).*?^ {0,3}(?:\x60{3,}|~{3,})[^\n]*(?:\n|$)")
SKIP_SCHEMES = ("https:", "http:", "mailto:", "tel:", "data:", "javascript:")


def local_target(source: Path, url: str) -> Path | None:
    url = url.strip("<>")
    if not url or url.startswith(("#", "//")) or url.lower().startswith(SKIP_SCHEMES):
        return None
    path = unquote(urlsplit(url).path)
    if not path:
        return None
    target = (source.parent / path).resolve()
    try:
        target.relative_to(ROOT)
    except ValueError as exc:
        raise ValueError("link leaves repository") from exc
    return target


def main() -> int:
    failures = []
    count = 0
    for source in MARKDOWN:
        if not source.is_file():
            failures.append(f"{source.relative_to(ROOT)}: source document not found")
            continue
        text = CODE_BLOCKS.sub("", source.read_text(encoding="utf-8"))
        for match in [*LINKS.finditer(text), *HTML_IMAGES.finditer(text)]:
            url = match.group(1)
            line = text.count("\n", 0, match.start()) + 1
            try:
                target = local_target(source, url)
            except ValueError:
                failures.append(f"{source.relative_to(ROOT)}:{line}: invalid link {url!r}")
                continue
            if target is None:
                continue
            count += 1
            if not target.exists():
                failures.append(f"{source.relative_to(ROOT)}:{line}: missing target {url!r}")
    for failure in failures:
        print(failure, file=sys.stderr)
    if failures:
        print(f"Documentation links: {len(failures)} errors ({count} local links checked)", file=sys.stderr)
        return 1
    print(f"Documentation links: OK ({len(MARKDOWN)} files, {count} local links checked)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
