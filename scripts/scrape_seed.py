#!/usr/bin/env python3
"""One-off scraper: pulls the entry table from https://vandyke.thor.edu/ into JSON.

Usage: scrape_seed.py <input.html> <output.json>
Produces [{"word":..., "meaning":..., "created_at":"YYYY-MM-DDTHH:MM:SSZ"}, ...]
preserving order, duplicates and whitespace-trimmed field values.
"""
import html
import json
import re
import sys
from datetime import datetime

ROW_RE = re.compile(
    r"<tr>\s*<td>(.*?)</td>\s*<td>(.*?)</td>\s*<td>(.*?)</td>\s*</tr>",
    re.DOTALL,
)


def main() -> int:
    src, dst = sys.argv[1], sys.argv[2]
    with open(src, encoding="utf-8") as fh:
        page = fh.read()

    rows = []
    for word, meaning, raw_date in ROW_RE.findall(page):
        word = html.unescape(word).strip()
        meaning = html.unescape(meaning).strip()
        raw_date = html.unescape(raw_date).strip()
        if not word and not meaning:
            continue
        # Original format: 2017-10-02 00:00:00.000000 (UTC, no tz marker)
        parsed = datetime.strptime(raw_date, "%Y-%m-%d %H:%M:%S.%f")
        rows.append(
            {
                "word": word,
                "meaning": meaning,
                "created_at": parsed.strftime("%Y-%m-%dT%H:%M:%SZ"),
            }
        )

    with open(dst, "w", encoding="utf-8") as fh:
        json.dump(rows, fh, ensure_ascii=False, indent=1)
        fh.write("\n")

    dups = len(rows) - len({(r["word"], r["meaning"], r["created_at"]) for r in rows})
    print(f"wrote {len(rows)} entries to {dst} ({dups} duplicate word/meaning/date rows)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
