// The page of a group. A group is a shelf, not a level of the launch: it has
// a name, a contour, its projects in their order and what they set otherwise
// than the contour; what is said for all of them is written into each; the
// directories lying next to them on disk are offered to put on it; only an
// empty shelf is deleted, and a full one first moves its projects onto another.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";
import { ownLabel } from "../../ui/own.js";
import { useAction } from "../../actions/gate.js";
import { plural } from "../../format.js";
import { count, field, fieldOf, label, revert, touched, valueOf } from "./draft.js";
import { modelRows } from "./controls.js";
import { useSchema } from "./schema.js";
import { marks } from "./disk.js";
import { Bar, DragRows, Layer, LeaveSheet, useDraft } from "./kit.js";

// The button says what the confirmation sheet will say: group.remove.
const DELETE = "Delete group";

// What can be said for a whole shelf: a choice, not a text a project owns.
const FOR_ALL = ["transport", "model", "effort", "permissionMode", "remoteControl", "contextCap", "autoRestart"];

// deviations returns what a project sets otherwise than its contour gives:
// what it inherits is the contour's by definition, so a value that differs is
// one it sets itself.
export function deviations(project, contour) {
    return (project.effective || [])
        .filter((v) => JSON.stringify(v.value) !== JSON.stringify(valueOf(contour.effective, v.key).value));
}

// summary counts, over the shelf, the projects setting each such value.
export function summary(projects, contour) {
    const tally = new Map();
    for (const p of projects) {
        for (const v of deviations(p, contour)) {
            const word = ownLabel(v);
            tally.set(word, (tally.get(word) || 0) + 1);
        }
    }
    return [...tally].map(([word, n]) => `${word} ${n} of ${projects.length}`);
}

function parent(path) {
    return String(path || "").replace(/\/+$/, "").split("/").slice(0, -1).join("/");
}

// nearby returns the directories found on disk next to the shelf's projects —
// in the same folders — those claude was run in first.
export function nearby(disk, projects) {
    if (!disk || disk.state !== "ok") return [];
    const folders = new Set(projects.map((p) => parent(p.path)).filter(Boolean));
    return (disk.dirs || [])
        .filter((d) => folders.has(parent(d.path)))
        .sort((a, b) => Number(Boolean(b.claude)) - Number(Boolean(a.claude)));
}

// contourMove says what moving the shelf to another contour means, and
// whether this machine allows it at all: where the router's registry picks the
// account by directory, the map moving does not move a single session.
export function contourMove(group, from, to, routed) {
    const projects = group.projects || [];
    const n = `${projects.length} ${plural(projects.length, "project", "projects")}`;
    if (routed) {
        const dirs = [...new Set(projects.map((p) => parent(p.path)))].join(", ");
        return {
            said: `the directory decides the account here: these ${n}${dirs ? ` in ${dirs}` : ""} stay ${from.name}`,
            holds: true,
        };
    }
    const rc = projects.filter((p) => valueOf(p.effective, "remoteControl").value === true).length;
    return {
        said: `${n} start under ${to.name}: its token, limits, archive`
            + (rc > 0 && to.auth === "token" ? `. ${rc} of them have Remote Control on — ${to.name} has no bridge` : ""),
        holds: false,
    };
}

// ForAll is the sheet that says one value for every project of the shelf.
function ForAll({ open, schema, catalog, projects, onClose, onSay }) {
    const [key, setKey] = useState("");
    const params = (schema.params || []).filter((p) => FOR_ALL.includes(p.key));
    const param = params.find((p) => p.key === key);
    const options = !param ? []
        : param.kind === "bool" ? [{ value: true, label: "On" }, { value: false, label: "Off" }]
            : param.kind === "model" ? modelRows(catalog).map((r) => ({ value: r.value, label: r.name }))
                : param.kind === "int" ? [70, 80, 90].map((n) => ({ value: n, label: `${n}%` }))
                    : param.options || [];
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="set for all projects">
            <div class="pzsheet">
                <h3>Set for all ${projects.length} ${plural(projects.length, "project", "projects")}</h3>
                <p class="pzhelp">the value is written into each project of the shelf; a project moved off it keeps it</p>
                <div class="pzopts" role="group" aria-label="parameter">
                    ${params.map((p) => html`
                        <button key=${p.key} class="pzopt" type="button" aria-pressed=${key === p.key ? "true" : "false"}
                                onClick=${() => setKey(p.key)}>${p.label}</button>
                    `)}
                </div>
                ${param && html`
                    <div class="pzopts" role="group" aria-label=${param.label}>
                        ${options.map((o) => html`
                            <button key=${String(o.value)} class="pzopt" type="button"
                                    onClick=${() => onSay(param, o.value)}>${o.label}</button>
                        `)}
                        <button class="pzopt" type="button" onClick=${() => onSay(param, null)}>follow the contour</button>
                    </div>
                `}
            </div>
        <//>
    `;
}

export function GroupSettings({ group, contour, profiles, disk, catalog, onClose, onDone, onRemove, onForm }) {
    const { schema, error } = useSchema();
    const run = useAction();
    const [busy, setBusy] = useState(false);
    const [conflict, setConflict] = useState("");
    const [forAll, setForAll] = useState(false);
    const { draft, setDraft, leaving, setLeaving, leave, hold } = useDraft(onClose, forAll);
    useBackClose(true, onClose, hold);
    const changes = count(draft);
    const projects = group.projects || [];
    const baseOrder = projects.map((p) => p.id);
    const order = fieldOf(draft, { order: baseOrder }, "order") || baseOrder;
    const contours = profiles || [];
    const routed = contours.some((p) => p.route);
    const contourNow = fieldOf(draft, { profileId: contour.id }, "profileId") ?? contour.id;
    const target = contours.find((p) => p.id === contourNow) || contour;
    const move = contourNow !== contour.id ? contourMove(group, contour, target, routed) : null;

    const setField = (name, value, base = group) => {
        setConflict("");
        setDraft((d) => field(d, base, name, value));
    };

    const name = String(fieldOf(draft, group, "name") || "").trim();
    let problem = null;
    if (conflict) problem = { text: conflict, exit: null };
    else if (!name) problem = { text: "a group has to have a name — that is what it is called on the map", exit: touched(draft, "name") ? { text: "Undo the change", run: (d) => revert(d, "name") } : null };
    else if (move && move.holds) problem = { text: move.said, exit: { text: `Back to ${contour.name}`, run: (d) => revert(d, "profileId") } };

    const save = async () => {
        if (problem || count(draft) === 0) return false;
        setBusy(true);
        let result = { ok: true, data: null };
        if (touched(draft, "name") || touched(draft, "profileId")) {
            const fields = { name };
            if (move) Object.assign(fields, { profileId: contourNow, moveProfile: true });
            result = await run(move ? "group.edit" : "group.save", name, move
                ? { id: group.id, fields, toProfile: target.name, fromProfile: contour.name, projects: projects.length }
                : { id: group.id, fields });
        }
        if (result && result.ok && touched(draft, "order")) {
            result = await run("project.reorder", name, { groupId: group.id, ids: order });
        }
        setBusy(false);
        if (!result || !result.ok) {
            if (result && result.status === 409) setConflict(result.error);
            return false;
        }
        onDone(result.data);
        setDraft({ fields: {}, launch: {} });
        return true;
    };

    const say = async (param, value) => {
        setForAll(false);
        const result = await run("group.set", group.name, {
            id: group.id, key: param.key, value, n: projects.length,
            what: value === null ? param.label : `${param.label} ${label(param, value)}`,
        });
        if (result && result.ok) onDone(result.data);
    };

    const take = async (dir) => {
        const title = dir.path.split("/").filter(Boolean).pop() || dir.path;
        const result = await run("project.add", title, {
            groupId: group.id, group: group.name, profile: contour.name,
            fields: { name: title, path: dir.path, session: "", launch: {} },
        });
        if (result && result.ok) onDone(result.data);
    };
    const hide = async (dir) => {
        const result = await run("disk.hide", dir.path);
        if (result && result.ok) onDone(result.data);
    };
    const moveAll = async (to) => {
        const result = await run("group.move", group.name, { id: group.id, to: to.id, toName: to.name, n: projects.length });
        if (result && result.ok) onDone(result.data);
    };

    const bar = html`<${Bar} changes=${changes} problem=${problem} busy=${busy}
        onSave=${save} onDiscard=${() => { setConflict(""); setDraft({ fields: {}, launch: {} }); }}
        onExit=${() => problem && problem.exit && setDraft((d) => problem.exit.run(d))} />`;

    const head = html`
        <${BackHead} onBack=${leave} label="back">
            <h2>${name || group.name}</h2>
            <span class="where">${target.name} · ${projects.length} ${plural(projects.length, "project", "projects")}</span>
        <//>
    `;
    if (!schema) return html`${head}<p class=${error ? "hint crit" : "empty"}>${error || "Loading…"}</p>`;

    const found = nearby(disk, projects);
    const others = (contour.groups || []).filter((g) => g.id !== group.id);
    const said = summary(projects, contour);

    return html`
        ${head}

        <label class="pffield">
            <span class="pflabel">Name ${touched(draft, "name") && html`<span class="pzdraft">not saved</span>`}</span>
            <input class="search" value=${fieldOf(draft, group, "name") || ""} onInput=${(e) => setField("name", e.target.value)} />
        </label>

        ${contours.length > 1 && html`
            <div class="pffield">
                <span class="pflabel">Contour ${touched(draft, "profileId") && html`<span class="pzdraft">not saved</span>`}</span>
                <div class="pzopts" role="group" aria-label="contour">
                    ${contours.map((p) => html`
                        <button key=${p.id} class="pzopt" type="button" aria-pressed=${p.id === contourNow ? "true" : "false"}
                                onClick=${() => setField("profileId", p.id, { profileId: contour.id })}>${p.name}</button>
                    `)}
                </div>
                <span class=${move ? "pfhelp warn" : "pfhelp"}>${move ? move.said : routed
                    ? "the directory picks the account on this machine: the group stays where its projects' directories put it"
                    : "the group moves to another contour whole, with its projects"}</span>
            </div>
        `}

        <div class="pfsub">projects</div>
        ${projects.length === 0 && html`<p class="pfhelp">no projects on this shelf yet</p>`}
        <${DragRows}
            items=${projects}
            order=${order}
            onOrder=${(ids) => setField("order", ids, { order: baseOrder })}
            name=${(p) => `project ${p.name}`}
            row=${(p) => html`
                <button class="pzgroupmain" type="button"
                        onClick=${() => onForm({ kind: "project", mode: "edit", profile: contour, group, project: p })}>
                    <span class="pzgroupname">${p.name}</span>
                    <span class="pown">${deviations(p, contour).map((v) => html`<span class="ownchip" key=${v.key}>${ownLabel(v)}</span>`)}</span>
                    <span class="chev">${Icon.chevron()}</span>
                </button>
            `}
        />
        <div class="pzgroupfoot">
            ${touched(draft, "order") && html`<span class="pzdraft">order not saved</span>`}
            <button class="pzadd" type="button" onClick=${() => onForm({ kind: "project", mode: "add", profile: contour, group })}>+ Project</button>
        </div>

        ${projects.length > 0 && html`
            <div class="pfsub">the shelf as a whole</div>
            <p class="pzhelp pzsummary">${said.length > 0
                ? `set otherwise than the contour: ${said.join(" · ")}`
                : "every project takes what the contour says"}</p>
            <button class="btn" type="button" onClick=${() => setForAll(true)}>
                Set for all ${projects.length} ${plural(projects.length, "project", "projects")}…
            </button>
        `}

        ${found.length > 0 && html`
            <div class="pfsub">found next to these on disk</div>
            ${found.map((d) => html`
                <div class="pznear" key=${d.path}>
                    <span class="pznearpath">${d.path}</span>
                    ${marks(d) && html`<span class="pzhelp">${marks(d)}</span>`}
                    <span class="pzneardo">
                        <button class="btn" type="button" onClick=${() => take(d)}>Add</button>
                        <button class="btn" type="button" onClick=${() => hide(d)}>Hide</button>
                    </span>
                </div>
            `)}
        `}

        <div class="pzdanger">
            <span class="pzdangertitle">Delete the group</span>
            ${projects.length === 0
                ? html`
                    <span class="pfhelp">the shelf leaves the map; there is nothing on it to lose</span>
                    <button class="btn danger" type="button" onClick=${async () => {
                        const result = await onRemove({ kind: "group", profile: contour, group });
                        if (result && result.ok) onClose();
                    }}>${DELETE}</button>
                `
                : others.length > 0
                    ? html`
                        <span class="pfhelp">only an empty shelf is deleted — move its ${projects.length}
                            ${" "}${plural(projects.length, "project", "projects")} to:</span>
                        <div class="pzopts">
                            ${others.map((g) => html`
                                <button key=${g.id} class="pzopt" type="button" onClick=${() => moveAll(g)}>${g.name}</button>
                            `)}
                        </div>
                    `
                    : html`<span class="pfhelp">only an empty shelf is deleted, and the contour has no other group to move its
                        ${" "}${plural(projects.length, "project", "projects")} to</span>`}
        </div>

        ${bar}

        <${ForAll} open=${forAll} schema=${schema} catalog=${catalog} projects=${projects}
            onClose=${() => setForAll(false)} onSay=${say} />

        <${LeaveSheet}
            open=${leaving}
            changes=${changes}
            problem=${problem}
            busy=${busy}
            onStay=${() => setLeaving(false)}
            onDiscard=${() => { setLeaving(false); onClose(); }}
            onSave=${async () => {
                if (await save()) {
                    setLeaving(false);
                    onClose();
                }
            }}
        />
    `;
}

export function GroupLayer(props) {
    return html`<${Layer} label=${props.group.name}><${GroupSettings} ...${props} /><//>`;
}
