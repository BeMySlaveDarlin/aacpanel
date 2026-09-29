// The “Sessions” tab: one page per profile, paged by swipe.

import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { follow, liveOf } from "../catchup.js";
import { NotRecorded, Stale, Trouble } from "../ui/trouble.js";
import { plural } from "../format.js";
import { Icon } from "../ui/icons.js";
import { ContourDoor } from "./profiles/door.js";
import { useSessionsArchive } from "../history.js";
import { Chat } from "./chat.js";
import { blocksOf, ProjectBlock, RANK, SessionSheet } from "./sessions/blocks.js";
import { ProfileLimits } from "./sessions/limits.js";
import { pageNames, Pages, useProfilePage } from "./sessions/pages.js";
import { Past } from "./sessions/past.js";
import { pagesOf, Project } from "./sessions/map.js";

// splitNotes sorts the collector notes by whom they are addressed to.
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

// sessionChips reports that the sessions tab has no chips.
export function sessionChips() {
    return null;
}

// The last conversations the page lays out by project: the quiet projects
// under the live ones, each with its last conversation. The ones that said
// nothing are dropped, so the page asks the archive for more than it shows.
const RECENT_ASK = 20;

// Sessions renders the sessions tab.
export function Sessions({ snapshot, error, ageSec, exec, wait, faults = [], onLayer, want, onWanted, pick, onUsage }) {
    const [project, setProject] = useState(null);

    const profiles = (snapshot && snapshot.profileMap) || [];
    const limits = (snapshot && snapshot.limits) || null;
    const names = pageNames(profiles, limits);
    const [profile, pickProfile] = useProfilePage(names);
    const contour = (profiles.find((p) => p.profile === profile) || {}).id || 0;
    const stale = ageSec === null || ageSec === undefined;

    const [past, setPast] = useState(false);
    const [settings, setSettings] = useState(0);

    const [shown, setShown] = useState([]);

    const [chat, setChat] = useState(null);
    const openChat = (name, id) =>
        pick ? pick({ name, id: id || null }) : setChat({ name, id: id || null });

    useEffect(() => {
        if (!want) return;
        const target = { name: want.name, id: want.id || null };
        if (pick) pick(target);
        else setChat(target);
        if (onWanted) onWanted();
    }, [want, onWanted, pick]);

    const layer = Boolean(project) || past || Boolean(chat);
    useEffect(() => {
        if (onLayer) onLayer(layer);
    }, [layer, onLayer]);

    const recentState = useSessionsArchive({ limit: RECENT_ASK, profile, contour });
    const recent = recentState.kind === "ready" ? spoken(recentState.archive.rows) : [];

    // The open conversation follows its session: to a new name when it is
    // renamed, to a new conversation when it is restarted.
    const chatLive = chat ? liveOf((snapshot && snapshot.sessions) || [], chat) : null;
    useEffect(() => {
        if (!chat) return;
        const next = follow((snapshot && snapshot.sessions) || [], chat);
        if (next !== chat) setChat(next);
    }, [chat, snapshot]);

    if (chat) {
        return html`<${Chat}
            name=${chat.name}
            id=${chat.id}
            snapshot=${snapshot}
            live=${chatLive}
            exec=${exec}
            wait=${wait}
            archive=${recent.find((r) => r.sessionId === chat.id) || null}
            onBack=${() => setChat(null)}
            onUsage=${onUsage}
            onOpenChat=${(talk) => setChat({ name: talk.name, id: talk.live ? null : talk.id })}
        />`;
    }

    if (settings) return html`<${ContourDoor} id=${settings} onClose=${() => setSettings(0)} />`;

    // A project opened from the archive comes back to the archive: the project
    // is looked at before it.
    const sessionsNow = (snapshot && snapshot.sessions) || [];
    if (project) {
        return html`<${Project}
            project=${project}
            sessions=${sessionsNow}
            notes=${splitNotes((snapshot && snapshot.sessionNotes) || [], sessionsNow).own}
            exec=${exec}
            wait=${wait}
            onBack=${() => setProject(null)}
            onChat=${openChat}
        />`;
    }

    if (past) {
        return html`<${Past}
            profile=${profile}
            contour=${contour}
            map=${profiles.find((p) => p.profile === profile) || null}
            sessions=${sessionsNow}
            onBack=${() => setPast(false)}
            onProject=${setProject}
            exec=${exec}
            chat=${openChat}
        />`;
    }

    if (error) {
        return html`
            <${Trouble} what="claude sessions" error=${error} hint="they are read by aacpanel-agent on the host — check whether it is running." />
            <${PastButton} onOpen=${() => setPast(true)} />
        `;
    }

    const sessions = (snapshot && snapshot.sessions) || [];
    const notes = splitNotes((snapshot && snapshot.sessionNotes) || [], sessions);
    const blind = sessions.length === 0 && notes.common.length > 0;

    const byPage = pagesOf(profiles, sessions, names);
    const ghosts = ghostsOf(profiles, wait.opening());
    const live = new Map([...byPage].map(([name, own]) => [name, own.length]));

    return html`
        <${Stale} ageSec=${ageSec} />
        <${NotRecorded} faults=${faults} blocks=${["sessions"]} />
        <${Notes} list=${notes.common} />
        ${blind && html`
            <${Trouble}
                what="claude sessions"
                error="the session collector did not run — the list is empty not because there are no sessions"
                hint="they are counted by aacpanel-agent on the host — why it did not work is said in the line above."
            />
        `}

        <${Pages}
            names=${names}
            current=${profile}
            onPick=${pickProfile}
            onShown=${setShown}
            live=${live}
            page=${(name) => html`
                <${Page}
                    key=${name}
                    name=${name}
                    profile=${profiles.find((p) => p.profile === name) || null}
                    limits=${limits}
                    stale=${stale}
                    sessions=${profiles.length > 0 ? byPage.get(name) || [] : sessions}
                    opening=${profiles.length > 0 ? ghosts.get(name) || [] : wait.opening()}
                    recent=${name === profile ? recent : []}
                    notes=${notes.own}
                    blind=${blind}
                    exec=${exec}
                            wait=${wait}
                    onProject=${setProject}
                    onChat=${openChat}
                    onPast=${() => setPast(true)}
                    onSettings=${setSettings}
                />
            `}
        />
    `;
}

// ghostsOf returns on whose page to show each session being raised.
export function ghostsOf(profiles, opening) {
    const out = new Map(profiles.map((p) => [p.profile, []]));
    const fallback = profiles.length > 0 ? profiles[0].profile : null;
    for (const task of opening) {
        const own = profiles.find((profile) => profile.groups
            .some((group) => group.projects.some((p) => p.session === task.target)));
        const page = out.get(own ? own.profile : fallback);
        if (page) page.push(task);
    }
    return out;
}

// The sections of a contour's page, in the order the list is read.
const SECTIONS = [
    { rank: RANK.wait, title: "needs you", tone: " pjwaits" },
    { rank: RANK.busy, title: "working", tone: "" },
    { rank: RANK.quiet, title: "quiet", tone: "" },
];

function Page({ name, profile, limits, stale, sessions, opening, recent, notes, blind, exec, wait, onProject, onChat, onPast, onSettings }) {
    const [acting, setActing] = useState("");
    const blocks = blocksOf({ profile, sessions, recent, opening });
    const acted = acting ? sessions.find((s) => s.session === acting) || null : null;
    const toProject = (project, group) => onProject({ ...project, profile: name, contour: profile ? profile.id : 0, group });
    return html`
        <${ProfileLimits} limits=${limits} profile=${name} contour=${profile ? profile.id : 0} stale=${stale} />
        ${blocks.length === 0
            ? !blind && html`<p class="empty">There are no live sessions.</p>`
            : SECTIONS.map((section) => {
                const list = blocks.filter((b) => b.rank === section.rank);
                if (list.length === 0) return null;
                return html`
                    <div class=${`grouphead pjsection${section.tone}`} key=${section.title}>
                        ${section.title}${section.rank !== RANK.quiet && html`<span class="pjcount">${list.length}</span>`}
                    </div>
                    ${list.map((block) => html`
                        <${ProjectBlock} key=${block.key} block=${block} exec=${exec} wait=${wait} notes=${notes}
                                         onOpen=${onChat} onMore=${(s) => setActing(s.session)} onProject=${toProject} />
                    `)}
                `;
            })}
        <button class="crumb wide" type="button" onClick=${onPast}>
            all projects and the archive
            <span class="chev">${Icon.chevron()}</span>
        </button>
        ${profile && html`
            <button class="crumb wide" type="button" onClick=${() => onSettings(profile.id)}>
                contour settings
                <span class="chev">${Icon.chevron()}</span>
            </button>
        `}
        <${SessionSheet} session=${acted} exec=${exec} onClose=${() => setActing("")} onOpen=${onChat} />
    `;
}

function PastButton({ onOpen }) {
    return html`
        <button class="crumb wide" type="button" onClick=${onOpen}>
            session archive
            <span class="chev">${Icon.chevron()}</span>
        </button>
    `;
}

// spoken drops the conversations that never said a word. A transcript can hold
// nothing but a snapshot of file history or a summary, and a card for one names
// a talk that never happened. The archive counts and pages every conversation
// it has; which of them are worth a card is the screen's question.
export function spoken(rows) {
    return (rows || []).filter((row) => (row.messages || 0) > 0);
}

const NOTES_SHOWN = 3;

function Notes({ list }) {
    const [all, setAll] = useState(false);
    if (!list || list.length === 0) return null;

    const shown = all ? list : list.slice(0, NOTES_SHOWN);
    const rest = list.length - shown.length;
    return html`
        <div class="notes">
            ${shown.map((note) => html`<p class="hint warn" key=${note}>${note}</p>`)}
            ${rest > 0 && html`
                <button class="ghost" type="button" onClick=${() => setAll(true)}>
                    ${rest} more ${plural(rest, "note", "notes")}
                </button>
            `}
        </div>
    `;
}
