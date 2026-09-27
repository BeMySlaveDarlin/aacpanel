// The settings page of one project: the command its next launch runs at the
// top, where the session lives as two cards, the launch parameters as rows
// drawn from the schema, the project itself, the map's hints and the
// deletion. Every change goes into a draft: one bar at the foot says how many
// there are and saves them together, and a draft the launch would refuse
// holds the bar with the reason and one press out of it.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { LaunchLine } from "../../ui/launchline.js";
import { useAction } from "../../actions/gate.js";
import {
    body, count, emptyDraft, field, fieldOf, fieldProblems, overlay, own, pins, put, touched, valueOf, weighty,
} from "./draft.js";
import { ModelSheet, traitOf } from "./controls.js";
import { paramOf, useSchema } from "./schema.js";
import {
    Bar, LAUNCH_ORDER, LaunchRow, Layer, LeaveSheet, Where, launchAsk, modelHolds, problemOf, useDraft, usePreview,
} from "./kit.js";

// The button says what the confirmation sheet will say: project.remove.
const DELETE = "Delete project";

// Account names where the session goes and what reaches it past the command:
// the account's own model, effort and mode are not in the words above, and
// without this line a bare command reads as nothing set at all.
function Account({ contour, effective, stream }) {
    const past = ["model", "effort", "permissionMode"]
        .map((key) => valueOf(effective, key))
        .filter((v) => v.layer === "account" && v.value !== null)
        .map((v) => String(v.value));
    return html`
        <div class="pzacct">
            <span>account ${contour.name} · ${stream ? "feed (claude -p)" : "console (tmux)"}</span>
            ${past.length > 0 && html`<span>from the account, not in the command: ${past.join(" · ")}</span>`}
        </div>
    `;
}

export function ProjectSettings({ project, contour, group, catalog, onClose, onDone, onRemove }) {
    const { schema, error } = useSchema();
    const run = useAction();
    const [modelOpen, setModelOpen] = useState(false);
    const [busy, setBusy] = useState(false);
    const [conflict, setConflict] = useState("");
    const { draft, setDraft, leaving, setLeaving, leave, hold } = useDraft(onClose, modelOpen);
    useBackClose(true, onClose, hold);
    const preview = usePreview("project", project.id,
        `${JSON.stringify(project.launch || {})}:${project.session}:${project.path}`,
        launchAsk(draft, ["session", "path"]));
    const changes = count(draft);

    const groups = (contour.groups || []).filter((g) => g && g.id);
    const groupNow = fieldOf(draft, project, "groupId") ?? project.groupId;
    const head = html`
        <${BackHead} onBack=${leave} label="back">
            <h2>${fieldOf(draft, project, "name") || project.name}</h2>
            <span class="where">${contour.name} · ${(groups.find((g) => g.id === groupNow) || group || {}).name || ""}</span>
        <//>
    `;
    if (!schema) return html`${head}<p class=${error ? "hint crit" : "empty"}>${error || "Loading…"}</p>`;

    const params = schema.params || [];
    const effective = (preview && preview.effective) || overlay(project.effective, draft, contour.effective);
    const line = (preview && preview.line) || project.line;
    const contourEffective = contour.effective || [];
    const transport = valueOf(effective, "transport").value || "tmux";
    const model = valueOf(effective, "model");
    const trait = traitOf(schema.traits, catalog, model.value);

    const set = (key, value) => {
        setConflict("");
        setDraft((d) => put(d, project, key, value));
    };
    const setField = (name, value) => {
        setConflict("");
        setDraft((d) => field(d, project, name, value));
    };
    const effOf = (key) => ({ ...valueOf(effective, key), below: valueOf(contourEffective, key) });

    const { blocked, strikes } = modelHolds({ schema, draft, owner: project, effective, trait, model });
    const problem = conflict ? { text: conflict, exit: null }
        : problemOf({ draft, params, preview, blocked, fields: fieldProblems(draft, project) });

    const guard = contour.contextGuard === false ? "the context guard hook is not installed in this account" : "";
    const noBridge = contour.auth === "token" ? "an account on a token has no bridge to claude.ai" : "";
    const offOf = { remoteControl: noBridge, autoRestart: guard, restartIntent: guard };

    const runExit = () => {
        if (!problem || !problem.exit) return;
        if (problem.exit.run) setDraft((d) => problem.exit.run(d));
        else set(problem.exit.remove, null);
    };

    const save = async () => {
        if (problem || count(draft) === 0) return false;
        setBusy(true);
        const fields = body(draft);
        const name = String(fieldOf(draft, project, "name") || project.name).trim();
        const groups = contour.groups || [];
        const moveTo = "groupId" in draft.fields
            ? ((groups.find((g) => g.id === draft.fields.groupId) || {}).name || "")
            : "";
        const kind = weighty(draft) ? "project.edit" : "project.save";
        const result = await run(kind, name, {
            id: project.id,
            fields,
            moveTo,
            pathFrom: "path" in draft.fields ? project.path : "",
        });
        setBusy(false);
        if (!result || !result.ok) {
            if (result && result.status === 409) setConflict(result.error);
            return false;
        }
        onDone(result.data);
        setDraft(emptyDraft());
        return true;
    };

    const rowProps = (key) => ({
        param: paramOf(schema, key),
        draft,
        owner: project,
        layer: "project",
        eff: effOf(key),
        transport,
        off: offOf[key] || "",
        onUnset: () => set(key, null),
    });

    const bar = () => html`<${Bar} changes=${changes} problem=${problem} busy=${busy}
        onSave=${save} onDiscard=${() => { setConflict(""); setDraft(emptyDraft()); }} onExit=${runExit} />`;

    const hints = pins(params, draft, project, contourEffective);
    const pathNow = String(fieldOf(draft, project, "path") || "");
    const dirName = pathNow.split("/").filter(Boolean).pop() || "";

    return html`
        ${head}

        <div class="pzlineblock">
            <${LaunchLine} line=${line} changed=${new Set([...Object.keys(draft.launch), ...("session" in draft.fields ? ["session"] : [])])} />
            <${Account} contour=${contour} effective=${effective} stream=${transport === "stream"} />
        </div>

        <div class="pfsub">where it lives</div>
        <${Where}
            param=${paramOf(schema, "transport")}
            draft=${draft}
            owner=${project}
            effective=${effective}
            below=${contourEffective}
            params=${params}
            onPick=${(value) => set("transport", value)}
        />

        <div class="pfsub">launch</div>
        ${LAUNCH_ORDER.map((key) => {
            const p = rowProps(key);
            if (!p.param) return null;
            const mine = own(draft, project, key);
            return html`<${LaunchRow} key=${key} p=${p} mine=${mine} catalog=${catalog} trait=${trait} model=${model}
                strike=${strikes[key]} onSet=${(value) => set(key, value)} onModel=${() => setModelOpen(true)} />`;
        })}

        <div class="pfsub">project</div>
        <label class="pffield">
            <span class="pflabel">Caption ${touched(draft, "name") && html`<span class="pzdraft">not saved</span>`}</span>
            <input class="search" value=${fieldOf(draft, project, "name") || ""}
                   onInput=${(e) => setField("name", e.target.value)} />
        </label>
        <label class="pffield">
            <span class="pflabel">Directory ${touched(draft, "path") && html`<span class="pzdraft">not saved</span>`}</span>
            <input class="search" spellcheck="false" value=${pathNow}
                   onInput=${(e) => setField("path", e.target.value)} />
            <span class=${touched(draft, "path") ? "pfhelp warn" : "pfhelp"}>${touched(draft, "path")
                ? `the conversations of ${project.path} stay in the archive and no longer resume here`
                : "absolute; the host checks that it is inside the allowed roots"}</span>
        </label>
        <label class="pffield">
            <span class="pflabel">Session name ${touched(draft, "session") && html`<span class="pzdraft">not saved</span>`}</span>
            <input class="search" spellcheck="false" placeholder=${`from the directory name: ${dirName}`}
                   value=${fieldOf(draft, project, "session") || ""}
                   onInput=${(e) => setField("session", e.target.value)} />
        </label>
        ${groups.length > 1 && html`
            <div class="pffield">
                <span class="pflabel">Group ${touched(draft, "groupId") && html`<span class="pzdraft">not saved</span>`}</span>
                <div class="pzopts" role="group" aria-label="group">
                    ${groups.map((g) => html`
                        <button key=${g.id} class="pzopt" type="button" aria-pressed=${g.id === groupNow ? "true" : "false"}
                                onClick=${() => setField("groupId", g.id)}>${g.name}</button>
                    `)}
                </div>
                <span class="pfhelp">a group is a shelf of this contour; a group moves to another contour whole</span>
            </div>
        `}
        <label class="pffield">
            <span class="pflabel">Base branch ${touched(draft, "base") && html`<span class="pzdraft">not saved</span>`}</span>
            <input class="search" spellcheck="false" placeholder="main — nobody picked one"
                   value=${fieldOf(draft, project, "base") || ""}
                   onInput=${(e) => setField("base", e.target.value)} />
            <span class="pfhelp">what a review of this branch is measured against</span>
        </label>

        ${hints.length > 0 && html`
            <div class="pfsub">hints</div>
            ${hints.map((h) => html`
                <div class="pzhint" key=${h.key}>
                    <span>${h.text}</span>
                    <button class="btn" type="button" onClick=${() => set(h.key, null)}>Remove</button>
                </div>
            `)}
        `}

        <div class="pzdanger">
            <span class="pzdangertitle">Delete the project</span>
            <span class="pfhelp">the entry leaves the map; the directory and its conversations stay on the host, and the note
                after the deletion offers Undo</span>
            <button class="btn danger" type="button" onClick=${async () => {
                const result = await onRemove({ kind: "project", profile: contour, group, project });
                if (result && result.ok) onClose();
            }}>${DELETE}</button>
        </div>

        ${bar()}

        <${ModelSheet}
            open=${modelOpen}
            param=${paramOf(schema, "model")}
            eff=${effOf("model")}
            mine=${own(draft, project, "model")}
            catalog=${catalog}
            contour=${contour.name}
            onPick=${(value) => set("model", value)}
            onClose=${() => setModelOpen(false)}
            bar=${bar()}
        />

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

// locate finds a project on the map as the map has it now: after a Save the
// page shows what was saved, not what it was opened with.
export function locate(profiles, id) {
    for (const contour of profiles || []) {
        for (const group of contour.groups || []) {
            for (const project of group.projects || []) {
                if (project.id === id) return { contour, group, project };
            }
        }
    }
    return {};
}

// ProjectLayer is the page as a layer: full screen on the phone, a dialog on
// a wide screen, and on either the question before a draft is thrown away.
export function ProjectLayer(props) {
    return html`<${Layer} label=${props.project.name}><${ProjectSettings} ...${props} /><//>`;
}
