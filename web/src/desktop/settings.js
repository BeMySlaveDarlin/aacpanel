// The settings of the map on a wide screen: three columns over the panel —
// the tree of contours and their groups, the page of the contour or the group
// picked in it, and the page of a project picked on the group (or the journal
// of the map). Each page keeps its own draft and bar; a pick in the tree that
// would replace a page with changes on it asks first. A project dragged from
// the group's page onto a group of the tree moves there. A page closed by its
// own arrow has asked about its draft itself; the frame asks only for the
// pages a pick or a close of another one would take with it.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { Sheet } from "../ui/sheet.js";
import { plural } from "../format.js";
import { ContourSettings } from "../screens/profiles/contour.js";
import { GroupSettings } from "../screens/profiles/shelf.js";
import { ProjectSettings } from "../screens/profiles/settings.js";
import { Journal } from "../screens/profiles/journal.js";
import { EditLayer } from "../screens/profiles/forms.js";

// pickOf returns what a form of the map opens in the columns.
export function pickOf(form) {
    if (!form || form.mode !== "edit") return null;
    if (form.kind === "profile") return { contour: form.profile.id };
    if (form.kind === "group") return { contour: form.profile.id, group: form.group.id };
    if (form.kind === "project") return { contour: form.profile.id, group: form.group.id, project: form.project.id };
    return null;
}

// replaced says which column a new pick throws away: the middle one when the
// contour or the group changes, the right one when only the project does.
export function replaced(from, to) {
    const middle = from.contour !== to.contour || (from.group || 0) !== (to.group || 0);
    const right = middle || (from.project || 0) !== (to.project || 0) || Boolean(from.journal) !== Boolean(to.journal);
    return { middle, right };
}

export function SettingsColumns({ map, start, sessions, onClose }) {
    const { profiles, catalog, disk, apply, remove } = map;
    const [pick, setPick] = useState(start);
    const [dirty, setDirty] = useState({ middle: 0, right: 0 });
    const [asking, setAsking] = useState(null);
    const [adding, setAdding] = useState(null);

    const contour = (profiles || []).find((p) => p.id === pick.contour) || null;
    const group = contour && pick.group ? (contour.groups || []).find((g) => g.id === pick.group) || null : null;
    const project = group && pick.project ? (group.projects || []).find((p) => p.id === pick.project) || null : null;

    useEffect(() => {
        if (!contour) onClose();
    }, [contour]);

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

    return html`
        <div class="dkscrim dkcolscrim">
            <div class="dkcols" role="dialog" aria-modal="true" aria-label="settings of the map">
                <nav class="dkcoltree" aria-label="contours and groups">
                    <div class="dkcoltreehead">
                        <span>Settings</span>
                        <button class="dkclose" type="button" aria-label="close" onClick=${() => {
                            if (dirty.middle + dirty.right > 0) setAsking({ to: null, changes: dirty.middle + dirty.right });
                            else onClose();
                        }}>${Icon.close()}</button>
                    </div>
                    ${(profiles || []).map((p) => html`
                        <div class="dktreecontour" key=${p.id}>
                            <button class="dktreeitem" type="button" aria-pressed=${p.id === pick.contour && !pick.group ? "true" : "false"}
                                    onClick=${() => go({ contour: p.id })}>
                                <span class="dktreename">${p.name}</span>
                                <span class="dknum">${(p.groups || []).length}</span>
                            </button>
                            ${(p.groups || []).map((g) => html`
                                <button class="dktreeitem dktreegroup" type="button" key=${g.id} data-drop=${p.id === contour.id ? String(g.id) : undefined}
                                        aria-pressed=${g.id === pick.group ? "true" : "false"}
                                        onClick=${() => go({ contour: p.id, group: g.id })}>
                                    <span class="dktreename">${g.name}</span>
                                    <span class="dknum">${(g.projects || []).length}</span>
                                </button>
                            `)}
                        </div>
                    `)}
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
