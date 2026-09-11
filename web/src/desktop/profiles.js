// The Profiles section on a wide screen: contours on the left, the tree in the centre.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { plural } from "../format.js";
import { knows, whyNot } from "../exec.js";
import { useAction } from "../actions/gate.js";
import { EditLayer } from "../screens/profiles/forms.js";
import { LooseLayer } from "../screens/profiles/loose.js";
import { hooksWarning } from "../screens/profiles/page.js";
import { authState, dotOf, filterGroups, looseOf } from "../screens/profiles/pick.js";
import { summary } from "../screens/profiles/launch.js";
import { orderOf, useProfileMap } from "../screens/profiles/state.js";

function countsOf(profile) {
    const groups = profile.groups || [];
    const projects = groups.reduce((n, g) => n + (g.projects || []).length, 0);
    return { groups: groups.length, projects };
}

function binFact(profile) {
    if (profile.claudeBin) return { value: "own path", tip: profile.claudeBin };
    return {
        value: "host decides",
        tip: "the path is not set: the launcher will take AACP_CLAUDE, then the wrapper from the delivery, "
            + "then the first claude in PATH — and that one brings the session up in the personal account",
    };
}

function ContourColumn({ profiles, current, onPick, onForm }) {
    const list = profiles || [];
    return html`
        <aside class="dkleft">
            <div class="dkcontour">
                <span class="dkcontourname">contours</span>
                <span class="dkcontournum">${list.length} ${plural(list.length, "contour", "contours")}</span>
            </div>
            <div class="dkscroll">
                ${list.map((profile) => {
                    const auth = authState(profile);
                    const counts = countsOf(profile);
                    const launch = summary(profile.launch);
                    return html`
                        <button
                            key=${profile.id}
                            class=${`dksess${current === profile.name ? " on" : ""}`}
                            type="button"
                            onClick=${() => onPick(profile.name)}
                        >
                            <span class=${`dkdot dk${auth.dot} dkside`}></span>
                            <span class="dksessbody">
                                <span class="dksessmain">
                                    <span class="dkname">${profile.name}</span>
                                    <span class="dknum">${counts.projects}</span>
                                    <span class="dkrowacts" onClick=${(e) => e.stopPropagation()}>
                                        <i
                                            class="dkact"
                                            data-tip="New group"
                                            data-tipside="left"
                                            onClick=${() => onForm({ kind: "group", mode: "add", profile })}
                                        ><${Icon.plus} /></i>
                                        <i
                                            class="dkact"
                                            data-tip="Edit contour"
                                            data-tipside="left"
                                            onClick=${() => onForm({ kind: "profile", mode: "edit", profile })}
                                        ><${Icon.pencil} /></i>
                                    </span>
                                </span>
                                <span class="dksesssub">
                                    <span class="dklast">${launch ? `${auth.text} · ${launch}` : auth.text}</span>
                                    <span class="dkgroup">${counts.groups} ${plural(counts.groups, "group", "groups")}</span>
                                </span>
                            </span>
                        </button>
                    `;
                })}
                ${list.length === 0 && html`<p class="dkempty">there are no contours on the map</p>`}
            </div>
            <button class="dkadd" type="button" onClick=${() => onForm({ kind: "profile", mode: "add" })}>
                <${Icon.plus} /> new contour
            </button>
        </aside>
    `;
}

function ProjectRow({ project, profile, group, gone, exec, onForm, onRemove, onOpened }) {
    const run = useAction();
    const can = knows(exec, "session.open");
    return html`
        <div class=${`dkrow dkitem${gone ? " dkgone" : ""}`}>
            <span class="dkname">${project.name}</span>
            <span class="dkpath" title=${project.path}>${project.path}</span>
            ${gone && html`<span class="dkmiss" data-tip="The directory is not on disk" data-tipside="left">!</span>`}
            <span class="dkacts">
                <i
                    class=${`dkact${can ? "" : " off"}`}
                    data-tip=${can ? `Start a session in ${project.name}` : whyNot(exec, "session.open")}
                    data-tipside="left"
                    onClick=${async () => {
                        if (!can) return;
                        const target = project.session || project.name;
                        const done = await run("session.open", target, { project: project.id });
                        if (done && done.ok && onOpened) onOpened(target);
                    }}
                ><${Icon.play} /></i>
                <i
                    class="dkact"
                    data-tip="Edit project"
                    data-tipside="left"
                    onClick=${() => onForm({ kind: "project", mode: "edit", profile, group, project })}
                ><${Icon.pencil} /></i>
                <i
                    class="dkact danger"
                    data-tip="Take off the map"
                    data-tipside="left"
                    onClick=${() => onRemove({ kind: "project", profile, group, project })}
                ><${Icon.close} /></i>
            </span>
        </div>
    `;
}

// GroupRow renders a shelf with its projects, expanded by a tap on the row.
export function GroupRow({ profile, group, expanded, onToggle, onForm, onRemove, gone, exec, onOpened }) {
    const projects = group.projects || [];
    return html`
        <div>
            <button class="dkrow dkproj" type="button" onClick=${onToggle}>
                <span class=${`dkchev${expanded ? " down" : ""}`}><${Icon.chevron} /></span>
                <span class="dkdotcell">
                    ${dotOf(group, gone) !== "ok" && html`<span class=${`dkdot dk${dotOf(group, gone)}`}></span>`}
                </span>
                <span class="dkname">${group.name}</span>
                <span class="dknum">${projects.length}</span>
                <span class="dkacts" onClick=${(e) => e.stopPropagation()}>
                    <i
                        class="dkact"
                        data-tip="New project"
                        data-tipside="left"
                        onClick=${() => onForm({ kind: "project", mode: "add", profile, group })}
                    ><${Icon.plus} /></i>
                    <i
                        class="dkact"
                        data-tip="Edit group"
                        data-tipside="left"
                        onClick=${() => onForm({ kind: "group", mode: "edit", profile, group })}
                    ><${Icon.pencil} /></i>
                    <i
                        class="dkact danger"
                        data-tip="Delete group"
                        data-tipside="left"
                        onClick=${() => onRemove({ kind: "group", profile, group })}
                    ><${Icon.close} /></i>
                </span>
            </button>
            ${expanded && projects.length === 0 && html`
                <p class="dkempty dkunder">the group has no projects — there is nothing to launch from it</p>
            `}
            ${expanded && projects.map((project) => html`
                <${ProjectRow}
                    key=${project.id}
                    project=${project}
                    profile=${profile}
                    group=${group}
                    gone=${Boolean(gone && gone.has(project.id))}
                    exec=${exec}
                    onForm=${onForm}
                    onRemove=${onRemove}
                    onOpened=${onOpened}
                />
            `)}
        </div>
    `;
}

export function DeskProfiles({ exec, onDone }) {
    const {
        profiles, catalog, disk, error, gone,
        current, pick, profile,
        open, toggle, form, setForm, loose, setLoose,
        apply, remove, reorder,
    } = useProfileMap();
    const [query, setQuery] = useState("");

    const editing = form && html`<${EditLayer}
        form=${form}
        profiles=${profiles}
        catalog=${catalog}
        disk=${disk}
        order=${orderOf(profiles, form, reorder)}
        onClose=${() => setForm(null)}
        onDone=${apply}
        onRemove=${remove}
    />`;

    const center = () => {
        if (loose && profile) {
            return html`<div class="dkpage"><${LooseLayer}
                profile=${profile}
                disk=${disk}
                profiles=${profiles}
                onBack=${() => setLoose(false)}
                onDone=${apply}
            /></div>`;
        }
        if (error) return html`<p class="dkempty dkfail">${error}</p>`;
        if (profiles === null) return html`<p class="dkempty">loading…</p>`;
        if (!profile) {
            return html`
                <p class="dkempty">There are no profiles yet. A profile is a contour with its own token and its own projects.</p>
            `;
        }

        const groups = profile.groups || [];
        const total = groups.reduce((n, g) => n + (g.projects || []).length, 0);
        const shown = filterGroups(groups, query);
        const warn = hooksWarning(profile);
        const auth = authState(profile);
        const launch = summary(profile.launch);
        const bin = binFact(profile);
        const found = looseOf(disk, profiles, profile.name).length;

        return html`
            <div class="dkhead">
                <div class="dkheadtop">
                    <span class="dkheadname">${profile.name}</span>
                    <span class="dkheadpath">${launch ? `${auth.text} · ${launch}` : auth.text}</span>
                </div>
                <div class="dkheadbot">
                    <span class="dkfact"><b>${groups.length}</b><span>${plural(groups.length, "group", "groups")}</span></span>
                    <span class="dkfact"><b>${total}</b><span>${plural(total, "project", "projects")}</span></span>
                    <span class="dkfact" data-tip=${bin.tip}><b>${bin.value}</b><span>what to launch with</span></span>
                    ${total > 1 && html`
                        <input
                            class="dkfind"
                            type="search"
                            spellcheck="false"
                            placeholder="project or path"
                            value=${query}
                            onInput=${(e) => setQuery(e.target.value)}
                        />
                    `}
                </div>
                ${warn && html`<p class="dkwarnline">${warn}</p>`}
            </div>

            <div class="dkscroll">
                ${groups.length === 0 && html`
                    <p class="dkempty">There are no groups yet. A group is a shelf to put projects on.</p>
                `}
                ${groups.length > 0 && shown.length === 0 && html`
                    <p class="dkempty">nothing found — neither in the names nor in the paths</p>
                `}
                ${shown.map((group) => html`
                    <${GroupRow}
                        key=${group.id}
                        profile=${profile}
                        group=${group}
                        expanded=${query.trim() !== "" || open.has(`g:${group.id}`)}
                        onToggle=${() => toggle(`g:${group.id}`)}
                        onForm=${setForm}
                        onRemove=${remove}
                        gone=${gone}
                        exec=${exec}
                        onOpened=${onDone}
                    />
                `)}
                ${found > 0 && html`
                    <button class="dkadd dkloose" type="button" onClick=${() => setLoose(true)}>
                        found on disk but not on the map
                        <span class="dknum">${found}</span>
                    </button>
                `}
            </div>
        `;
    };

    return html`
        <${ContourColumn} profiles=${profiles} current=${current} onPick=${pick} onForm=${setForm} />
        <section class="dkcenter">
            ${center()}
            ${editing}
        </section>
    `;
}
