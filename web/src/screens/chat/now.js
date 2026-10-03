// What the session does now, above the composer: running a call or thinking
// between calls, how long it has stood so, the call it is on and the calls of
// its run so far.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { callWord, KIND_NAMES, kindIcon, shortTokens, tokenWord } from "./labels.js";
import { aims, inForeground, ToBackground } from "./tobg.js";

// What the person put into the turn. The turn going on began at the last one
// of them the session read: a message still in the queue has not begun anything.
const INPUTS = new Set(["me", "asked", "permitted", "shell", "wake", "command"]);

const ms = (iso) => (iso ? Date.parse(iso) : NaN);

const later = (a, b) => (a.pos - b.pos) || ((a.index || 0) - (b.index || 0));

// nowOf reads the feed for the turn going on: its last run of calls, the
// latest call of that run and whether the host says it is still out, the
// badges of the run, the calls of the run the turn waits on in the foreground,
// and since when it has stood as it stands. A turn with no call yet has only
// its start.
export function nowOf(items) {
    const list = items || [];
    let start = -1;
    for (let i = list.length - 1; i >= 0; i--) {
        const item = list[i];
        if (item.role === "turn") {
            start = i;
            break;
        }
        if (INPUTS.has(item.role) && item.state !== "queued" && item.state !== "withdrawn") {
            start = i;
            break;
        }
    }
    const turn = list.slice(start + 1);
    let run = null;
    for (let i = turn.length - 1; i >= 0 && run == null; i--) {
        if (turn[i].role === "tools" || turn[i].role === "think") run = turn[i].run;
    }
    let last = start >= 0 ? ms(list[start].at) : NaN;
    for (const item of turn) {
        for (const at of [item.at, ...((item.calls || []).map((c) => c.at))]) {
            const t = ms(at);
            if (Number.isFinite(t) && !(t <= last)) last = t;
        }
    }
    if (run == null) {
        return { call: null, kind: "", running: false, since: last, think: null, kinds: [], run: null, fore: [] };
    }

    let call = null;
    let kind = "other";
    let think = null;
    const kinds = [];
    const fore = [];
    for (const item of turn) {
        if (item.run !== run) continue;
        if (item.role === "think") {
            think = { count: (think ? think.count : 0) + (item.count || 0),
                      tokens: (think ? think.tokens : 0) + (item.tokens || 0) };
            continue;
        }
        if (item.role !== "tools") continue;
        const calls = item.calls || [];
        const was = kinds.find((k) => k.kind === item.kind);
        const failed = calls.filter((c) => c.failed).length;
        if (was) {
            was.count += calls.length;
            was.failed += failed;
        } else {
            kinds.push({ kind: item.kind, count: calls.length, failed });
        }
        for (const one of calls) {
            if (inForeground(one)) fore.push(one);
            if (!call || later(one, call) > 0) {
                call = one;
                kind = item.kind;
            }
        }
    }
    const running = Boolean(call) && call.open === true;
    return { call, kind, running, since: running ? ms(call.at) : last, think, kinds, run, fore };
}

// clock says how long something has stood, the way a stopwatch does.
export function clock(sec) {
    const s = Math.floor(Math.max(0, sec));
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const pad = (n) => String(n).padStart(2, "0");
    return h > 0 ? `${h}:${pad(m)}:${pad(s % 60)}` : `${m}:${pad(s % 60)}`;
}

// useTick redraws its component every second, asking nothing: a clock.
export function useTick() {
    const [, setNow] = useState(0);
    useEffect(() => {
        const timer = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(timer);
    }, []);
}

// NowBar is the bar above the composer while a session is at work, one line
// of it: the state — thinking between calls, running while a call is out —
// and how long it has stood so, then the call, the one going out or the last
// one back, and the badges of the run at the far end. The call opens itself
// among the calls of the run; the badges open the list of them. With to — the
// session, when the panel can reach it — a call the turn waits on goes to the
// background from a line under it: the one going out by itself on the stream,
// and every one of them at once where there are several, or in the console,
// where one key moves them all.
export function NowBar({ now, onCalls, onCall, to }) {
    useTick();
    const { call, kind, running, think, kinds } = now;
    const fore = (to && now.fore) || [];
    const one = aims(to) && running && inForeground(call) ? call : null;
    const all = fore.length > (aims(to) ? 1 : 0);
    const took = Number.isFinite(now.since) ? clock((Date.now() - now.since) / 1000) : "";
    const badges = (think && think.count > 0) || kinds.length > 0;
    return html`
        <div class=${`nowbar${running ? " running" : ""}`} aria-live="polite">
            <div class="nowtop">
                <span class="nowdot"></span>
                <b class="nowstate">${running ? "running" : "thinking"}</b>
                ${took && html`<span class="nowel">${took}</span>`}
                ${call && html`
                    <span class="nowsep" aria-hidden="true">·</span>
                    <button class="nowcall" type="button" onClick=${() => onCall && onCall(call)}
                            aria-label=${`open the call ${call.name}`}>
                        <span class=${`nowkind k-${kind}`}>${kindIcon(kind)}</span>
                        <span class="nowcalltext"><b class="nowname">${call.name}</b> <code class="nowarg">${call.arg || ""}</code></span>
                    </button>
                `}
                ${badges && html`
                    <span class="nowchips">
                        ${think && think.count > 0 && html`
                            <button class="mtools mthink" type="button" onClick=${onCalls}
                                    title=${`thinking: ${think.count}${think.tokens ? ` · ${shortTokens(think.tokens)} ${tokenWord(think.tokens)}` : ""}`}
                                    aria-label=${`thinking blocks: ${think.count}`}>
                                <span class="mticon">${Icon.thinking()}</span>
                                <span class="mtnum">${think.count}</span>
                            </button>
                        `}
                        ${kinds.map((k) => {
                            const label = KIND_NAMES[k.kind] || KIND_NAMES.other;
                            const broke = k.failed > 0 ? ` · ${k.failed} failed` : "";
                            return html`
                                <button class=${`mtools k-${k.kind}${k.failed > 0 ? " mtfail" : ""}`} type="button"
                                        key=${k.kind} onClick=${onCalls} title=${`${label}${broke}`}
                                        aria-label=${`${label}: ${k.count} ${callWord(k.count)}${broke}`}>
                                    <span class="mticon">${kindIcon(k.kind)}</span>
                                    <span class="mtnum">${k.count}</span>
                                </button>
                            `;
                        })}
                    </span>
                `}
            </div>
            ${(one || all) && html`
                <div class="nowbg">
                    ${one && html`<${ToBackground} to=${to} call=${one} key=${`one-${one.use}`} />`}
                    ${all && html`<${ToBackground} to=${to} count=${fore.length} key="all" />`}
                </div>
            `}
        </div>
    `;
}

// WaitBar is the bar while a session waits on the work it sent off: its own
// turn is over, and what holds it busy is that work, at it since the first of
// it began. It reads as the bar of a turn does — what holds the session and
// the clock beside it, on one line — and a tap opens its list.
export function WaitBar({ waits, onOpen }) {
    useTick();
    const took = Number.isFinite(waits.since) ? clock((Date.now() - waits.since) / 1000) : "";
    return html`
        <button class="nowbar nowwait" type="button" onClick=${() => onOpen && onOpen({ kind: waits.kind })}>
            <span class="nowtop">
                <span class="nowdot"></span>
                <span class="nowkind">${waits.kind === "agents" ? Icon.robot() : Icon.flow()}</span>
                <b class="nowstate">${waits.word}</b>
                ${took && html`<span class="nowel">${took}</span>`}
            </span>
        </button>
    `;
}
