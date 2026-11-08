// The card of a session, one skeleton of five lines for every state and for
// both the phone and the desk, an archived conversation included: a card
// that changed its height with its state would move the list under the eye.
//
//   1  the glyph of the state, the name, and its account and resource
//      readings at the right;
//   2  the state in a word or the subject of the wait, what runs now right
//      after it, and the collector's note;
//   3  the step of the checklist in hand and n/N, over a bar a segment a step;
//   4  who runs it on which model, what queued and compacted, and how long
//      the state has lasted — or when it ends;
//   5  its settings, each in a place of its own whether on or off, and how
//      full its context is; the context bar runs along the bottom edge.
//
// A live card carries no controls, and its name takes the line: on a phone a
// long press on the card opens them as its pie (cardpie.js), in the reach of
// the thumb, and at a desk a right click opens them as its menu
// (desktop/sessionmenu.js), which holds more than a row of icons could.

import { useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useHold } from "../../ui/pie.js";
import { Sheet } from "../../ui/sheet.js";
import { alarmTime, bytes, fill, pct, tokens } from "../../format.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { CODEX_NOTE, agentKey, agentName, codexRestartWhy, codexResume, isCodex, shownName } from "../../agent.js";
import { checklistOf } from "../chat/checklist.js";
import { modelTitle } from "../chat/head.js";
import { effortName } from "../chat/picker.js";
import { useCardPie } from "./cardpie.js";
import { busyOf } from "./controls.js";
import { placeOf } from "./kin.js";
import { stamp } from "./card.js";
import { PauseSheet, continueProject, pauseWhy } from "./pause.js";
import { runsOf, shortText, stateOf, timeOf } from "./state.js";

// The glyph of each kind of state; a kind with none draws a shape of its own
// (a dot at work, a ring for the background, a hollow ring for a closed one).
const GLYPHS = {
    ask: Icon.ask, question: Icon.ask, held: Icon.ask, permission: Icon.lock, request: Icon.hand, brief: Icon.plan,
    secret: Icon.key, call: Icon.alerts, input: Icon.hourglass, compact: Icon.compact, saving: Icon.refresh,
    restarting: Icon.refresh, limit: Icon.limit, paused: Icon.pause, done: Icon.check,
};

// The short words of the modes, each fitting the place the mode keeps.
const MODES = {
    default: "Manual", acceptEdits: "Edits", plan: "Plan", auto: "Auto", bypassPermissions: "Bypass", dontAsk: "No ask",
};

// Glyph is the mark of a state, drawn beside its word.
export function Glyph({ state }) {
    const icon = GLYPHS[state.kind];
    return html`
        <span class="scglyph" data-tone=${state.tone} data-kind=${state.kind} data-unseen=${state.unseen ? "" : undefined} aria-hidden="true">
            ${icon ? icon() : html`<i class="scshape"></i>`}
        </span>
    `;
}

// nameOf is the part of a card's name that is its own: a card under the
// heading of its project, or under the session that opened it, drops what it
// shares with the name that line says already (known); a card with nothing
// over it reads by its whole name.
export function nameOf(name, known = "") {
    const at = known && name.length > known.length && name.startsWith(known)
        && /[-_.]/.test(name[known.length]) ? known.length + 1 : 0;
    return at ? name.slice(at) : name;
}

// Name is the name of a card, its own part on the screen and whole for a
// reader, with the address of the session in its tip where the name read is
// another — a thread of codex reads by its title; tag is a quiet word after
// it — who opened a session that stands away from its parent, the contour of
// a closed one on a shelf of several.
function Name({ full, own, tag = "", address = full }) {
    return html`
        <span class="scname" title=${address !== full ? address : undefined} aria-label=${full}>
            <span class="scown" aria-hidden="true">${own}</span>
            ${tag && html`<span class="sctag">${tag}</span>`}
        </span>
    `;
}

function Resources({ res, account }) {
    if (!res && !account) return null;
    const mem = res && bytes(res.mem).replace(" ", "").replace(/([KMGT])B$/, "$1");
    const cpu = res?.cpu === undefined ? "—" : pct(res.cpu);
    // The reading leaves out the cache of files the session read and wrote:
    // the kernel drops it first, and counted in it would look like the
    // session's own weight.
    const memTip = res?.cache ? `Session memory · ${bytes(res.cache)} of file cache besides` : "Session memory";
    return html`
        <span class="scres">
            ${account && html`<span class="scmeter scaccount" title="Subscription account" aria-label=${`Subscription account: ${account}`}>${Icon.badge()}${account}</span>`}
            ${res && html`
                <span class="scmeter" title=${memTip} aria-label=${`${memTip}: ${mem}`}>${Icon.memory()}${mem}</span>
                <span class="scmeter" title="Session CPU · one core" aria-label=${`Session CPU: ${cpu}`}>${Icon.cpu()}${cpu}</span>
            `}
        </span>
    `;
}

// Line is the state's line: its glyph, the word in the colour of the state, the subject
// of a wait in the ink, a command in mono; then what runs now, only what
// runs, the wake the session has ahead, and the note of the collector. A
// close or restart on its way (busy) stands in place of the state: a spinner
// is its glyph, one mark on the line, as on the card of a session being raised.
function Line({ state, runs, alarm = null, notes, busy, time = null, tone = "" }) {
    return html`
        <div class="scl2">
            <span class="scsay">
                ${busy ? html`
                    <span class="scglyph" data-tone="busy" data-kind="ghost" aria-hidden="true"><span class="spin"></span></span>
                    <span class="scbusy" role="status">${busy}</span>
                ` : html`
                    <${Glyph} state=${state} />
                    <span class="scword">${state.word}</span>
                    ${state.subject && html` <b class="scsubj">${state.subject}</b>`}
                    ${state.mono && html` <code class="scmono">${state.mono}</code>`}
                    ${state.note && html`<span class="scaside"> · ${state.note}</span>`}
                `}
            </span>
            ${runs && runs.agents > 0 && html`<span class="scrun" title=${`${runs.agents} subagents running`}>${Icon.robot()}<b>${runs.agents}</b></span>`}
            ${runs && runs.commands > 0 && html`<span class="scrun" title=${`${runs.commands} background commands running`}>${Icon.terminal()}<b>${runs.commands}</b></span>`}
            ${runs && runs.watchers > 0 && html`<span class="scrun" title=${`${runs.watchers} observers watching`}>${Icon.eye()}<b>${runs.watchers}</b></span>`}
            ${alarm && html`<span class="scalarm" title=${`${alarm.name} · ${alarmTime(alarm.next, true)}`}>${Icon.clock()}${alarmTime(alarm.next)}</span>`}
            ${notes && notes.length > 0 && html`<span class="sccollector" title=${notes.join("\n")}>${notes.join(" · ")}</span>`}
            ${time && html`<${When} time=${time} tone=${tone} />`}
        </div>
    `;
}

// wakeOf is the alarm a session has ahead that is not the end of its pause:
// the end of a pause stands in the corner of a paused card.
function wakeOf(s) {
    const next = s.nextAlarm;
    return next && next.next && next.id !== (s.pauseAlarm && s.pauseAlarm.id) ? next : null;
}

// Steps is the checklist: the step in hand, wrapped rather than cut, and
// n/N; under it a segment a step over the whole width — done in grey, the
// one at work in the colour of the state, those ahead dark. A list with
// every step behind it is done, and its bar is whole. A session with no
// checklist draws neither line; an archived conversation with none says on
// the line of the step what it was about, with no bar under it.
function Steps({ step = "", n, total, segs, over }) {
    if (!total) return step ? html`<div class="scl3"><span class="scstep">${step}</span></div>` : null;
    return html`
        <div class="scl3"><span class="scstep" title=${step}>${step}</span><span class=${`scn${over ? " scover" : ""}`}><b>${n}</b>/${total}</span></div>
        <div class=${`scsegs${over ? " scover" : ""}`} aria-hidden="true">${segs.map((cls, i) => html`<i key=${i} class=${cls}></i>`)}</div>
    `;
}

// stepsOf reads a checklist for the card: the step in hand — the last one
// done for a list that is over — and a class a segment.
export function stepsOf(checklist) {
    const p = checklistOf(checklist);
    if (!p) return { total: 0 };
    const working = !p.over && p.items[p.current].status === "active";
    return {
        total: p.total,
        n: p.n,
        over: p.over,
        step: p.over ? (p.items.filter((item) => item.status === "done").pop() || p.items[p.items.length - 1]).text : p.step,
        segs: p.items.map((item, i) => (item.status === "done" || item.status === "dropped" ? "scd"
            : working && i === p.current ? "sca" : "")),
    };
}

// pastSteps reads the last checklist an archived conversation kept, as its
// archive entry carries it: the step it stood at and how far it got.
function pastSteps(last, about) {
    const total = (last && last.total) || 0;
    if (!total) return { total: 0, step: about };
    const done = Math.min(total, last.done || 0);
    return {
        total, over: done >= total, n: Math.min(total, done + (done < total ? 1 : 0)), step: last.step || about || "",
        segs: Array.from({ length: total }, (_, i) => (i < done ? "scd" : "")),
    };
}

// Agent is who runs the session, in the hue of its agent, on which model at
// which effort; then what waits in its queue and how often it was compacted,
// in words — for a closed thread of codex, what it spent; an archived
// conversation keeps the time of its state at the right, a live one says it
// on the line of its state.
function Agent({ who, model, effort, queued = 0, compacts = 0, spent = "", time, tone }) {
    return html`
        <div class="scl4">
            <span class="scagent"><span class="agentword" data-agent=${agentKey(who)} data-tip=${isCodex(who) ? CODEX_NOTE : undefined}>${agentName(who)}</span>${model && html` ${model}`}${effort && html` <span class="sceffort">${effort}</span>`}</span>
            ${spent && html`<span class="scmore">${spent}</span>`}
            ${queued > 0 && html`<span class="scmore">${queued} queued</span>`}
            ${compacts > 0 && html`<span class="scmore">${compacts} ${compacts === 1 ? "compaction" : "compactions"}</span>`}
            ${time && html`<${When} time=${time} tone=${tone} />`}
        </div>
    `;
}

// When is how long the state has lasted, or an arrow and the moment it ends.
function When({ time, tone }) {
    return html`<span class="sctime" data-tone=${tone}>${time.until && html`<i class="scarrow">→</i>`}${time.text}</span>`;
}

function modelOf(id) {
    return id ? modelTitle(id, { withWindow: false }) : "";
}

// Badges are the settings of a session in fixed places, an off one dimmed in
// its place: the mode, where it lives, its cap of the context and whether it
// restarts at it, Remote Control and a worktree; at the right how full the
// context is. A card that kept no settings says nothing of its cap.
function Badges({ mode, place, cap, auto, remote, worktree, ctx, heavyPlace = 0, known = true }) {
    const capped = cap != null && cap > 0 && cap <= 100;
    return html`
        <div class="scl5">
            <span class=${`scb${mode ? "" : " scoff"}`}>${mode ? MODES[mode] || mode.charAt(0).toUpperCase() + mode.slice(1) : "—"}</span>
            <span class=${`scb${place ? "" : " scoff"}`}>${place || "—"}</span>
            ${capped && auto
                ? html`<span class="scb scon" title=${`At ${cap}% of the context it saves its work and restarts`}>${Icon.refresh()}at ${cap}%</span>`
                : known ? html`<span class="scb">${capped ? `cap ${cap}%` : "no cap"}</span>`
                : html`<span class="scb scoff">—</span>`}
            <span class=${`scb${remote ? " scon" : " scoff"}`} title=${remote ? "Remote Control is on: the session is open on claude.ai too" : undefined}>RC</span>
            <span class=${`scb${worktree ? " scon" : " scoff"}`} title=${worktree ? `A worktree of ${worktree}` : undefined}>worktree</span>
            ${heavyPlace > 0 && html`<span class="scb" title="Heavy run waiting for admission">queued ${heavyPlace}</span>`}
            <span class="scctx">${ctx}</span>
        </div>
    `;
}

// Edge is the context bar along the bottom of the card, of the whole window,
// with the cap marked on it: in the accent where the session restarts there.
// What lies past the cap is the share the session never reaches, set apart.
// A sleeping session holds its context without spending it: its bar is grey,
// as an archived one's.
function Edge({ pct, cap, auto, past = false, asleep = false }) {
    const capped = !past && cap != null && cap > 0 && cap < 100;
    return html`
        <span class=${`scedge ${past ? "scpeak" : asleep ? "scsleep" : fill(pct || 0)}`} aria-hidden="true">
            ${pct != null && html`<i style=${`width:${Math.max(0, Math.min(100, pct))}%`}></i>`}
            ${capped && html`<s class="scblock" style=${`left:${cap}%`}></s>`}
            ${capped && html`<b class=${`sccap${auto ? " scauto" : ""}`} style=${`left:${cap}%`}></b>`}
        </span>
    `;
}

// menuOn opens the menu of a card from a right click anywhere on it, the
// focus going back to the card's way into its conversation as the menu closes.
function menuOn(onMenu) {
    return onMenu ? (event) => onMenu(event, event.currentTarget.querySelector(".scopen")) : undefined;
}

// Act is one control of a card: an icon, its name for a reader and its tip;
// on paints it in the accent, pressed says a toggle's state to a reader and
// busy that its press is on its way. One that cannot be pressed now stays in
// its place, dimmed, and says why.
function Act({ label, tip, off, on = false, pressed, busy = false, onPress, children }) {
    return html`
        <button class=${`scact${on ? " scon" : ""}`} type="button" aria-label=${label} title=${tip || undefined}
                data-tip=${tip || undefined} data-tipside="left" aria-disabled=${off ? "true" : undefined}
                aria-pressed=${pressed === undefined ? undefined : String(pressed)} aria-busy=${busy ? "true" : undefined}
                onClick=${(e) => { e.stopPropagation(); if (!off) onPress(); }}>${children}</button>
    `;
}

// SessionCard is a live session. known is the name a heading or the opening
// session above it says already. depth is how many steps in the card stands
// under the sessions that opened it; from names a parent that stands
// elsewhere. A press anywhere on the card opens the conversation, a long one
// on a phone its pie; onMenu is the menu of a right click, where there is
// one; current marks the conversation open beside the list.
export function SessionCard({ s, limits, wait, exec, profile = null, profiles = [], notes = null, known = "",
    depth = 0, from = "", current = false, desk = false, onOpen, onMenu }) {
    const busy = busyOf(s, wait);
    const state = stateOf(s, limits, { viewed: current });
    const steps = stepsOf(s.checklist);
    const name = shownName(s);
    // An unassigned session can stand on the default contour's page, so the
    // page's label names its account only when the contour matches.
    // Codex can share another contour's auth file, whose home names its
    // owning contour in the map independently of the thread's own page.
    // A claude wrapper can name another token owner too, mapped by the
    // owner's config directory rather than the session's contour.
    const owner = s.account?.codexHome && profiles.find((p) => p.codexHome === s.account.codexHome);
    const claudeOwner = s.account?.contour && profiles.find((p) => p.id === s.account.contour);
    const account = isCodex(s) ? owner?.profile || s.account?.profile || ""
        : claudeOwner?.profile || s.account?.profile || (s.contour && profile?.id === s.contour && profile.profile) || s.profile || "";
    const time = timeOf(state);
    const timeTone = state.tone === "wait" ? "wait" : state.unseen ? "unseen" : ["idle", "paused"].includes(state.tone) ? "quiet" : "";
    // On a phone the controls are the card's pie, opened by a long press on
    // the card; a session the panel only reads has none either way.
    const pie = useCardPie();
    const card = useRef(null);
    const pies = !desk && Boolean(pie) && !s.outside;
    const hold = useHold(card, pies ? (press) => pie.open({ name: s.session, profile, card: card.current, press }) : null);
    return html`
        <div class=${`sc${state.tone === "wait" ? " scwait" : ""}${depth ? " sckid" : ""}${current ? " sccur" : ""}${desk ? " scdesk" : ""}${pies ? " scpie" : ""}`}
             data-name=${s.session} data-tone=${state.tone} data-kind=${state.kind} style=${depth ? `--depth:${depth}` : undefined}
             ref=${card} onContextMenu=${menuOn(onMenu)} ...${hold}>
            <button class="scopen" type="button" aria-label=${`open conversation ${s.session}`}
                    onClick=${() => onOpen && onOpen(s)}></button>
            <div class="scl1">
                <${Name} full=${name} address=${s.session} own=${nameOf(name, known)} tag=${from ? `opened by ${from}` : ""} />
                <${Resources} res=${s.res} account=${account} />
            </div>
            <${Line} state=${state} runs=${runsOf(s.work)} alarm=${wakeOf(s)} notes=${notes} busy=${busy} time=${time} tone=${timeTone} />
            <${Steps} ...${steps} />
            <${Agent} who=${s} model=${modelOf(s.model)} effort=${s.effort ? effortName(s.effort) : ""}
                      queued=${s.queued || 0} compacts=${s.compacts || 0} />
            <${Badges} mode=${s.mode || ""} place=${placeOf(s)} cap=${s.cap} auto=${s.autoRestart} remote=${Boolean(s.remote)}
                       worktree=${s.checkout || ""} heavyPlace=${s.heavyPlace || 0} ctx=${s.noRequests ? "—" : `${Math.round(s.pct || 0)}%`} />
            <${Edge} pct=${s.noRequests ? null : s.pct || 0} cap=${s.cap} auto=${s.autoRestart} asleep=${Boolean(s.sleeping)} />
        </div>
    `;
}

// PastCard is an archived conversation on the same five lines, dimmed: what
// its checklist stood at when it closed — or what the person last said in
// it — who ran it on what, its settings as they were, and how full it got —
// a thread of codex keeps no share of its context, and says what it spent;
// Resume at the right of its name. contour names the contour of
// a card on a shelf of several.
export function PastCard({ row, about = "", exec, known = "", contour = "", current = false, desk = false, onOpen, onMenu }) {
    const run = useAction();
    const last = row.last || {};
    const who = { agent: last.agent || row.agent };
    const steps = pastSteps(row.last, about ? `«${about}»` : "");
    const ready = knows(exec, "session.resume");
    const lastAt = stamp(row.lastAt);
    const codex = isCodex(who);
    const place = last.transport || (codex ? "daemon" : row.transport || "");
    return html`
        <div class=${`sc scpast${current ? " sccur" : ""}${desk ? " scdesk" : ""}`} data-name=${row.name} data-tone="closed" data-kind="closed"
             onContextMenu=${menuOn(onMenu)}>
            <button class="scopen" type="button" aria-label=${`open conversation ${row.name}`}
                    onClick=${() => onOpen && onOpen(row)}></button>
            <div class="scl1">
                <${Name} full=${row.name} own=${nameOf(row.name, known)} tag=${contour} />
                ${row.sessionId && html`
                    <span class="scacts">
                        <button class="scresume" type="button" aria-label=${`resume conversation of ${row.name}`}
                                aria-disabled=${ready ? undefined : "true"} title=${ready ? undefined : whyNot(exec, "session.resume")}
                                data-tip=${ready ? undefined : whyNot(exec, "session.resume")} data-tipside="left"
                                onClick=${(e) => { e.stopPropagation(); if (ready) run("session.resume", row.name, { session: row.sessionId, ...codexResume(row) }); }}>Resume</button>
                    </span>
                `}
            </div>
            <div class="scl2"><span class="scsay"><span class="scglyph" data-tone="closed" data-kind="closed" aria-hidden="true"><i class="scshape"></i></span><span class="scword">closed</span></span></div>
            <${Steps} ...${steps} />
            <${Agent} who=${who} model=${modelOf(last.model || row.model)} effort=${last.effort || row.effort ? effortName(last.effort || row.effort) : ""}
                      spent=${codex && row.tokensUsed ? `${tokens(row.tokensUsed)} tokens` : ""}
                      time=${{ until: false, text: lastAt ? shortText(Date.now() / 1000 - lastAt) : "" }} tone="quiet" />
            <${Badges} mode=${last.mode || row.mode || ""} place=${place} cap=${last.cap} auto=${false} remote=${Boolean(last.remote)} known=${Boolean(row.last)}
                       worktree=${last.checkout || ""} ctx=${codex ? "—" : `peak ${Math.round(row.pctMax || 0)}%`} />
            <${Edge} pct=${row.pctMax || 0} past />
        </div>
    `;
}

// GhostCard holds the place of a session being raised: none of its lines is
// known yet, so it is one line, in the columns of the card.
export function GhostCard({ task, known = "" }) {
    const sec = Math.max(0, Math.round((Date.now() - task.since) / 1000));
    return html`
        <div class="sc scghost" data-name=${task.target} role="status">
            <div class="scl1">
                <span class="scglyph" data-tone="busy" data-kind="ghost" aria-hidden="true"><span class="spin"></span></span>
                <${Name} full=${task.target} own=${nameOf(task.target, known)} />
                <span class="scword">starting</span>
                <span class="sctime">${sec}s</span>
            </div>
        </div>
    `;
}

// restartable lists the sessions of a heading a restart of them all reaches:
// what the panel started and can restart, and is not closing or restarting.
export function restartable(rows, wait) {
    return rows.filter((s) => !s.outside && !s.restarting && !(isCodex(s) && codexRestartWhy(s))
        && !(wait && (wait.of("close", s.session) || wait.of("restart", s.session))));
}

// HeadActs are the controls of a project's heading: a new session in it, the
// pause of the project — the continue once all of it is paused — and a
// restart of every live session under the heading, one after another, each
// through its own confirmation as a restart of one goes; a cancel stops the
// rest.
export function HeadActs({ block, profile, sessions, exec, wait }) {
    const run = useAction();
    const project = block.project;
    const canNew = project && !block.home;
    const newWhy = knows(exec, "session.open") ? "" : whyNot(exec, "session.open");
    const rows = restartable(block.live, wait);
    const restartWhy = (knows(exec, "session.restart") ? "" : whyNot(exec, "session.restart"))
        || (rows.length ? "" : "No session here can be restarted now");
    const restartAll = async () => {
        for (const s of rows) {
            const reply = await run("session.restart", s.session, {});
            if (!reply || reply.cancelled) return;
        }
    };
    return html`
        <span class="scheadacts">
            ${canNew && html`
                <${Act} label=${`new session in project ${block.name}`} tip=${newWhy || `A new session in ${block.name}`} off=${newWhy}
                        onPress=${() => run("session.open", project.session || project.name, { project: project.id })}>${Icon.plus()}<//>
            `}
            ${project && html`<${GroupPause} project=${project} profile=${profile} sessions=${sessions} rows=${block.live} exec=${exec} />`}
            <${Act} label=${`restart the sessions of ${block.name}`} off=${restartWhy}
                    tip=${restartWhy || `Restart ${rows.length} ${rows.length === 1 ? "session" : "sessions"}, each asked for on its own`}
                    onPress=${restartAll}>${Icon.refresh()}<//>
        </span>
    `;
}

// GroupPause is the pause of a project's heading: the sheet of the pause for
// the project, or, once every live session under the heading is paused, the
// continue of them all.
function GroupPause({ project, profile, sessions, rows, exec }) {
    const run = useAction();
    const [open, setOpen] = useState(false);
    const why = pauseWhy(project, profile, sessions, exec);
    const reached = rows.filter((s) => !s.outside && s.sessionId);
    if (reached.length > 0 && reached.every((s) => s.pauseAlarm)) {
        return html`
            <${Act} label=${`continue sessions in project ${project.name}`} tip=${why || "Continue them all now"} off=${why} on
                    onPress=${() => continueProject(run, project)}>
                ${Icon.play()}
            <//>
        `;
    }
    return html`
        <${Act} label=${`pause sessions in project ${project.name}`} tip=${why || "Pause sessions…"} off=${why}
                onPress=${() => setOpen(true)}>${Icon.pause()}<//>
        ${open && html`<${Sheet} open tall onClose=${() => setOpen(false)} label="pause sessions">
            <${PauseSheet} project=${project} profile=${profile} sessions=${sessions} exec=${exec} onDone=${() => setOpen(false)} />
        <//>`}
    `;
}
