// The sessions of a contour as blocks of their projects. A person thinks in
// projects — "is aacpanel waiting for me", not "is session four" — so a block
// holds a project's live sessions as rows and its last conversation, and the
// list puts pinned groups first, then what needs the person, and the rest
// in the order of the map.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { plural } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { sessionsOf } from "./of.js";
import { kinLabel, kinOf, layoutOf, openedLabel } from "./kin.js";
import { useFolds } from "./folds.js";
import { shownName } from "../../agent.js";
import { listen } from "../chat/answered.js";
import { stamp, when } from "./card.js";
import { stateOf } from "./state.js";
import { GhostCard, Glyph, HeadActs, PastCard, SessionCard, nameOf } from "./sessioncard.js";
import { comparePins, pinAt } from "./pin.js";

// The most pressing state of a block: who waits for the person, who works —
// on an answer or in its background — and who is quiet.
export const RANK = { wait: 0, busy: 1, quiet: 2 };

// How many projects with nothing live the list shows under the live ones: the
// rest is a tap away in the archive, which holds every project.
export const QUIET_MAX = 5;

// A message that says nothing about the work: "go on", "yes", the word a
// restart opens with. The card is named by the last one that says something.
const ABOUT_MIN = 12;

export { stateOf };

// splitNotes sorts the collector notes by whom they are addressed to: a note
// that opens with the name of a live session goes on its card.
export function splitNotes(notes, sessions) {
    const live = new Set(sessions.map((s) => s.session));
    const own = new Map();
    const common = [];

    for (const note of notes || []) {
        const at = note.indexOf(":");
        const name = at > 0 ? note.slice(0, at) : "";
        if (name && live.has(name)) {
            const text = note.slice(at + 1).trim();
            own.set(name, [...(own.get(name) || []), text]);
            continue;
        }
        common.push(note);
    }
    return { own, common };
}

// useAnswers redraws a list of sessions when an answer from this page marks a
// session answered, or the mark goes: the list stands beside the conversation
// at a desk, and the row should stop waiting with the head of it, not at the
// next snapshot.
export function useAnswers() {
    const [, redraw] = useState(0);
    useEffect(() => listen(() => redraw((n) => n + 1)), []);
}

export { stopsOf } from "../chat/head.js";

// intentOf returns the first message a project opens with, as its launch line
// names it: a conversation is not named by the words it was opened with.
function intentOf(project) {
    const words = (project && project.line && project.line.words) || [];
    const said = words.find((w) => w.key === "intent");
    return said ? said.text.trim() : "";
}

// aboutOf names an archived conversation by the last thing the person said in
// it that says something: not a slash command, not the project's opening
// message, and not a word or two.
export function aboutOf(row, project) {
    const intent = intentOf(project);
    const said = (row && row.prompts) || [];
    for (let i = said.length - 1; i >= 0; i--) {
        const text = String(said[i] || "").trim();
        if (text.length < ABOUT_MIN || text.startsWith("/") || (intent && text === intent)) continue;
        return text;
    }
    return "";
}

// projectsIn lists the projects of a contour's map in its order, each with the
// name of the group it stands in.
function projectsIn(profile) {
    const out = [];
    for (const group of (profile && profile.groups) || []) {
        for (const project of group.projects || []) out.push({ project, group: group.name });
    }
    return out;
}

// liveBlocksOf lays the live sessions of a contour out as blocks of their
// projects, in the order of the map: every live session in the block of its
// project, and a session being raised in the block of the project it is
// raised in, at the place of the project whether anything there lives yet or
// not; a session no project holds, or raised in no project, in a block of its
// own after them.
// A session another started — opened through the panel or run inside its
// work — stands in the block of the eldest of them, whatever its own project:
// the group is read as one. A block keeps its sessions in the order of the
// list it is given, the ones another started after the rest; how they stand
// is the business of the screen that draws them.
export function liveBlocksOf({ profile, sessions = [], opening = [], wait = null }) {
    const { rootOf } = kinOf(sessions);
    const tops = sessions.filter((s) => rootOf(s) === s);
    const projects = projectsIn(profile);
    const out = new Map();
    const put = (key, init) => {
        if (!out.has(key)) out.set(key, { key, live: [], ghosts: [], home: false, ...init });
        return out.get(key);
    };

    const claimed = new Set();
    const raised = new Set();
    for (const { project, group } of projects) {
        const own = sessionsOf(project, tops);
        const ghosts = opening.filter((task) => !raised.has(task) && task.target === project.session);
        if (own.length === 0 && ghosts.length === 0) continue;
        const block = put(`p${project.id}`, { name: project.name, group, project });
        for (const s of own) {
            block.live.push(s);
            block.home = block.home || Boolean(s.home);
            claimed.add(s.session);
        }
        for (const task of ghosts) {
            block.ghosts.push(task);
            raised.add(task);
        }
    }
    for (const s of tops) {
        if (claimed.has(s.session)) continue;
        const block = put(`s:${s.session}`, { name: s.session, group: s.home ? "the home session" : "outside the map", project: null });
        block.live.push(s);
        block.home = block.home || Boolean(s.home);
    }
    const blockOf = new Map();
    for (const block of out.values()) {
        for (const s of block.live) blockOf.set(s.session, block);
    }
    for (const s of sessions) {
        const root = rootOf(s);
        if (root !== s && blockOf.has(root.session)) blockOf.get(root.session).live.push(s);
    }
    for (const task of opening) {
        if (!raised.has(task)) put(`s:${task.target}`, { name: task.target, group: "", project: null }).ghosts.push(task);
    }
    // Only the pinned root and its children move. Moving its whole project
    // would lift unrelated sessions and mix the order of several pins.
    const pinned = [];
    const rest = [];
    for (const block of out.values()) {
        const roots = block.live.filter((s) => rootOf(s) === s && pinAt(s, wait));
        for (const root of roots) {
            const live = block.live.filter((s) => rootOf(s) === root);
            pinned.push({ ...block, key: `pin:${root.session}`, live, ghosts: [],
                home: live.some((s) => s.home), pinnedAt: pinAt(root, wait) });
        }
        block.live = block.live.filter((s) => !roots.includes(rootOf(s)));
        if (block.live.length || block.ghosts.length) rest.push(block);
    }
    return [...pinned.sort((a, b) => comparePins(a.pinnedAt, b.pinnedAt)), ...rest];
}

// blocksOf lays the sessions of a contour out as the blocks of the list: the
// live ones as liveBlocksOf has them, and the last conversation of a block
// with nothing live. After the pins, a block that needs the person comes
// first; the rest keep the map's order whatever their activity. A block with
// a live session shows none of its past: the way into the project is the live
// session, and a closed conversation beside it reads as that session.
// Projects with nothing live come last and at most QUIET_MAX of them.
export function blocksOf({ profile, sessions = [], recent = [], opening = [], wait = null, limits = null }) {
    const projects = projectsIn(profile);
    // The snapshot follows context fill; names keep rows within a project
    // and outside the map from moving whenever their work changes.
    const ordered = [...sessions].sort((a, b) => a.session.localeCompare(b.session));
    const out = new Map(liveBlocksOf({ profile, sessions: ordered, opening, wait }).map((block) => [block.key, { ...block, past: null }]));
    const put = (key, init) => {
        if (!out.has(key)) out.set(key, { key, live: [], ghosts: [], past: null, home: false, ...init });
        return out.get(key);
    };

    const live = new Set(sessions.map((s) => s.sessionId).filter(Boolean));
    const liveProjects = new Set([...out.values()].filter((b) => b.live.length && b.project).map((b) => b.project.id));
    let quiet = 0;
    for (const row of recent) {
        if (live.has(row.sessionId)) continue;
        if (row.project && liveProjects.has(row.project.id)) continue;
        const key = row.project ? `p${row.project.id}` : `d:${row.cwd || row.name}`;
        let block = out.get(key);
        if (!block) {
            if (quiet >= QUIET_MAX) continue;
            quiet++;
            const known = row.project && projects.find((p) => p.project.id === row.project.id);
            block = put(key, {
                name: (row.project && row.project.name) || row.name,
                group: (row.project && row.project.group) || (row.home ? "the home session" : ""),
                project: known ? known.project : null,
                home: Boolean(row.home),
            });
        }
        if (block.live.length > 0) continue;
        if (!block.past) block.past = row;
    }

    for (const block of out.values()) {
        const tones = block.live.map((s) => stateOf(s, limits).tone);
        block.rank = tones.includes("wait") ? RANK.wait
            : tones.includes("busy") || tones.includes("bg") || block.ghosts.length > 0 ? RANK.busy
                : RANK.quiet;
    }
    // liveBlocksOf supplies the map's order and the archive is appended.
    // Only attention lifts an unpinned block; activity cannot move the rest.
    const attention = (b) => !b.pinnedAt && b.rank === RANK.wait ? 0 : 1;
    return [...out.values()].sort((a, b) => comparePins(a.pinnedAt, b.pinnedAt) || attention(a) - attention(b));
}

// headed says whether a block stands under a heading of its own: every
// project of the map, a lone card or a quiet one included, and a group of
// two cards or more that no project holds. The heading says the project and
// carries its plus, so a card under it is the session alone; a lone session
// no project holds has no heading and reads by its whole name. Without the
// heading a card named as its project reads as a session outside the map.
export function headed(block) {
    return Boolean(block.project) || block.live.length + block.ghosts.length > 1;
}

// knownOf is what the heading of a block says of the names of its cards
// already: the session of its project, which they drop. A heading over cards
// no project holds says nothing of their names.
export function knownOf(block) {
    return block.project ? block.project.session || block.project.name : "";
}

// ProjectBlock is one project: a heading with its name, its group and its
// controls (see headed); its live sessions, those being raised and, with
// none, its last conversation.
export function ProjectBlock({ block, profile, profiles, exec, wait, notes, live, past, onOpen, onProject, limits }) {
    const project = block.project;
    const known = knownOf(block);
    const sessions = live || block.live;
    return html`
        <section class=${`pjblock${block.live.length === 0 && block.ghosts.length === 0 ? " pjquiet" : ""}`}>
            ${headed(block) && html`
                <div class="pjhead">
                    ${project && onProject
                        ? html`<button class="pjname" type="button" aria-label=${`project ${block.name}`}
                                       onClick=${() => onProject(project, block.group)}>${block.name}</button>`
                        : html`<span class="pjname">${block.name}</span>`}
                    ${block.group && html`<span class="pjgroup">${block.group}</span>`}
                    <${HeadActs} block=${block} profile=${profile} sessions=${sessions} exec=${exec} wait=${wait} />
                </div>
            `}
            <${LiveLines} limits=${limits} list=${block.live} live=${live} past=${past} notes=${notes} wait=${wait}
                          onOpen=${onOpen} exec=${exec} profile=${profile} profiles=${profiles} known=${known} />
            ${block.ghosts.map((task) => html`<${GhostCard} key=${task.target} task=${task} known=${known} />`)}
            ${block.past && html`<${PastCard} row=${block.past} about=${aboutOf(block.past, project)} exec=${exec} known=${known}
                                            onOpen=${(row) => onOpen && onOpen(row.name, row.sessionId)} />`}
        </section>
    `;
}

// LiveLines lays out the live sessions of a project as cards: the sessions a
// session had the panel open stand a step in under it, behind a fold that is
// open until the person closes it and says who of them waits; the runs it
// started inside its work fold under it, closed; the sessions of a parent
// that closed stand under what is left of it. live is every live session the
// screen knows of, and past the closed conversations it has read: they tell a
// parent alive elsewhere from one that closed, and when it closed. known is
// what the heading over the cards says of their names already. A desk hands
// its own folds — its arrow keys go down the same rows — the conversation
// open beside the column (current), the menu of a card (menuOf) and how a
// closed parent opens (onStub).
export function LiveLines({ list, live = null, past = [], notes, wait, exec, onOpen, limits, profile = null, profiles = [], known = "",
    folds = null, desk = false, current = "", menuOf = null, onStub = null }) {
    const own = useFolds();
    const [folded, fold] = folds || own;
    const card = (s, depth = 0, from = "", parent = "") => html`
        <${SessionCard} key=${s.session} s=${s} depth=${depth} from=${from} limits=${limits} wait=${wait} exec=${exec}
                        profile=${profile} profiles=${profiles} notes=${notes && notes.get(s.session)}
                        known=${parent || known}
                        desk=${desk} current=${current === s.session} onMenu=${menuOf ? menuOf(s) : null}
                        onOpen=${(row) => onOpen && onOpen(row.session, row.sessionId)} />
    `;
    const openStub = onStub || ((row) => onOpen && onOpen(row.name, row.sessionId));
    const row = (e) => {
        switch (e.kind) {
        case "fold":
            return html`<${OpenedFold} limits=${limits} key=${`fold:${e.parent.session}`} kids=${e.kids} hidden=${e.hidden} open=${e.open}
                                      depth=${e.depth} onToggle=${() => fold(e.parent.session)} />`;
        case "runs":
            return html`<${KinFold} limits=${limits} key=${`kin:${e.parent.session}`} kids=${e.kids} depth=${e.depth}
                                   line=${(s) => card(s, e.depth, "", e.parent.session)} />`;
        case "stub":
            return html`<${ParentStub} key=${`stub:${e.parent}`} parent=${e.parent} row=${closedRow(past, e.parent)} onOpen=${openStub} />`;
        default:
            return card(e.s, e.depth - 1, e.from || "", e.s.parent ? e.s.parent.session : "");
        }
    };
    return layoutOf(list, live, folded).map((e) => html`
        <div key=${`${e.kind}:${e.s ? e.s.session : e.parent.session || e.parent}`} class="pjtree">${row(e)}</div>
    `);
}

// waitsOf says who under a fold waits for the person: the one that does, by
// its name and the state its glyph is drawn from, or how many do.
export function waitsOf(kids, limits = null) {
    const waiting = kids.map((s) => ({ s, state: stateOf(s, limits) })).filter((k) => k.state.tone === "wait");
    if (waiting.length === 0) return null;
    if (waiting.length > 1) return { count: waiting.length, name: "", state: waiting[0].state };
    const s = waiting[0].s;
    return { count: 1, name: nameOf(shownName(s), s.parent ? s.parent.session : ""), state: waiting[0].state };
}

// Waits is what waitsOf found, in the colour of waiting: a closed fold does
// not hide a question. One that waits is its glyph and its name, several
// are a count.
export function Waits({ waits }) {
    if (!waits) return null;
    if (waits.count > 1) return html`<span class="scfoldwaits">${waits.count} wait for you</span>`;
    return html`<span class="scfoldwaits" title=${`${waits.name}: ${waits.state.text}`}><${Glyph} state=${waits.state} />${waits.name}</span>`;
}

// closedRow is the latest closed conversation of a session, by its name, among
// the ones a screen has read; null when it has read none.
export function closedRow(past, name) {
    return (past || []).find((row) => row.name === name) || null;
}

// OpenedFold heads the sessions a session had the panel open: how many, and
// who in their subtree waits for the person. Its count names only the
// children standing directly under it.
function OpenedFold({ kids, hidden, open, onToggle, limits, depth = 1 }) {
    return html`
        <button class="scfold" type="button" aria-expanded=${open ? "true" : "false"} style=${`--depth:${depth}`}
                onClick=${onToggle}>
            <span class=${`scfoldchev${open ? " open" : ""}`}>${Icon.chevron()}</span>
            <span class="scfoldtext">${openedLabel(kids.length)}</span>
            <${Waits} waits=${waitsOf(hidden, limits)} />
        </button>
    `;
}

// ParentStub holds the place of a parent that closed, over the sessions it
// opened: its name, struck through, and when it closed. Where the screen has
// read its conversation a press opens it, as a closed conversation opens.
function ParentStub({ parent, row, onOpen }) {
    const closed = row ? `closed ${when(stamp(row.lastAt))}` : "closed";
    if (!row || !onOpen) {
        return html`
            <div class="scstub"><span class="scstubname">${parent}</span><span class="scstubnote">${closed}</span></div>
        `;
    }
    return html`
        <button class="scstub" type="button" aria-label=${`open the closed conversation ${parent}`}
                onClick=${() => onOpen(row)}>
            <span class="scstubname">${parent}</span>
            <span class="scstubnote">${closed}</span>
            <span class="chev">${Icon.chevron()}</span>
        </button>
    `;
}

// KinFold holds the runs a session started inside its work under it, closed
// until the person opens it. A run that waits for the person stands outside
// the fold, where it is seen; the fold keeps the rest.
function KinFold({ kids, line, limits, depth = 1 }) {
    const [open, setOpen] = useState(false);
    const waiting = kids.filter((s) => stateOf(s, limits).tone === "wait");
    const rest = kids.filter((s) => !waiting.includes(s));
    return html`
        ${waiting.map((s) => line(s))}
        ${rest.length > 0 && html`
            <button class="scfold" type="button" aria-expanded=${open ? "true" : "false"} style=${`--depth:${depth}`}
                    onClick=${() => setOpen(!open)}>
                <span class=${`scfoldchev${open ? " open" : ""}`}>${Icon.chevron()}</span>
                <span class="scfoldtext">${waiting.length > 0 ? `${rest.length} more ${plural(rest.length, "run", "runs")}` : kinLabel(rest.length)}</span>
            </button>
            ${open && rest.map((s) => line(s))}
        `}
    `;
}
