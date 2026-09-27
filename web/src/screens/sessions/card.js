// Times of a conversation as the session cards show them.

// stamp returns a transcript timestamp in seconds of the epoch.
export function stamp(iso) {
    if (!iso) return 0;
    const ms = Date.parse(iso);
    return Number.isFinite(ms) ? Math.round(ms / 1000) : 0;
}

// when renders a timestamp from the history.
export function when(sec) {
    if (!sec) return "—";
    return new Date(sec * 1000).toLocaleString("ru-RU", {
        day: "numeric", month: "short", hour: "2-digit", minute: "2-digit",
    });
}
