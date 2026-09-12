// Labels and counters shared by the feed, the call list and the sheets.

import { Icon } from "../../ui/icons.js";

export const KIND_NAMES = {
    bash: "commands",
    files: "files",
    web: "web",
    agents: "agents",
    skill: "skills",
    ask: "questions",
    artifact: "artifacts",
    time: "schedule",
    browser: "browser",
    mcp: "mcp",
    other: "other",
};

export function kindIcon(kind) {
    const pick = {
        bash: Icon.terminal,
        files: Icon.file,
        web: Icon.globe,
        agents: Icon.robot,
        skill: Icon.skill,
        ask: Icon.ask,
        artifact: Icon.artifact,
        time: Icon.clock,
        browser: Icon.globe,
        mcp: Icon.plug,
    }[kind];
    return (pick || Icon.tools)();
}

// callWord returns the form of the word that matches the number.
export function callWord(n) {
    return n === 1 ? "call" : "calls";
}

export function countCalls(n) {
    return `${n} ${callWord(n)}`;
}

// tokenWord returns the form of the word that matches the number.
export function tokenWord(n) {
    return n === 1 ? "token" : "tokens";
}

// shortTokens renders a token count short enough for a badge.
export function shortTokens(n) {
    if (n >= 10000) return `${Math.round(n / 1000)}k`;
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
    return String(n);
}

// stampText renders when a message was written.
export function stampText(iso) {
    const ms = Date.parse(iso);
    if (!Number.isFinite(ms)) return "";
    const at = new Date(ms);
    const day = at.toLocaleString("ru-RU", { day: "numeric", month: "short" }).replace(".", "");
    const time = at.toLocaleString("ru-RU", { hour: "2-digit", minute: "2-digit" });
    return `${day} · ${time}`;
}
