#!/usr/bin/env python3
"""Model catalog of a contour: which model stands behind an alias and what window it has."""

import json
import os
import time
import urllib.error
import urllib.request

import contours
import paths

CACHE_PATH = os.environ.get("AACP_MODELS_CACHE", paths.state("models.json"))

ENDPOINT = os.environ.get("AACP_MODELS_URL", "https://api.anthropic.com/v1/models?limit=100")
OAUTH_BETA = "oauth-2025-04-20"
API_VERSION = "2023-06-01"

REFRESH = float(os.environ.get("AACP_MODELS_REFRESH", "86400"))
TIMEOUT = float(os.environ.get("AACP_MODELS_TIMEOUT", "10"))

MAX_BYTES = 1 << 20


def _token(config_dir):
    path = os.path.join(config_dir, ".credentials.json")
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    oauth = data.get("claudeAiOauth")
    if not isinstance(oauth, dict):
        return None
    token = oauth.get("accessToken")
    return token if isinstance(token, str) and token else None


def _rows(payload):
    data = payload.get("data") if isinstance(payload, dict) else None
    if not isinstance(data, list):
        return None
    out = []
    for item in data:
        if not isinstance(item, dict):
            continue
        mid = item.get("id")
        if not isinstance(mid, str) or not mid:
            continue
        row = {"id": mid, "name": item.get("display_name") or mid}
        for key, field in (("max_input_tokens", "window"), ("max_tokens", "output")):
            value = item.get(key)
            if isinstance(value, int) and value > 0:
                row[field] = value
        out.append(row)
    return out


def _fetch(token):
    request = urllib.request.Request(ENDPOINT, headers={
        "Authorization": f"Bearer {token}",
        "anthropic-beta": OAUTH_BETA,
        "anthropic-version": API_VERSION,
    })
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
            raw = response.read(MAX_BYTES)
    except urllib.error.HTTPError as e:
        return None, f"http-{e.code}"
    except TimeoutError:
        return None, "timeout"
    except (urllib.error.URLError, OSError):
        return None, "network"
    try:
        rows = _rows(json.loads(raw.decode("utf-8")))
    except (ValueError, UnicodeDecodeError):
        return None, "bad-json"
    if rows is None:
        return None, "bad-json"
    return rows, ""


def load():
    """Returns the cache from disk, empty when it is missing or broken."""
    if not CACHE_PATH:
        return {}
    try:
        with open(CACHE_PATH, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


def save(cache):
    """Writes the cache to disk through a temporary file."""
    os.makedirs(os.path.dirname(CACHE_PATH), exist_ok=True)
    tmp = CACHE_PATH + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(cache, f, ensure_ascii=False)
    os.replace(tmp, CACHE_PATH)


def catalog(now=None):
    """Returns the catalog of every contour, as it goes into the snapshot."""
    if not CACHE_PATH:
        return None
    now = time.time() if now is None else now
    cache = load()
    saved = cache.get("contours")
    saved = saved if isinstance(saved, dict) else {}
    changed = False
    out = []

    for name, config_dir in contours.profiles():
        was = saved.get(name) if isinstance(saved.get(name), dict) else {}
        models = was.get("models") if isinstance(was.get("models"), list) else None
        at = was.get("at") if isinstance(was.get("at"), (int, float)) else 0

        token = _token(config_dir)
        if token is None:
            out.append(_row(name, models, at, "no-credentials"))
            continue

        reason = was.get("error") or ""
        if models is None or now - at >= REFRESH:
            fresh, reason = _fetch(token)
            if fresh is not None:
                models, at = fresh, int(now)
            entry = {"at": int(at), "models": models or []}
            if reason:
                entry["error"] = reason
            saved[name] = entry
            changed = True

        out.append(_row(name, models, at, reason))

    if changed:
        cache["contours"] = saved
        try:
            save(cache)
        except OSError:
            pass

    return {"at": int(now), "contours": out}


def _row(name, models, at, reason):
    row = {"profile": name}
    if models:
        row["state"] = "ok"
        row["at"] = int(at)
        row["models"] = models
    elif reason and reason != "no-credentials":
        row["state"] = "error"
    else:
        row["state"] = "unknown"
    if reason:
        row["error"] = reason
    return row


_windows = {}
_windows_key = None


def windows():
    """Returns the windows of every known model as id to token count."""
    global _windows, _windows_key
    key = None
    if CACHE_PATH:
        try:
            stat = os.stat(CACHE_PATH)
            key = (stat.st_mtime_ns, stat.st_size)
        except OSError:
            key = None
    if key != _windows_key:
        found = {}
        saved = load().get("contours")
        if isinstance(saved, dict):
            for row in saved.values():
                if not isinstance(row, dict):
                    continue
                for model in row.get("models") or []:
                    if isinstance(model, dict) and isinstance(model.get("window"), int):
                        found[model["id"]] = model["window"]
        _windows, _windows_key = found, key
    return _windows


def window_for(model):
    """Returns the window of one model, or None when the catalog does not know it."""
    if not model:
        return None
    known = windows()
    if model in known:
        return known[model]
    base = model.split("[", 1)[0]
    return known.get(base)
