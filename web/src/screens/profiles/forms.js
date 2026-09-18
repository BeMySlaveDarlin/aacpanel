// The edit layer: contour, group, project — and the same forms for adding them.
import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { useWide } from "../../ui/wide.js";
import { useAction } from "../../actions/gate.js";
import { LaunchFields, clean } from "./launch.js";
import { DiskPicker, diskNote } from "./disk.js";
import { PERSONAL } from "../../contour.js";

const TITLES = {
    "profile.add": ["New profile", "its own token, its own config directory, its own projects"],
    "profile.edit": ["Profile", "the change applies to the sessions launched next"],
    "group.add": ["New group", "a shelf inside the profile: projects are laid out on it"],
    "group.edit": ["Group", "the name shows on the map and in the launch list"],
    "project.add": ["New project", "something the panel can bring up as a console"],
    "project.edit": ["Project", "the change applies to the sessions launched next"],
};

// EditLayer renders one layer for all six cases: there is always one open form.
export function EditLayer({ form, profiles, catalog, disk, order, onClose, onDone, onRemove }) {
    useBackClose(true, onClose);
    const wide = useWide();
    const [title, sub] = TITLES[`${form.kind}.${form.mode}`] || ["", ""];

    useEffect(() => {
        if (!wide) return undefined;
        const onKey = (event) => {
            if (event.key === "Escape") onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [wide, onClose]);

    const body = html`
        <${Body} key=${keyOf(form)} form=${form} profiles=${profiles} catalog=${catalog} disk=${disk}
                 onClose=${onClose} onDone=${onDone} />
        <${Tail} form=${form} order=${order} onRemove=${onRemove} onClose=${onClose} />
    `;

    if (wide) {
        return html`
            <div class="dkscrim" onClick=${onClose}>
                <div
                    class="dkmodal"
                    role="dialog"
                    aria-modal="true"
                    aria-label=${title}
                    onClick=${(event) => event.stopPropagation()}
                >
                    <div class="dkmodalhead">
                        <span class="dkmodaltitle">${title}</span>
                        <span class="dkmodalsub">${sub}</span>
                        <button class="dkclose" type="button" aria-label="close" onClick=${onClose}>
                            ${Icon.close()}
                        </button>
                    </div>
                    <div class="dkmodalbody">${body}</div>
                </div>
            </div>
        `;
    }

    return html`
        <${BackHead} onBack=${onClose} label="to the profile map">
            <h2>${title}</h2>
            <span class="where">${sub}</span>
        <//>
        ${body}
    `;
}

function keyOf(form) {
    const target = form.project || form.group || form.profile;
    return `${form.kind}.${form.mode}.${(target && target.id) || "new"}`;
}

function Body({ form, profiles, catalog, disk, onClose, onDone }) {
    if (form.kind === "profile") {
        return html`<${ProfileForm} form=${form} catalog=${catalog} onClose=${onClose} onDone=${onDone} />`;
    }
    if (form.kind === "group") {
        return html`<${GroupForm} form=${form} profiles=${profiles} onClose=${onClose} onDone=${onDone} />`;
    }
    return html`<${ProjectForm} form=${form} catalog=${catalog} disk=${disk}
                                onClose=${onClose} onDone=${onDone} />`;
}

const DANGER = {
    profile: "Delete profile",
    group: "Delete group",
    project: "Delete project",
};

function Tail({ form, order, onRemove, onClose }) {
    if (form.mode !== "edit") return null;

    const spec = {
        kind: form.kind,
        profile: form.profile,
        group: form.group,
        project: form.project,
    };

    return html`
        ${order && html`
            <div class="pfsub">place in the list</div>
            <div class="pforder">
                <span class="pfhelp">${order.index + 1} of ${order.total}</span>
                <button
                    class="btn"
                    type="button"
                    disabled=${order.index === 0}
                    onClick=${() => order.move("up")}
                >Up</button>
                <button
                    class="btn"
                    type="button"
                    disabled=${order.index === order.total - 1}
                    onClick=${() => order.move("down")}
                >Down</button>
            </div>
        `}

        <button
            class="pfdanger"
            type="button"
            onClick=${async () => {
                const result = await onRemove(spec);
                if (result && result.ok) onClose();
            }}
        >${DANGER[form.kind]}</button>
    `;
}

function Buttons({ busy, problem, ok, onClose, onSave }) {
    return html`
        ${problem && html`<p class="hint warn pfproblem">${problem}</p>`}
        <div class="btnrow">
            <button class="btn" type="button" onClick=${onClose} disabled=${busy}>Cancel</button>
            <button class="btn primary" type="button" onClick=${onSave} disabled=${busy}>${ok}</button>
        </div>
    `;
}

function useSave(onClose, onDone) {
    const run = useAction();
    const [busy, setBusy] = useState(false);
    const [problem, setProblem] = useState("");
    const [nameProblem, setNameProblem] = useState("");

    const save = async (id, target, params, check) => {
        const bad = check();
        if (bad) {
            setProblem(bad);
            setNameProblem("");
            return;
        }
        setProblem("");
        setNameProblem("");
        setBusy(true);
        const result = await run(id, target, params);
        setBusy(false);
        if (!result.ok) {
            if (result.status === 409) setNameProblem(result.error);
            return;
        }
        onDone(result.data);
        onClose();
    };

    return { busy, problem, nameProblem, clearNameProblem: () => setNameProblem(""), save };
}

function absolute(path) {
    return path.startsWith("/") && !path.split("/").includes("..");
}

function ProfileForm({ form, catalog, onClose, onDone }) {
    const editing = form.mode === "edit";
    const was = form.profile || {};
    const [name, setName] = useState(was.name || "");
    const [dir, setDir] = useState(was.configDir || "");
    const [prefix, setPrefix] = useState(was.prefix || "");
    const [bin, setBin] = useState(was.claudeBin || "");
    const [launch, setLaunch] = useState(was.launch || {});
    const { busy, problem, nameProblem, clearNameProblem, save } = useSave(onClose, onDone);

    const id = editing ? "profile.edit" : "profile.add";
    const locked = editing && was.name === PERSONAL;
    const binNow = bin.trim();
    const binWas = (was.claudeBin || "").trim();
    const fields = {
        name: name.trim(),
        configDir: dir.trim(),
        prefix: prefix.trim(),
        ...((editing ? binNow !== binWas : binNow !== "") ? { claudeBin: binNow } : {}),
        launch: clean(launch),
    };
    const check = () => {
        if (!fields.name) return "Without a name a profile cannot be told from its neighbour.";
        if (!fields.configDir) return "The config directory is required: the profile token lives in it.";
        if (!absolute(fields.configDir)) return "The config directory has to be an absolute path.";
        if (fields.prefix && !absolute(fields.prefix)) return "The path prefix has to be an absolute path.";
        if (binNow && !absolute(binNow)) {
            return "The path to claude has to be absolute: a relative one is picked by PATH, and PATH differs between the unit and a person.";
        }
        return "";
    };

    return html`
        <label class="pffield">
            <span class="pflabel">Name</span>
            <input class="search" spellcheck="false" placeholder=${PERSONAL} disabled=${locked}
                   value=${name} onInput=${(e) => { setName(e.target.value); clearNameProblem(); }} />
            ${nameProblem
                ? html`<span class="pfhelp warn">${nameProblem}</span>`
                : locked
                    ? html`<span class="pfhelp">the personal contour is called ${PERSONAL} and is not
                        renamed: by that name the panel, the collector and the installer find its config directory</span>`
                    : html`<span class="pfhelp">the name is unique across the machine: it is how the profile is recognised</span>`}
        </label>

        <label class="pffield">
            <span class="pflabel">Config directory</span>
            <input class="search" spellcheck="false" placeholder="~/.claude"
                   value=${dir} onInput=${(e) => setDir(e.target.value)} />
            <span class="pfhelp">the token, the settings and the conversation archive of this contour live there</span>
        </label>

        <label class="pffield">
            <span class="pflabel">Path prefix</span>
            <input class="search" spellcheck="false" placeholder="/srv/proj"
                   value=${prefix} onInput=${(e) => setPrefix(e.target.value)} />
            <span class="pfhelp">the wrapper picks the profile by it for the directory it was called from</span>
        </label>

        <label class="pffield">
            <span class="pflabel">What to launch with</span>
            <input class="search" spellcheck="false" placeholder="/usr/local/bin/claude"
                   value=${bin} onInput=${(e) => setBin(e.target.value)} />
            ${binNow
                ? html`<span class="pfhelp">the host checks the file at launch: the service lives in a container
                    and does not see the files of the machine</span>`
                : html`<span class="pfhelp">empty — the launcher looks for the path itself: AACP_CLAUDE first, then
                    the wrapper from the delivery, then the first claude in PATH; the last one brings the session up in
                    the personal account, whatever the contour is</span>`}
        </label>

        <div class="pfsub">default launch parameters</div>
        <${LaunchFields} value=${launch} onChange=${setLaunch} catalog=${catalog} />

        <${Buttons}
            busy=${busy}
            problem=${problem}
            ok=${editing ? "Save" : "Add"}
            onClose=${onClose}
            onSave=${() => save(id, fields.name, editing ? { id: was.id, fields } : { fields }, check)}
        />
    `;
}

function GroupForm({ form, profiles, onClose, onDone }) {
    const editing = form.mode === "edit";
    const was = form.group || {};
    const [name, setName] = useState(was.name || "");
    const [profile, setProfile] = useState(String(form.profile.id));
    const { busy, problem, nameProblem, clearNameProblem, save } = useSave(onClose, onDone);

    const id = editing ? "group.edit" : "group.add";
    const moved = editing && profile !== String(form.profile.id);
    const toProfile = moved
        ? ((profiles || []).find((p) => String(p.id) === profile) || {}).name || ""
        : "";
    const fields = {
        name: name.trim(),
        ...(moved ? { profileId: Number(profile), moveProfile: true } : {}),
    };
    const check = () => (fields.name ? "" : "A group has to have a name — that is what it is called on the map.");

    return html`
        <p class="hint">in profile “${form.profile.name}”</p>

        <label class="pffield">
            <span class="pflabel">Group name</span>
            <input class="search" placeholder="Beta"
                   value=${name} onInput=${(e) => { setName(e.target.value); clearNameProblem(); }} />
            ${nameProblem && html`<span class="pfhelp warn">${nameProblem}</span>`}
        </label>

        ${editing && (profiles || []).length > 1 && html`
            <label class="pffield">
                <span class="pflabel">Contour</span>
                <select class="search" value=${profile} onChange=${(e) => setProfile(e.target.value)}>
                    ${(profiles || []).map((p) => html`
                        <option key=${p.id} value=${String(p.id)}>${p.name}</option>
                    `)}
                </select>
                ${toProfile
                    ? html`<span class="pfhelp warn">the group moves to “${toProfile}” together with its
                        projects: their sessions will go by its token and its subscription</span>`
                    : html`<span class="pfhelp">changing the contour means taking every project of the
                        group there too; their account becomes a different one</span>`}
            </label>
        `}

        <${Buttons}
            busy=${busy}
            problem=${problem}
            ok=${editing ? "Save" : "Add"}
            onClose=${onClose}
            onSave=${() => save(
                id,
                fields.name,
                editing
                    ? {
                        id: was.id,
                        fields,
                        toProfile,
                        fromProfile: form.profile.name,
                        projects: (was.projects || []).length,
                    }
                    : { profileId: form.profile.id, profile: form.profile.name, fields },
                check,
            )}
        />
    `;
}

function ProjectForm({ form, catalog, disk, onClose, onDone }) {
    const editing = form.mode === "edit";
    const was = form.project || {};
    const [name, setName] = useState(was.name || "");
    const [path, setPath] = useState(was.path || "");
    const [session, setSession] = useState(was.session || "");
    const [base, setBase] = useState(was.base || "");
    const [launch, setLaunch] = useState(was.launch || {});
    const [group, setGroup] = useState(String((form.group && form.group.id) || ""));
    const { busy, problem, nameProblem, clearNameProblem, save } = useSave(onClose, onDone);
    const [picking, setPicking] = useState(false);
    const [picked, setPicked] = useState("");
    const diskReady = Boolean(disk && disk.state === "ok" && (disk.dirs || []).length > 0);
    const pick = (dir) => {
        setPath(dir.path);
        const base = dir.path.split("/").filter(Boolean).pop() || "";
        if (name.trim() === "" || name === picked) {
            setName(base);
            clearNameProblem();
        }
        setPicked(base);
        setPicking(false);
    };

    const id = editing ? "project.edit" : "project.add";
    const groups = (form.profile.groups || []).filter((g) => g && g.id);
    const moved = editing && group !== String(was.groupId || (form.group && form.group.id) || "");
    const fields = {
        name: name.trim(),
        path: path.trim(),
        session: session.trim(),
        base: base.trim(),
        ...(editing && group ? { groupId: Number(group) } : {}),
        launch: clean(launch),
    };
    const check = () => {
        if (!fields.name) return "Without a caption the project cannot be found in the launch list.";
        if (!fields.path) return "The path is required: the session is brought up from it.";
        if (!absolute(fields.path)) return "The path has to be absolute, without “..” — a relative one goes somewhere else.";
        return "";
    };

    return html`
        <p class="hint">${form.profile.name} · ${form.group.name}</p>

        <label class="pffield">
            <span class="pflabel">Caption</span>
            <input class="search" placeholder="aacpanel"
                   value=${name} onInput=${(e) => { setName(e.target.value); clearNameProblem(); }} />
            ${nameProblem
                ? html`<span class="pfhelp warn">${nameProblem}</span>`
                : html`<span class="pfhelp">how the project is called on the map and in the launch list</span>`}
        </label>

        <label class="pffield">
            <span class="pflabel">Path</span>
            <input class="search" spellcheck="false" placeholder="/srv/proj/Beta/panel"
                   value=${path} onInput=${(e) => setPath(e.target.value)} />
            <span class="pfhelp">absolute; the host will check that it is inside the allowed roots</span>
        </label>

        <div class="btnrow pfdiskrow">
            <button class="btn" type="button" disabled=${!diskReady}
                    onClick=${() => setPicking(!picking)}>${picking ? "Hide the list" : "Pick a project"}</button>
            <span class="pfhelp">${diskNote(disk)}</span>
        </div>
        ${picking && diskReady && html`
            <${DiskPicker} disk=${disk} group=${form.group} profile=${form.profile} onPick=${pick} />
        `}

        ${editing && groups.length > 1 && html`
            <label class="pffield">
                <span class="pflabel">Group</span>
                <select class="search" value=${group} onChange=${(e) => setGroup(e.target.value)}>
                    ${groups.map((g) => html`
                        <option key=${g.id} value=${String(g.id)}>${g.name}</option>
                    `)}
                </select>
                <span class="pfhelp">changing the group is the move itself; shelves of another
                    contour are not offered here, a group moves there whole</span>
            </label>
        `}

        <label class="pffield">
            <span class="pflabel">Session name</span>
            <input class="search" spellcheck="false" placeholder="after the directory name"
                   value=${session} onInput=${(e) => setSession(e.target.value)} />
            <span class="pfhelp">empty — the launcher names it after the directory</span>
        </label>

        <div class="pfsub">git</div>
        <label class="pffield">
            <span class="pflabel">Base branch</span>
            <input class="search" spellcheck="false" placeholder="main"
                   value=${base} onInput=${(e) => setBase(e.target.value)} />
            <span class="pfhelp">what a review of this branch is measured against; empty — main,
                and the screen says so rather than writing it in for you</span>
        </label>

        <div class="pfsub">launch parameters</div>
        <${LaunchFields} value=${launch} onChange=${setLaunch} inherited=${form.profile.launch}
                        catalog=${catalog} />

        <${Buttons}
            busy=${busy}
            problem=${problem}
            ok=${editing ? "Save" : "Add"}
            onClose=${onClose}
            onSave=${() => save(
                id,
                fields.name,
                editing
                    ? { id: was.id, fields, moveTo: moved ? nameOfGroup(groups, group) : "" }
                    : { groupId: form.group.id, group: form.group.name, profile: form.profile.name, fields },
                check,
            )}
        />
    `;
}

function nameOfGroup(groups, id) {
    const own = groups.find((g) => String(g.id) === String(id));
    return own ? own.name : "";
}
