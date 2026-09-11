// Sorting out what was found on disk: directories that are not on the map.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { plural } from "../../format.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { useAction } from "../../actions/gate.js";
import { grouped, marks, openFolders } from "./disk.js";
import { guessGroup, hiddenOf, looseOf } from "./pick.js";

export function LooseLayer({ profile, disk, profiles, onBack, onDone }) {
    useBackClose(true, onBack);
    const run = useAction();
    const dirs = looseOf(disk, profiles, profile.name);
    const roots = (disk && disk.roots) || [];
    const groups = profile.groups || [];
    const count = `${dirs.length} ${plural(dirs.length, "directory", "directories")}`;
    const [open, setOpen] = useState(() => openFolders({ projects: allProjects(profile) }, profile, roots));
    const [picked, setPicked] = useState({});
    const [busy, setBusy] = useState("");
    const [showHidden, setShowHidden] = useState(false);
    const hidden = hiddenOf(disk, profiles, profile.name);

    const toggle = (folder) => setOpen((prev) => {
        const next = new Set(prev);
        if (next.has(folder)) next.delete(folder);
        else next.add(folder);
        return next;
    });

    const hide = async (path) => {
        setBusy(path);
        const result = await run("disk.hide", path);
        setBusy("");
        if (result && result.ok) onDone(result.data);
    };

    const back = async (path) => {
        setBusy(path);
        const result = await run("disk.show", path);
        setBusy("");
        if (result && result.ok) onDone(result.data);
    };

    const take = async (dir, groupId) => {
        const group = groups.find((g) => String(g.id) === String(groupId));
        if (!group) return;
        const name = dir.path.split("/").filter(Boolean).pop() || dir.path;
        setBusy(dir.path);
        const result = await run("project.add", name, {
            groupId: group.id,
            group: group.name,
            profile: profile.name,
            fields: { name, path: dir.path, session: "", launch: {} },
        });
        setBusy("");
        if (result && result.ok) onDone(result.data);
    };

    return html`
        <${BackHead} onBack=${onBack} label="to the profile map">
            <h2>Not on the map</h2>
            <span class="where">${count} turned up by walking the disk: the contour comes from the prefix, the group is yours to pick</span>
        <//>

        ${groups.length === 0 && html`
            <p class="hint warn">the contour has no groups — there is nowhere to put a project, start with a shelf</p>
        `}

        ${dirs.length === 0
            ? html`<p class="empty">Everything found on disk is already on the map.</p>`
            : grouped(dirs, roots).map(({ folder, items }) => html`
                <section class="card pfdiskfolder" key=${folder} data-open=${open.has(folder) ? "1" : "0"}>
                    <button class="pfdiskhead" type="button" onClick=${() => toggle(folder)}>
                        <span class="pfdiskname">${folder}</span>
                        <span class="count">
                            ${items.length} ${plural(items.length, "directory", "directories")}
                        </span>
                    </button>
                    ${open.has(folder) && items.map((dir) => {
                        const choice = picked[dir.path] !== undefined
                            ? picked[dir.path]
                            : guessGroup(profile, dir.path, roots);
                        return html`
                            <div class="pfloosepath" key=${dir.path}>
                                <span class="pfdiskrel">${dir.rel}</span>
                                ${marks(dir) && html`<span class="pfdiskmeta">${marks(dir)}</span>`}
                                <div class="pfloosepick">
                                    <select
                                        class="search pfpick"
                                        value=${choice}
                                        onChange=${(e) => setPicked({ ...picked, [dir.path]: e.target.value })}
                                    >
                                        <option value="">into a group…</option>
                                        ${groups.map((group) => html`
                                            <option key=${group.id} value=${String(group.id)}>${group.name}</option>
                                        `)}
                                    </select>
                                    <button
                                        class="btn"
                                        type="button"
                                        disabled=${!choice || busy === dir.path}
                                        onClick=${() => take(dir, choice)}
                                    >add</button>
                                    <button
                                        class="pfhide"
                                        type="button"
                                        aria-label=${`hide ${dir.rel}`}
                                        disabled=${busy === dir.path}
                                        onClick=${() => hide(dir.path)}
                                    >hide</button>
                                </div>
                            </div>
                        `;
                    })}
                </section>
            `)}

        ${hidden.length > 0 && html`
            <button class="pfloose" type="button" onClick=${() => setShowHidden(!showHidden)}>
                <span class="pfloosetext">${showHidden ? "hide the list of hidden" : "hidden from the queue"}</span>
                <span class="count">${hidden.length}</span>
                <span class="chev">${Icon.chevron()}</span>
            </button>
        `}

        ${showHidden && hidden.length > 0 && html`
            <section class="card">
                ${hidden.map((path) => html`
                    <div class="pfloosepath" key=${path}>
                        <span class="pfdiskrel">${path}</span>
                        <div class="pfloosepick">
                            <button
                                class="btn"
                                type="button"
                                disabled=${busy === path}
                                onClick=${() => back(path)}
                            >restore</button>
                        </div>
                    </div>
                `)}
            </section>
        `}
    `;
}

function allProjects(profile) {
    const out = [];
    for (const group of (profile.groups || [])) {
        for (const project of (group.projects || [])) out.push(project);
    }
    return out;
}
