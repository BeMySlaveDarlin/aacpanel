// The settings of the map on a wide screen: three columns over the panel —
// the tree of contours and their groups, the page of the contour or the group
// picked in it, and the page of a project picked on the group (or the journal
// of the map). Each page keeps its own draft and bar; a pick in the tree that
// would replace a page with changes on it asks first. A project dragged from
// the group's page onto a group of the tree moves there. A page closed by its
// own arrow has asked about its draft itself; the frame asks only for the
// pages a pick or a close of another one would take with it. The Projects
// button of the sessions section opens them straight away: a contour, a group
// and a project are deleted on their own pages, a new session of a project is
// started from its page, and the tree finds a group or a project by name.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { Sheet } from "../ui/sheet.js";
import { plural } from "../format.js";
import { knows, whyNot } from "../exec.js";
import { useAction } from "../actions/gate.js";
import { useProfileMap } from "../screens/profiles/state.js";
import { ContourSettings } from "../screens/profiles/contour.js";
import { GroupSettings } from "../screens/profiles/shelf.js";
import { ProjectSettings } from "../screens/profiles/settings.js";
import { Journal } from "../screens/profiles/journal.js";
import { EditLayer } from "../screens/profiles/forms.js";

// The tree offers to find a group or a project once there are more groups
// than a glance takes in.
const FIND_FROM = 5;

// startOf says where the columns open: where they were left while that is
// still on the map, else the first contour the sessions column shows, else
// the first contour of the map; nothing on a map with no contour at all.
export function startOf(profiles, last, picks) {
    const list = profiles || [];
    const contour = last ? list.find((p) => p.id === last.contour) : null;
    if (contour) {
        const group = last.group ? (contour.groups || []).find((g) => g.id === last.group) : null;
        const project = group && last.project ? (group.projects || []).find((p) => p.id === last.project) : null;
        const at = { contour: contour.id };
        if (group) at.group = group.id;
        if (project) at.project = project.id;
        return at;
    }
    const first = list.find((p) => (picks || []).includes(p.name)) || list[0];
    return first ? { contour: first.id } : null;
}

// findIn narrows the tree to what a query names: the groups whose name has
// it, and the groups with a project whose name or directory has it, each with
// those projects. An empty query keeps the whole tree and names no project.
export function findIn(groups, query) {
    const text = String(query || "").trim().toLowerCase();
    if (!text) return (groups || []).map((group) => ({ group, projects: [] }));
    const out = [];
    for (const group of groups || []) {
        const projects = (group.projects || []).filter((p) => `${p.name} ${p.path}`.toLowerCase().includes(text));
        if (projects.length > 0 || group.name.toLowerCase().includes(text)) out.push({ group, projects });
    }
    return out;
}

// MapSettings is the settings of the map opened from the sessions section:
// the columns at the place they were left, a note while the map loads or when
// it cannot, and on a map with no contour yet the way to the first one.
export function MapSettings({ picks, last, exec, sessions, onPick, onClose }) {
    const map = useProfileMap();
    const { profiles, error } = map;
    if (!error && profiles !== null && profiles.length === 0) {
        return html`<${EditLayer}
            form=${{ kind: "profile", mode: "add" }}
            profiles=${profiles}
            catalog=${map.catalog}
            disk=${map.disk}
            accounts=${map.accounts}
            order=${null}
            onClose=${onClose}
            onDone=${map.apply}
            onRemove=${map.remove}
        />`;
    }
    const start = profiles ? startOf(profiles, last, picks) : null;
    if (error || !start) {
        return html`
            <div class="dkscrim dkcolscrim">
                <div class="dkcols dkcolsnote" role="dialog" aria-modal="true" aria-label="settings of the map">
                    <button class="dkclose dkcolsclose" type="button" aria-label="close" onClick=${onClose}>${Icon.close()}</button>
                    <p class=${`dkempty${error ? " dkfail" : ""}`}>${error || "loading the map…"}</p>
                </div>
            </div>
        `;
    }
    return html`<${SettingsColumns} map=${map} start=${start} sessions=${sessions} exec=${exec} onPick=${onPick} onClose=${onClose} />`;
}

// Start is the new session of a project, at the head of its page: the way in
// the sessions column is the plus of its contour, and this one is for the
// project already open here. A session that starts closes the settings, so
// the person sees it come up in the column.
function Start({ project, exec, onStarted }) {
    const run = useAction();
    const ready = knows(exec, "session.open");
    return html`
        <button class=${`btn dkcolstart${ready ? "" : " off"}`} type="button"
                aria-label=${`a new session of ${project.name}`}
                data-tip=${ready ? undefined : whyNot(exec, "session.open")} data-tipside="left"
                onClick=${async () => {
                    if (!ready) return;
                    const done = await run("session.open", project.session || project.name, { project: project.id });
                    if (done && done.ok) onStarted();
                }}><${Icon.play} />New session</button>
    `;
}

// replaced says which column a new pick throws away: the middle one when the
// contour or the group changes, the right one when only the project does.
export function replaced(from, to) {
    const middle = from.contour !== to.contour || (from.group || 0) !== (to.group || 0);
    const right = middle || (from.project || 0) !== (to.project || 0) || Boolean(from.journal) !== Boolean(to.journal);
    return { middle, right };
}

export function SettingsColumns({ map, start, sessions, exec, onPick, onClose }) {
    const { profiles, catalog, disk, apply, remove } = map;
    const [pick, setPick] = useState(start);
    const [dirty, setDirty] = useState({ middle: 0, right: 0 });
    const [asking, setAsking] = useState(null);
    const [adding, setAdding] = useState(null);
    const [query, setQuery] = useState("");

    const contour = (profiles || []).find((p) => p.id === pick.contour) || null;
    const group = contour && pick.group ? (contour.groups || []).find((g) => g.id === pick.group) || null : null;
    const project = group && pick.project ? (group.projects || []).find((p) => p.id === pick.project) || null : null;

    useEffect(() => {
        if (!contour) onClose();
    }, [contour]);
    useEffect(() => {
        if (onPick) onPick(pick);
    }, [pick]);

    const go = (to) => {
        const lost = replaced(pick, to);
        const changes = (lost.middle ? dirty.middle : 0) + (lost.right ? dirty.right : 0);
        if (changes > 0) {
            setAsking({ to, changes });
            return;
        }
        take(to);
    };
    const take = (to) => {
        const lost = replaced(pick, to);
        setDirty((d) => ({ middle: lost.middle ? 0 : d.middle, right: lost.right ? 0 : d.right }));
        setPick(to);
        setAsking(null);
    };
    const mark = (column) => (n) => setDirty((d) => (d[column] === n ? d : { ...d, [column]: n }));

    if (!contour) return null;

    // A form the columns do not hold — adding something — opens over them.
    const form = (f) => {
        if (f.mode === "edit" && f.kind === "group") go({ contour: contour.id, group: f.group.id });
        else if (f.mode === "edit" && f.kind === "project") go({ contour: contour.id, group: f.group.id, project: f.project.id });
        else setAdding(f);
    };
    // Closing the whole window asks about every page with changes on it.
    const leaveAll = () => {
        if (dirty.middle + dirty.right > 0) setAsking({ to: null, changes: dirty.middle + dirty.right });
        else onClose();
    };

    const groupCount = (profiles || []).reduce((n, p) => n + (p.groups || []).length, 0);
    const searching = query.trim() !== "";
    const tree = (profiles || [])
        .map((p) => ({ contour: p, groups: findIn(p.groups, query) }))
        .filter((t) => !searching || t.groups.length > 0);

    return html`
        <div class="dkscrim dkcolscrim">
            <div class="dkcols" role="dialog" aria-modal="true" aria-label="settings of the map">
                <button class="dkclose dkcolsclose" type="button" aria-label="close" onClick=${leaveAll}>${Icon.close()}</button>
                <nav class="dkcoltree" aria-label="contours and groups">
                    <div class="dkcoltreehead">
                        <span>Settings</span>
                    </div>
                    ${groupCount >= FIND_FROM && html`
                        <input class="dktreefind" type="search" placeholder="group or project" aria-label="find a group or a project"
                               value=${query} onInput=${(e) => setQuery(e.target.value)} />
                    `}
                    ${tree.map(({ contour: p, groups }) => html`
                        <div class="dktreecontour" key=${p.id}>
                            <button class="dktreeitem" type="button" aria-pressed=${p.id === pick.contour && !pick.group ? "true" : "false"}
                                    onClick=${() => go({ contour: p.id })}>
                                <span class="dktreename">${p.name}</span>
                                <span class="dknum">${(p.groups || []).length}</span>
                            </button>
                            ${groups.map(({ group: g, projects }) => html`
                                <button class="dktreeitem dktreegroup" type="button" key=${g.id} data-drop=${p.id === contour.id ? String(g.id) : undefined}
                                        aria-pressed=${g.id === pick.group ? "true" : "false"}
                                        onClick=${() => go({ contour: p.id, group: g.id })}>
                                    <span class="dktreename">${g.name}</span>
                                    <span class="dknum">${(g.projects || []).length}</span>
                                </button>
                                ${projects.map((x) => html`
                                    <button class="dktreeitem dktreeproject" type="button" key=${`p${x.id}`}
                                            aria-pressed=${x.id === pick.project ? "true" : "false"}
                                            onClick=${() => go({ contour: p.id, group: g.id, project: x.id })}>
                                        <span class="dktreename">${x.name}</span>
                                    </button>
                                `)}
                            `)}
                        </div>
                    `)}
                    ${searching && tree.length === 0 && html`<p class="dkempty dktreenone">nothing matches the query</p>`}
                    <button class="dktreeadd" type="button" onClick=${() => form({ kind: "profile", mode: "add" })}>
                        <${Icon.plus} />new contour
                    </button>
                </nav>
                <section class="dkcol" aria-label=${group ? `group ${group.name}` : `contour ${contour.name}`}>
                    ${group
                        ? html`<${GroupSettings}
                            key=${`g${group.id}`}
                            group=${group}
                            contour=${contour}
                            profiles=${profiles}
                            disk=${disk}
                            catalog=${catalog}
                            onClose=${() => (dirty.right > 0
                                ? setAsking({ to: { contour: contour.id }, changes: dirty.right })
                                : take({ contour: contour.id }))}
                            onDone=${apply}
                            onRemove=${remove}
                            onForm=${form}
                            onDirty=${mark("middle")}
                        />`
                        : html`<${ContourSettings}
                            key=${`c${contour.id}`}
                            contour=${contour}
                            catalog=${catalog}
                            order=${null}
                            onClose=${() => (dirty.right > 0 ? setAsking({ to: null, changes: dirty.right }) : onClose())}
                            onDone=${apply}
                            onRemove=${remove}
                            onForm=${form}
                            onJournal=${() => go({ contour: contour.id, journal: true })}
                            onDirty=${mark("middle")}
                        />`}
                </section>
                <section class="dkcol" aria-label="project">
                    ${project && html`<${ProjectSettings}
                        key=${`p${project.id}`}
                        project=${project}
                        contour=${contour}
                        group=${group}
                        catalog=${catalog}
                        sessions=${sessions}
                        tools=${html`<${Start} project=${project} exec=${exec} onStarted=${leaveAll} />`}
                        onClose=${() => take({ contour: contour.id, group: group.id })}
                        onDone=${apply}
                        onRemove=${remove}
                        onDirty=${mark("right")}
                    />`}
                    ${!project && pick.journal && html`<${Journal} onClose=${() => take({ contour: contour.id })} onDone=${apply} />`}
                    ${!project && !pick.journal && html`
                        <p class="dkempty dkcolhint">${group
                            ? `pick a project of ${group.name} — ${(group.projects || []).length} ${plural((group.projects || []).length, "project", "projects")} on the shelf`
                            : "pick a group in the tree, or a project on a group"}</p>
                    `}
                </section>
            </div>
            ${adding && html`<${EditLayer}
                form=${adding}
                profiles=${profiles}
                catalog=${catalog}
                disk=${disk}
                accounts=${map.accounts}
                order=${null}
                onClose=${() => setAdding(null)}
                onDone=${apply}
                onRemove=${remove}
            />`}
            <${Sheet} open=${Boolean(asking)} onClose=${() => setAsking(null)} label="changes not saved">
                ${asking && html`
                    <div class="pzleave">
                        <h3>${asking.changes} ${asking.changes === 1 ? "change" : "changes"} not saved</h3>
                        <p class="pfhelp">the page that holds them would close — Save is on its bar</p>
                        <div class="btnrow">
                            <button class="btn" type="button" onClick=${() => setAsking(null)}>Stay</button>
                            <button class="btn primary" type="button"
                                    onClick=${() => (asking.to ? take(asking.to) : onClose())}>Discard</button>
                        </div>
                    </div>
                `}
            <//>
        </div>
    `;
}
