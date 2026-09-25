// The “Sessions” tab: one page per profile, paged by swipe.

import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { liveOf, ownName } from "../catchup.js";
import { NotRecorded, Stale, Trouble } from "../ui/trouble.js";
import { plural } from "../format.js";
import { Icon } from "../ui/icons.js";
import { useSessionsArchive } from "../history.js";
import { Chat } from "./chat.js";
import { Ghost, LiveRow, PastRow } from "./sessions/card.js";
import { PastActions } from "./sessions/actions.js";
import { ProfileLimits } from "./sessions/limits.js";
import { pageNames, Pages, useProfilePage } from "./sessions/pages.js";
import { Past } from "./sessions/past.js";
import { Groups, pagesOf, placesOf, Project } from "./sessions/map.js";

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

const MIN_CARDS = 5;

// The strip shows MIN_CARDS of them and drops the conversations that said
// nothing, so it asks the archive for more than it shows.
const RECENT_ASK = MIN_CARDS * 4;

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

    // A session renamed while its conversation is open is followed to its new
    // name: every request of the screen goes by the name.
    const chatLive = chat ? liveOf((snapshot && snapshot.sessions) || [], chat) : null;
    useEffect(() => {
        if (chat && chatLive && chatLive.session !== chat.name) setChat({ ...chat, name: chatLive.session });
    }, [chat, chatLive]);

    if (chat) {
        return html`<${Chat}
            name=${chat.name}
            id=${chat.id}
            snapshot=${snapshot}
            live=${chatLive}
            exec=${exec}
            archive=${recent.find((r) => r.sessionId === chat.id) || null}
            onBack=${() => setChat(null)}
            onUsage=${onUsage}
        />`;
    }

    if (past) {
        return html`<${Past}
            profile=${profile}
            contour=${contour}
            onBack=${() => setPast(false)}
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
    const places = placesOf(profiles, sessions);
    const notes = splitNotes((snapshot && snapshot.sessionNotes) || [], sessions);

    if (project) {
        return html`<${Project}
            project=${project}
            sessions=${sessions}
            notes=${notes.own}
            exec=${exec}
            wait=${wait}
            onBack=${() => setProject(null)}
            onChat=${openChat}
        />`;
    }

    const blind = sessions.length === 0 && notes.common.length > 0;

    const byPage = pagesOf(profiles, sessions, names);
    const ghosts = ghostsOf(profiles, wait.opening());
    const live = new Map([...byPage].map(([name, own]) => [name, own.length]));

    return html`
        <div class="sidescroll">
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
                    places=${places}
                    notes=${notes.own}
                    blind=${blind}
                    exec=${exec}
                            wait=${wait}
                    onProject=${setProject}
                    onChat=${openChat}
                    onPast=${() => setPast(true)}
                />
            `}
        />

        </div>
    `;
}

// ghostsOf returns on whose page to show each console being raised.
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

function Page({ name, profile, limits, stale, sessions, opening, recent, places, notes, blind, exec, wait, onProject, onChat, onPast }) {
    const live = sessions.slice().sort((a, b) => Number(Boolean(b.home)) - Number(Boolean(a.home)));

    const filler = fillTo(recent, live, opening, MIN_CARDS);

    const homeRow = live.some((s) => s.home)
        ? null
        : recent.find((row) => row.home) || null;
    const rest = homeRow ? filler.filter((row) => row.sessionId !== homeRow.sessionId) : filler;

    return html`
        <${ProfileLimits} limits=${limits} profile=${name} contour=${profile ? profile.id : 0} stale=${stale} />

        ${live.length === 0 && opening.length === 0 && filler.length === 0
            ? !blind && html`<p class="empty">There are no live sessions.</p>`
            : html`
                <div class="grouphead">recent sessions</div>
                ${live.map((s) => html`
                    <${LiveRow}
                        key=${s.session}
                        session=${s}
                        where=${profile ? places.get(s.session) || null : undefined}
                        notes=${notes.get(s.session)}
                        exec=${exec}
                                    wait=${wait}
                        onOpen=${onChat}
                    />
                `)}
                ${opening.map((task) => html`<${Ghost} key=${task.target} task=${task} />`)}
                ${homeRow && html`
                    <${PastRow}
                        key=${`home-${homeRow.sessionId}`}
                        row=${homeRow}
                        dim
                        onOpen=${onChat}
                        action=${html`<${PastActions} row=${homeRow} exec=${exec} />`}
                    />
                `}
                ${rest.map((row) => html`
                    <${PastRow}
                        key=${`past-${row.sessionId}`}
                        row=${row}
                        dim
                        onOpen=${onChat}
                        action=${html`<${PastActions} row=${row} exec=${exec} />`}
                    />
                `)}
            `}

        ${profile && html`
            <${Groups}
                profile=${profile}
                sessions=${sessions}
                onOpen=${onProject}
                exec=${exec}
            />
        `}

        <${PastButton} onOpen=${onPast} />
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

// fillTo returns what to fill the list up to the wanted length with.
// spoken drops the conversations that never said a word. A transcript can hold
// nothing but a snapshot of file history or a summary, and a card for one names
// a talk that never happened. The archive counts and pages every conversation
// it has; which of them are worth a card is the screen's question.
export function spoken(rows) {
    return (rows || []).filter((row) => (row.messages || 0) > 0);
}

export function fillTo(archive, live, opening, want) {
    const rising = opening || [];
    const need = want - live.length - rising.length;
    if (need <= 0) return [];
    const taken = new Set(live.map((s) => s.sessionId).filter(Boolean));
    const names = new Set(live.map((s) => s.session));
    return (archive || [])
        .filter((row) => !(row.sessionId && taken.has(row.sessionId)))
        .filter((row) => !names.has(row.name))
        .filter((row) => !rising.some((task) => ownName(row.name, task.target)))
        .slice(0, need);
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
