// The connection: the chip in the header, and the sheet behind it with every
// router leg and its ping from where the panel is open.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { LABELS, measure } from "../router.js";
import { Icon } from "./icons.js";
import { Sheet } from "./sheet.js";

// Under AGING_SEC the snapshot is simply fresh and its age is not worth a word;
// from STALE_SEC it is old enough to doubt, from DEAD_SEC the agent is gone.
const AGING_SEC = 10;
const STALE_SEC = 60;
const DEAD_SEC = 300;

function routeWords(route) {
    const here = route && route.here ? LABELS[route.here] || route.here : "";
    const via = route && route.via ? LABELS[route.via] || route.via : "";
    if (!here) return { value: "—", brief: "the address map is unavailable" };
    const page = `page ${here}`;
    if (via) return { value: via, brief: `${page} · requests go through ${via}` };
    const why = {
        alone: `${page} · there is no closer address`,
        here: `${page} · requests from here too, nobody closer answered`,
        silent: `${page} · not one address answers`,
        "no-token": `${page} · the closer address answered, but no bearer was issued for it`,
        "no-map": "the address map is unavailable",
    }[route.why];
    return { value: here, brief: why || `${page} · requests from here too` };
}

function clock(date) {
    return date.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}

function ageWords(sec) {
    if (sec < 60) return `${Math.round(sec)} s`;
    if (sec < 3600) return `${Math.round(sec / 60)} min`;
    return `${Math.round(sec / 3600)} h`;
}

function ageTone(sec) {
    if (sec >= DEAD_SEC) return "crit";
    if (sec >= STALE_SEC) return "warn";
    return "";
}

// connection folds all the header knows about the link into one chip. A
// healthy link is named by its leg, with the age of the snapshot only once it
// is worth reading; a broken one is named by what broke, since which leg is
// silent matters less than that nothing answers. phrase is the same state as a
// whole sentence, for the sheet and for a screen reader.
export function connection({ conn, ageSec, route }) {
    const kind = conn ? conn.kind : "loading";
    const at = conn && conn.at ? clock(conn.at) : "";
    if (kind === "unauthorized") {
        return { tone: "crit", dot: "", words: "Signed out", phrase: "The session has ended — sign in again", signIn: true };
    }
    if (kind === "offline") {
        return at
            ? { tone: "warn", dot: "off", words: `No connection · ${at}`, phrase: `No connection — data from ${at}` }
            : { tone: "warn", dot: "off", words: "No connection", phrase: "No connection and no saved snapshot" };
    }
    if (kind === "stale") {
        return { tone: "warn", dot: "off", words: at ? `Snapshot · ${at}` : "Snapshot", phrase: `No connection — snapshot from ${at || "earlier"}` };
    }
    if (kind === "loading") return { tone: "", dot: "off", words: "Loading…", phrase: "Loading…" };
    if (ageSec === null || ageSec === undefined) {
        return { tone: "crit", dot: "on", words: "No agent snapshot", phrase: "The panel answers, but the agent snapshot is unavailable" };
    }
    const leg = route && route.here ? routeChip(route).text : "live";
    if (ageSec < AGING_SEC) return { tone: "", dot: "on", leg, age: "", words: leg, phrase: "Live — the agent snapshot is fresh" };
    const age = ageWords(ageSec);
    const tone = ageTone(ageSec);
    return {
        tone, dot: "on", leg, age, words: `${leg} · ${age}`,
        phrase: tone ? `The agent snapshot is ${age} old` : `Live — the agent snapshot is ${age} old`,
    };
}

function legWhere(row) {
    const url = row.url.replace(/^https?:\/\//, "");
    return row.kind === "local" ? `this device · ${url}` : url;
}

export function RouteTable({ route, round = 0 }) {
    const [rows, setRows] = useState(null);
    const here = (route && route.here) || "";
    const via = (route && route.via) || "";

    useEffect(() => {
        let alive = true;
        setRows(null);
        measure().then((got) => alive && setRows(got.rows));
        return () => { alive = false; };
    }, [round]);

    if (rows === null) return html`<p class="tbrief">measuring…</p>`;
    if (rows.length === 0) return html`<p class="tbrief">the address map is unavailable</p>`;
    return html`
        <div class="routetable">
            ${rows.map((row) => html`
                <div class=${`routerow ${row.ok ? "ok" : "bad"}${row.kind === here ? " here" : ""}${row.kind === via ? " via" : ""}`} key=${row.kind}>
                    ${Icon.monitor()}
                    <div class="routelab">
                        <span class="routekind">
                            ${LABELS[row.kind] || row.kind}
                            <span class="legtag">${row.ok ? `${row.ms} ms` : "no answer"}</span>
                        </span>
                        <span class="routeurl" title=${row.url}>${legWhere(row)}</span>
                    </div>
                    <span class="routehere">${row.kind === via ? "requests" : row.kind === here ? "page" : ""}</span>
                </div>
            `)}
        </div>
    `;
}

// routeChip returns the name of the leg the requests take and its tooltip.
export function routeChip(route) {
    const words = routeWords(route);
    const via = route && route.via ? LABELS[route.via] || route.via : "";
    return { text: via || words.value, tip: words.brief, near: Boolean(via) };
}

// RouteSheet is the connection behind the chip: the state in a sentence, the
// legs and how each answers, and a way to try again. Retry asks for the data
// and for the nearest leg anew, and pings every leg once more.
export function RouteSheet({ open, onClose, route, conn, ageSec, insecure = false, onRetry }) {
    const words = routeWords(route);
    const state = connection({ conn, ageSec, route });
    const [round, setRound] = useState(0);
    const retry = () => {
        setRound((n) => n + 1);
        if (route && route.onRecheck) route.onRecheck();
        if (onRetry) onRetry();
    };
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="connection">
            <div class="shead">
                <div class="grow">
                    <div class="stitle">Connection</div>
                    <div class="connsay" data-tone=${state.tone || undefined}>${state.phrase}</div>
                    <div class="ssub">${words.brief}</div>
                </div>
                <button class="btn connretry" type="button" onClick=${retry}>${Icon.refresh()}Retry</button>
            </div>
            ${insecure && html`
                <p class="warnline">Cookie without Secure: AACP_SECURE=0 in .env while the panel is reached over https, so the session cookie travels over plain http as well</p>
            `}
            <${RouteTable} route=${route} round=${round} />
        <//>
    `;
}
