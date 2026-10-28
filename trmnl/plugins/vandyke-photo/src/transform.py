#!/usr/bin/env python3
"""Newest vanDyke entry with a photo, for TRMNL."""

import json
import sys
import urllib.request
from datetime import datetime


def _fields(input):
    try:
        return input["trmnl"]["plugin_settings"]["custom_fields_values"]
    except (KeyError, TypeError):
        return {}


def _base(input):
    url = (_fields(input).get("url") or "").strip().rstrip("/")
    if not url:
        return None, "Set the url custom field to your vanDyke address."
    if not url.startswith(("http://", "https://")):
        return None, "The url custom field must start with http:// or https://."
    return url, None


def _get(url):
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    with urllib.request.urlopen(request, timeout=10) as response:
        return json.loads(response.read().decode("utf-8"))


def _date(value):
    if not value:
        return ""
    try:
        return datetime.fromisoformat(str(value).replace("Z", "+00:00")).strftime("%d %b %Y")
    except (TypeError, ValueError):
        return str(value)


def _photo(base, value):
    if not value:
        return ""
    if str(value).startswith(("http://", "https://")):
        return str(value)
    return base + "/" + str(value).lstrip("/")


def run(input):
    base, error = _base(input)
    if error:
        return {"error": error}
    try:
        page = _get(base + "/api/v1/entries?per_page=25&sort=created_at&order=desc")
    except Exception:
        return {"error": "Could not reach the vanDyke API. Check the url custom field."}

    items = page.get("items") or []
    for item in items:
        if not isinstance(item, dict):
            continue
        photo = _photo(base, item.get("photo_url"))
        if not photo:
            continue
        return {
            "has_photo": True,
            "photo_url": photo,
            "word": item.get("word") or "",
            "meaning": item.get("meaning") or "",
            "date": _date(item.get("created_at")),
            "total": page.get("total") or len(items),
        }
    return {
        "has_photo": False,
        "total": page.get("total") or len(items),
        "message": "No photo entries yet.",
    }


if __name__ == "__main__":
    raw = sys.stdin.read()
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except json.JSONDecodeError:
        payload = {}
    print(json.dumps(run(payload)))
