// The feed drawn as episodes. An episode keeps in view what the person put
// in, what came of it and what was handed over; the work that led there is
// one line on a phone, opened as a layer, and a column of its own on a desk.
// The further an episode stands from now, the tighter it lies: two back it is
// a band of one line, and the one before the last shows the head of its
// report. The one going on now ends with a card of what it does.

import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { plural } from "../../format.js";
import { Row } from "./rows.js";
import { SumBadges } from "./badges.js";
import { DELIVERIES, firstPara, isInput, lineCount, marksCalls, sumOf } from "./episodes.js";
import { KIND_NAMES, kindIcon, stampText } from "./labels.js";
import { COMMAND_TITLES } from "./command.js";
import { bare } from "./feed.js";
import { nearEnd } from "./feedwindow.js";

// useEpisodes keeps what the reader opened in the feed of one conversation:
// the bands unfolded, the reports read whole, the layer of work. It also
// remembers whether the host has ever said of a call whether it is still out:
// a call that no longer says so has come back.
export function useEpisodes(name, id, items) {
    const [opened, setOpened] = useState(() => new Set());
    const [whole, setWhole] = useState(() => new Set());
    const [work, setWork] = useState(null);
    const marks = useRef(false);
    useEffect(() => {
        setOpened(new Set());
        setWhole(new Set());
        setWork(null);
        marks.current = false;
    }, [name, id]);
    if (!marks.current && marksCalls(items)) marks.current = true;
    const flip = (set, key) => {
        const next = new Set(set);
        if (next.has(key)) next.delete(key);
        else next.add(key);
        return next;
    };
    return {
        opened,
        whole,
        work,
        marks: marks.current,
        unfold: useCallback((key) => setOpened((was) => flip(was, key)), []),
        readWhole: useCallback((key) => setWhole((was) => flip(was, key)), []),
        openWork: setWork,
    };
}

// tiersOf says how each episode is drawn: 0 in full, 1 in full with the head
// of its report, 2 as a band. A band the reader unfolded is drawn as 1.
// Messages waiting to be read count as an episode after the last: the feed
// settles when a message is sent, not a second later when it is echoed.
export function tiersOf(eps, opened, after = 0) {
    return eps.map((ep, n) => {
        const far = eps.length - 1 - n + after;
        if (far < 2) return far;
        return opened.has(ep.key) ? 1 : 2;
    });
}

// clock is the time of day of a moment, the way the stamps of the feed say it.
export function clock(at) {
    const t = typeof at === "number" ? at : Date.parse(at);
    if (!Number.isFinite(t)) return "";
    return new Date(t).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}

// spanWords says how long a stretch of work took, rounded the way a person says it.
export function spanWords(from, to) {
    if (!Number.isFinite(from) || !Number.isFinite(to)) return "";
    const sec = Math.max(0, (to - from) / 1000);
    if (sec < 60) return `${Math.round(sec)} s`;
    return `${Math.round(sec / 60)} min`;
}

// stopclock is a running time as a clock shows it: 0:11, 12:05, 1:02:40.
function stopclock(sec) {
    const s = Math.max(0, Math.floor(sec));
    const two = (n) => String(n).padStart(2, "0");
    if (s < 3600) return `${Math.floor(s / 60)}:${two(s % 60)}`;
    return `${Math.floor(s / 3600)}:${two(Math.floor((s % 3600) / 60))}:${two(s % 60)}`;
}

// plainLine is the first words of a text without its markup, for a line that
// only has to say which one it was.
export function plainLine(text, max = 140) {
    const line = String(text || "")
        .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
        .replace(/^#+\s*/gm, "")
        .replace(/[`*_>]/g, "")
        .replace(/\s+/g, " ")
        .trim();
    return line.length > max ? `${line.slice(0, max)}…` : line;
}

// afterOf names what a piece of work followed, for the head of its log.
export function afterOf(ep) {
    if (ep.how === "continued") return "the report";
    const head = [...ep.heads].reverse().find(isInput) || ep.heads[ep.heads.length - 1];
    if (!head) return "";
    if (head.role === "asked") return "your answer";
    if (head.role === "permitted") return "your permission";
    if (head.role === "me" || head.role === "shell") return plainLine(bare(head.text), 60) || "your message";
    return compactOf(head).text || compactOf(head).who;
}

// useTick draws its component anew every second while it is on: a time that
// runs is read off the clock, not off the last event.
function useTick(on) {
    const [, setBeat] = useState(0);
    useEffect(() => {
        if (!on) return undefined;
        const timer = setInterval(() => setBeat((n) => n + 1), 1000);
        return () => clearInterval(timer);
    }, [on]);
}

function Chevron({ up = false }) {
    return html`<span class=${`epchev${up ? " up" : ""}`} aria-hidden="true">${Icon.chevron()}</span>`;
}

// onKeyPress makes a row that opens something answer the keys a button does.
function onKeyPress(act) {
    return (event) => {
        if (event.key !== "Enter" && event.key !== " ") return;
        event.preventDefault();
        act();
    };
}

// ChangedFiles lists the files the replies of a piece of work named, each
// opening the file the way an attachment does.
export function ChangedFiles({ files, onFile }) {
    if (!files || !files.length) return null;
    return html`
        <div class="epfiles">
            <span class="epfileslabel">changed</span>
            ${files.map((file) => (file.outside
                ? html`<span class="epfile off" key=${file.path || file.name}
                              title="outside the conversation directory">
                        <span class="epfileico">${Icon.file()}</span>${file.name}</span>`
                : html`<button type="button" class="epfile" key=${file.path || file.name}
                               aria-label=${`open ${file.name}`}
                               onClick=${() => onFile && onFile(file)}>
                        <span class="epfileico">${Icon.file()}</span>${file.name}</button>`))}
        </div>
    `;
}

// An answer to a question is an input of the person, drawn as one: their
// bubble, with what was asked small above what they chose.
function Answered({ item }) {
    const rows = item.asked || [];
    return html`
        <div class="epanswered">
            ${rows.map((row, n) => html`
                <div class="eparow" key=${n}>
                    <span class="epaq">${row.header && html`<b>${row.header}</b> `}${row.text}</span>
                    <span class="epaa">${(row.answer || []).join(", ") || "skipped"}</span>
                </div>
            `)}
        </div>
        <div class="mstamp">${item.at ? `answered · ${stampText(item.at)}` : "answered"}</div>
    `;
}

// compactOf is a row of a head as one line of a band: who, and what.
function compactOf(row) {
    switch (row.role) {
    case "me": return { who: "you", text: plainLine(bare(row.text), 90) };
    case "shell": return { who: "you", text: `! ${row.text || ""}` };
    case "asked":
        return { who: row.status ? "question" : "answered",
                 text: (row.asked || []).map((r) => (r.answer || []).join(", ") || "skipped").join("; ") };
    case "permitted":
        return { who: "permission",
                 text: (row.rows || []).map((r) => `${r.tool} ${r.decision === "deny" ? "denied" : "allowed"}`).join("; ") };
    case "wake": return { who: "wake-up", text: plainLine(row.text, 90) };
    case "mail": return { who: `from ${row.from || "a letter"}`, text: plainLine(row.text, 90) };
    case "command": return { who: "command", text: COMMAND_TITLES[row.name] || row.name || "" };
    case "shots": {
        const n = (row.shots || []).length;
        return { who: "", text: `${n} ${plural(n, "picture", "pictures")}` };
    }
    case "shellout": return { who: "output", text: plainLine(row.text || row.err, 90) };
    case "taskdone": return { who: "done", text: plainLine(row.summary, 90) };
    default: return { who: "", text: plainLine(row.text, 90) };
    }
}

function YouLine({ row }) {
    const { who, text } = compactOf(row);
    return html`
        <div class=${`epyou${row.role === "me" || row.role === "shell" || row.role === "asked" ? " mine" : ""}`}>
            ${who && html`<span class="epyouwho">${who}</span>`}
            <q>${text}</q>
            ${row.at && html`<time class="eptime">${clock(row.at)}</time>`}
        </div>
    `;
}

// Heads draws what an episode began with, as the feed draws each of them.
function Heads({ ep, ctx }) {
    return html`
        ${ep.how === "continued" && html`<div class="epgoes">went on without you</div>`}
        ${ep.heads.map((row) => (row.role === "asked" && !row.status
            ? html`<${Answered} key=${`${row.role}-${row.pos}`} item=${row} />`
            : html`<${Row} key=${`${row.role}-${row.pos}`} ...${ctx.rowProps} item=${row} />`))}
    `;
}

// Band is an episode far from now, pressed into its input, one line of its
// course and the first line of what came of it. A tap unfolds it.
function Band({ ep, ctx }) {
    const first = ep.outcome[0];
    const gives = ep.shown.filter((row) => DELIVERIES.has(row.role));
    const steps = ep.log.filter((entry) => entry.row).length;
    const unfold = () => ctx.unfold(ep.key);
    return html`
        <section class="epband" role="button" tabindex="0" aria-expanded="false"
                 aria-label="unfold this exchange" onClick=${unfold} onKeyDown=${onKeyPress(unfold)}>
            ${ep.how === "continued" && html`<div class="epgoes">went on without you</div>`}
            ${ep.heads.map((row) => html`<${YouLine} key=${`${row.role}-${row.pos}`} row=${row} />`)}
            <div class="epbandline">
                ${ep.from != null && html`<span class="epbandwhen">${`${clock(ep.from)}–${clock(ep.to)}`}</span>`}
                ${steps > 0 && html`<span class="epbandsteps">${`${steps} ${plural(steps, "step", "steps")}`}</span>`}
                <${SumBadges} sum=${ep.work} onCalls=${() => ctx.onRuns(ep.work.runs)} cls="mini" />
                <${Chevron} />
            </div>
            ${first && html`<p class="epbandout">${plainLine(first.text, 300)}</p>`}
            ${gives.length > 0 && html`
                <div class="epgives">
                    ${gives.map((row) => html`
                        <span class="epgive" key=${`${row.role}-${row.pos}`}><span class="epgiveico">${Icon.file()}</span>${giveName(row)}</span>
                    `)}
                </div>
            `}
        </section>
    `;
}

function giveName(row) {
    if (row.role === "sent") return `sent ${(row.files || []).map((f) => f.name).join(", ")}`;
    if (row.role === "brief") return `brief ${row.title || ""}`.trim();
    return row.title || row.role;
}

// Stub is where the work of an episode sits on a phone: how long it took, its
// badges summed, how much was thought and said on the way. A tap opens it.
function Stub({ ep, ctx }) {
    const sum = sumOf(ep.settled);
    const more = [
        sum.minds > 0 && `${sum.minds} ${plural(sum.minds, "thought", "thoughts")}`,
        sum.said > 0 && `${sum.said} said`,
        sum.other > 0 && `${sum.other} more`,
    ].filter(Boolean).join(", ");
    const open = () => ctx.openWork(ep.key);
    return html`
        <div class="epstub" role="button" tabindex="0" aria-label="open the work of this exchange"
             onClick=${open} onKeyDown=${onKeyPress(open)}>
            <span class="epstubwho">${stubWho(sum)}</span>
            <${SumBadges} sum=${sum} onCalls=${() => ctx.onRuns(sum.runs)} />
            ${more && html`<span class="epstubmore">${more}</span>`}
            <${Chevron} />
        </div>
    `;
}

function stubWho(sum) {
    if (!sum.calls && !sum.minds && !sum.said && !sum.other) return "thought before going on";
    const took = spanWords(sum.from, sum.to);
    return took && took !== "0 s" ? `${took} of work` : "work";
}

// A report cut to its head leaves out more than its first paragraph when the
// paragraph itself is long: the box keeps four lines of it.
const CUT_CHARS = 280;

// Outcome is what came of an episode: its replies after the last run — on the
// episode before the last only the head of the first — the end of its turn,
// what it handed over and the files its replies named.
function Outcome({ ep, cut, ctx }) {
    const replies = ep.outcome;
    const head = replies.length ? firstPara(replies[0].text) : "";
    const cuts = cut && replies.length > 0
        && (head !== replies[0].text || replies.length > 1 || head.length > CUT_CHARS);
    const drawn = cuts ? [{ ...replies[0], text: head }] : replies;
    const lines = replies.reduce((n, row) => n + lineCount(row.text), 0);
    return html`
        ${drawn.length > 0 && html`
            <div class=${`epout${cuts ? " cut" : ""}`}>
                ${drawn.map((row) => html`<${Row} key=${`ai-${row.pos}`} ...${ctx.rowProps} item=${{ ...row, files: [] }} />`)}
                ${cuts && html`
                    <button type="button" class="epreadall" onClick=${() => ctx.readWhole(ep.key)}>
                        ${`read the whole report, ${lines} ${plural(lines, "line", "lines")}`}
                    </button>
                `}
            </div>
        `}
        ${ep.turn && html`<${Row} ...${ctx.rowProps} item=${ep.turn} />`}
        ${ep.shown.map((row) => html`<${Row} key=${`${row.role}-${row.pos}`} ...${ctx.rowProps} item=${row} />`)}
        ${!ep.open && html`<${ChangedFiles} files=${ep.files} onFile=${ctx.rowProps.onFile} />`}
    `;
}

// LiveLine says what an open episode does this second: the call out and for
// how long, or that it is thinking and for how long nothing has come.
function LiveLine({ now }) {
    useTick(true);
    const took = Number.isFinite(now.since) ? stopclock((Date.now() - now.since) / 1000) : "";
    if (now.running) {
        return html`
            <div class="wlogrun">
                <span class="epdot"></span>
                <span class=${`epkind k-${now.kind}`}>${kindIcon(now.kind)}</span>
                <b>${now.call.name}</b>
                <code>${plainLine(now.call.arg || "", 120)}</code>
                <span class="wlogel">${took}</span>
            </div>
        `;
    }
    return html`
        <div class="wlogrun">
            <span class="epdot"></span>
            <b>thinking</b>
            <span class="wlogel">${took}</span>
        </div>
    `;
}

// Now is the step going on at the bottom of an open episode: the call, how
// long it has been out, what the session said last and its latest thought.
// On a phone a tap opens the work of the episode.
function Now({ ep, wide, ctx }) {
    const now = ep.now;
    useTick(true);
    if (!now) return null;
    const { call, kind, running, step, said, thought } = now;
    const took = Number.isFinite(now.since) ? stopclock((Date.now() - now.since) / 1000) : "";
    const stepSum = step ? sumOf([step]) : null;
    const open = () => ctx.openWork(ep.key);
    // What the session said stands in the card with its links and its code:
    // a tap on one of them is theirs, not the card's.
    const tap = (event) => {
        if (event.target.closest && event.target.closest("a, button, .path, .mdbar")) return;
        open();
    };
    const press = wide ? {} : { role: "button", tabindex: "0", onClick: tap, onKeyDown: onKeyPress(open),
                                "aria-label": "what the session does now — open its work" };
    return html`
        <section class=${`epnow${running ? " running" : ""}`} aria-live="polite" ...${press}>
            <div class="epnowcall">
                <div class="epnowtop">
                    <span class="epdot"></span>
                    <span class="epnowword">now</span>
                    ${running
                        ? html`
                            <span class=${`epkind k-${kind}`}>${kindIcon(kind)}</span>
                            <b class="epnowname">${call.name}</b>
                            <span class="epnowkindname">${KIND_NAMES[kind] || ""}</span>`
                        : html`<b class="epnowname">thinking</b>`}
                    ${took && html`<span class="epnowel"><span>${running ? "running" : "for"}</span> ${took}</span>`}
                </div>
                ${call && html`
                    <div class="epnowrow">
                        <div class="epnowargbox">
                            ${!running && html`<span class="epnowlast">last call <b>${call.name}</b></span>`}
                            <code class="epnowarg">${call.arg || "no arguments"}</code>
                        </div>
                        ${!wide && html`<${SumBadges} sum=${stepSum} onCalls=${() => ctx.onRuns(stepSum.runs)} cls="small" />`}
                    </div>
                `}
                ${wide && stepSum && html`
                    <div class="epnowstep">
                        <span class="epnowlabel">this step</span>
                        <${SumBadges} sum=${stepSum} onCalls=${() => ctx.onRuns(stepSum.runs)} />
                        ${stepSum.from != null && html`<span class="epnowsince">${`since ${clock(stepSum.from)}`}</span>`}
                    </div>
                `}
            </div>
            ${(said || thought) && html`
                <div class="epnowtext">
                    ${said && html`<div class="epnowsaid"><${Row} ...${ctx.rowProps} item=${{ ...said, files: [] }} /></div>`}
                    ${thought && html`
                        <div class="epnowmind">
                            <span class="epnowlabel">${`latest thought, ${clock(thought.at)}`}</span>
                            <${Row} ...${ctx.rowProps} item=${thought} />
                        </div>
                    `}
                </div>
            `}
        </section>
    `;
}

// Episode draws one episode in full.
function Episode({ ep, tier, far, wide, ctx }) {
    return html`
        <section class=${`ep t${tier}${ep.open ? " live" : ""}`}>
            ${far && html`
                <button type="button" class="epfold" onClick=${() => ctx.unfold(ep.key)}>
                    fold back<${Chevron} up />
                </button>
            `}
            <${Heads} ep=${ep} ctx=${ctx} />
            ${!wide && ep.settled.length > 0 && html`<${Stub} ep=${ep} ctx=${ctx} />`}
            <${Outcome} ep=${ep} cut=${tier === 1 && !ctx.whole.has(ep.key)} ctx=${ctx} />
            ${ep.open && html`<${Now} ep=${ep} wide=${wide} ctx=${ctx} />`}
        </section>
    `;
}

// Episodes draws the feed: bands far back, episodes in full near now.
export function Episodes({ eps, tiers, wide, ctx }) {
    return eps.map((ep, n) => (tiers[n] === 2
        ? html`<${Band} key=${ep.key} ep=${ep} ctx=${ctx} />`
        : html`<${Episode} key=${ep.key} ep=${ep} tier=${tiers[n]} far=${eps.length - 1 - n >= 2}
                          wide=${wide} ctx=${ctx} />`));
}

function entryAt(entry) {
    const first = entry.texts[0];
    if (first && first.at) return first.at;
    const sum = sumOf([entry]);
    return sum.from;
}

// WorkLog is the work of an episode in order: when, what was said or thought
// on the way, whole, the badges of the run it led to with the lines hanging
// under them, the files the run changed. The step going on now says so.
export function WorkLog({ ep, ctx }) {
    const now = ep.now;
    return html`
        <ol class="wlog">
            ${ep.log.map((entry) => html`
                <li class="wlogentry" key=${entry.key}>
                    <time class="eptime">${clock(entryAt(entry))}</time>
                    <div class="wlogbody">
                        ${entry.texts.map((row) => html`
                            <${Row} key=${`${row.role}-${row.pos}`} ...${ctx.rowProps}
                                    item=${row.role === "ai" ? { ...row, files: [] } : row} />
                        `)}
                        ${entry.row && html`<${Row} ...${ctx.rowProps} item=${entry.row}
                                                  onCalls=${() => ctx.onRuns([entry.row.run])} />`}
                        <${ChangedFiles} files=${entry.files} onFile=${ctx.rowProps.onFile} />
                        ${now && now.step === entry && html`<${LiveLine} now=${now} />`}
                    </div>
                </li>
            `)}
            ${now && !now.step && html`<li class="wlogentry"><span></span><div class="wlogbody"><${LiveLine} now=${now} /></div></li>`}
        </ol>
    `;
}

// WorkLayer is the log of one episode over the feed on a phone, with a way to
// the work before it and after it.
export function WorkLayer({ eps, at, ctx }) {
    const ep = eps.find((one) => one.key === at);
    if (!ep) return html`<p class="hint">This work is no longer in the loaded part of the conversation.</p>`;
    const withWork = eps.filter((one) => one.log.length > 0 || one.open);
    const here = withWork.indexOf(ep);
    const before = withWork[here - 1];
    const next = withWork[here + 1];
    const sum = ep.work;
    const took = spanWords(sum.from, sum.to);
    const after = afterOf(ep);
    return html`
        <div class="wlayer" onClick=${ctx.onFeedClick}>
            <div class="wlhead">
                <div class="wlhtop">
                    <h2>Work</h2>
                    ${sum.from != null && html`<span class="wlhspan">${`${clock(sum.from)}–${clock(sum.to)}${took ? `, ${took}` : ""}`}</span>`}
                    <span class="wlhnav">
                        <button type="button" class="wlhprev" aria-label="earlier work" disabled=${!before}
                                onClick=${() => before && ctx.openWork(before.key)}>${Icon.chevron()}</button>
                        <button type="button" aria-label="later work" disabled=${!next}
                                onClick=${() => next && ctx.openWork(next.key)}>${Icon.chevron()}</button>
                    </span>
                </div>
                ${after && html`<p class="wlhafter">after <q>${after}</q></p>`}
                <${SumBadges} sum=${sum} onCalls=${() => ctx.onRuns(sum.runs)} />
            </div>
            ${ep.log.length > 0 || ep.now
                ? html`<${WorkLog} ep=${ep} ctx=${ctx} />`
                : html`<p class="hint">Nothing was done here but the answer.</p>`}
        </div>
    `;
}

// Process stands beside the feed on a desk: the work of every episode drawn
// in full, the one going on now marked. It keeps to its end while it is
// there, the way the feed does.
export function Process({ eps, tiers, ctx }) {
    const box = useRef(null);
    const body = useRef(null);
    const stick = useRef(true);
    const near = eps.filter((ep, n) => tiers[n] < 2 && (ep.log.length > 0 || ep.open));
    // The column grows without a render of its own — a font arrives, a thought
    // unfolds — so its end is kept by watching its content, not its renders.
    useEffect(() => {
        const el = box.current;
        if (!el || !body.current || typeof ResizeObserver !== "function") return undefined;
        const keep = () => {
            if (stick.current) el.scrollTop = el.scrollHeight;
        };
        keep();
        const eye = new ResizeObserver(keep);
        eye.observe(body.current);
        return () => eye.disconnect();
    }, []);
    const onScroll = (event) => {
        stick.current = nearEnd(event.currentTarget);
    };
    return html`
        <aside class="epside" ref=${box} onScroll=${onScroll} onClick=${ctx.onFeedClick} aria-label="process">
            <div class="epsidebody" ref=${body}>
                <div class="epsidehead">
                    <span class="epsidetitle">Process</span>
                    <span class="epsidesub">thoughts, calls and files of the work</span>
                </div>
                ${near.map((ep) => {
                    const after = afterOf(ep);
                    return html`
                        <section class=${`epsideep${ep.open ? " live" : ""}`} key=${ep.key}>
                            <div class="epsideephead">
                                ${ep.work.from != null && html`<span class="epsidewhen">${`${clock(ep.work.from)}–${clock(ep.work.to)}`}</span>`}
                                ${after && html`<span class="epsideafter">after <q>${after}</q></span>`}
                                <${SumBadges} sum=${ep.work} onCalls=${() => ctx.onRuns(ep.work.runs)} cls="small" />
                            </div>
                            <${WorkLog} ep=${ep} ctx=${ctx} />
                        </section>
                    `;
                })}
                ${near.length === 0 && html`<p class="epsidenone">No calls or thoughts in the exchanges on screen.</p>`}
            </div>
        </aside>
    `;
}
