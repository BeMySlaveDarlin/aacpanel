#!/usr/bin/env bash
set -uo pipefail

CONFIG="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
SNAP="${AACP_RATE_SNAPSHOT:-$CONFIG/rate-limits.json}"
MODELS="${AACP_SESSION_MODELS:-$CONFIG/session-models}"
NEXT="${AACP_STATUSLINE_NEXT-}"
THROTTLE="${AACP_RATE_THROTTLE:-20}"

# Files of sessions whose model has not changed for this many days are swept:
# a live one writes itself back on its next tick, a finished one is gone.
SWEEP_DAYS=7

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

# The model and the effort of the session as the status line sees them. The
# transcript learns of a change only with the next request; this file, at once.
# It is rewritten only when the model or the effort differs from what it holds,
# so its time is the time of the last change, and a reader can weigh it against
# the last request of the transcript.
session_model() {
    command -v jq >/dev/null 2>&1 || return 0

    local fields sid id name effort window
    mapfile -t fields < <(jq -r '
        .session_id // "",
        .model.id // "",
        .model.display_name // "",
        ((.effort | if type == "object" then .level else . end) // "" | tostring),
        (.context_window.context_window_size // "" | tostring)' <<<"$payload" 2>/dev/null)
    [ "${#fields[@]}" -eq 5 ] || return 0
    sid=${fields[0]}
    id=${fields[1]}
    name=${fields[2]}
    effort=${fields[3]}
    window=${fields[4]}
    [[ "$sid" =~ ^[0-9a-fA-F-]{36}$ ]] || return 0
    [ -n "$id" ] || return 0

    local file="$MODELS/$sid.json" held
    held=$(jq -r '[.model.id // "", .model.displayName // "", .effort // ""] | join("|")' \
        "$file" 2>/dev/null)
    [ "$id|$name|$effort" != "$held" ] || return 0

    mkdir -p "$MODELS" 2>/dev/null || return 0
    local tmp="$file.tmp.$$"
    if jq -n -c --arg at "$(date +%s)" --arg sid "$sid" --arg id "$id" --arg name "$name" \
        --arg effort "$effort" --arg window "$window" '{
            at: ($at | tonumber),
            sessionId: $sid,
            model: {id: $id, displayName: $name},
            effort: (if $effort == "" then null else $effort end),
            contextWindow: (if $window == "" then null else ($window | tonumber) end)
        }' > "$tmp" 2>/dev/null; then
        mv -f "$tmp" "$file" 2>/dev/null || rm -f "$tmp"
    else
        rm -f "$tmp"
    fi
    find "$MODELS" -maxdepth 1 -type f -name '*.json' -mtime "+$SWEEP_DAYS" -delete 2>/dev/null
}

snapshot || true
session_model || true

if [ -n "$NEXT" ]; then
    printf '%s' "$payload" | sh -c "$NEXT" || true
fi
