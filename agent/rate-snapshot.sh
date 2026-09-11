#!/usr/bin/env bash
set -uo pipefail

SNAP="${AACP_RATE_SNAPSHOT:-${CLAUDE_CONFIG_DIR:-$HOME/.claude}/rate-limits.json}"
NEXT="${AACP_STATUSLINE_NEXT-}"
THROTTLE="${AACP_RATE_THROTTLE:-20}"

payload=$(cat)

snapshot() {
    command -v jq >/dev/null 2>&1 || return 0

    local five_pct five_reset seven_pct seven_reset now was
    five_pct=$(jq -r '.rate_limits.five_hour.used_percentage // empty' <<<"$payload" 2>/dev/null) || return 0
    [ -n "$five_pct" ] || return 0

    now=$(date +%s)
    was=$(stat -c %Y "$SNAP" 2>/dev/null || echo 0)
    (( now - was >= THROTTLE )) || return 0

    five_reset=$(jq -r '.rate_limits.five_hour.resets_at // "null"' <<<"$payload" 2>/dev/null)
    seven_pct=$(jq -r '.rate_limits.seven_day.used_percentage // "null"' <<<"$payload" 2>/dev/null)
    seven_reset=$(jq -r '.rate_limits.seven_day.resets_at // "null"' <<<"$payload" 2>/dev/null)

    local tmp="$SNAP.tmp.$$"
    if printf '{"at":%s,"fiveHour":{"pct":%s,"resetsAt":%s},"sevenDay":{"pct":%s,"resetsAt":%s}}\n' \
        "$now" "$five_pct" "${five_reset:-null}" "${seven_pct:-null}" "${seven_reset:-null}" \
        > "$tmp" 2>/dev/null; then
        mv -f "$tmp" "$SNAP" 2>/dev/null || rm -f "$tmp"
    else
        rm -f "$tmp"
    fi
}

snapshot || true

if [ -n "$NEXT" ]; then
    printf '%s' "$payload" | sh -c "$NEXT" || true
fi
