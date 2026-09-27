// The settings page of one project: the command its next launch runs at the
// top, where the session lives as two cards, the launch parameters as rows
// drawn from the schema, the project itself, the map's hints and the
// deletion. Every change goes into a draft: one bar at the foot says how many
// there are and saves them together, and a draft the launch would refuse
// holds the bar with the reason and one press out of it.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Sheet } from "../../ui/sheet.js";
import { useWide } from "../../ui/wide.js";
import { LaunchLine } from "../../ui/launchline.js";
import { useAction } from "../../actions/gate.js";
import {
    body, consequences, count, emptyDraft, exit, field, fieldOf, fieldProblems, label, outcome, overlay, own, pins, put,
    revert, sourceOf, touched, valueOf, weighty,
} from "./draft.js";
import {
    ArgTokens, CapChips, EnvRows, Meaning, ModelRow, ModelSheet, Options, Row, TextRow, optionsOf, struck, traitOf,
} from "./controls.js";
import { paramOf, useSchema } from "./schema.js";

// The button says what the confirmation sheet will say: project.remove.
const DELETE = "Delete project";

const LAUNCH_ORDER = [
    "model", "effort", "permissionMode", "remoteControl", "intent",
    "contextCap", "autoRestart", "restartIntent", "env", "args",
];

// usePreview asks the service what the project would launch with if the
// draft were saved: the command by the launcher's own code, the effective
// values and what the launch would refuse. With nothing in the draft that
// touches the launch, the project's own answer stands.
function usePreview(project, draft) {
    const [got, setGot] = useState(null);
    const ask = previewBody(draft);
    const key = ask ? JSON.stringify(ask) : "";
    const base = project ? `${project.id}:${JSON.stringify(project.launch || {})}:${project.session}:${project.path}` : "";
    useEffect(() => {
        if (!project || !ask) {
            setGot(null);
            return undefined;
        }
        const ctl = new AbortController();
        const timer = setTimeout(async () => {
            try {
                const response = await fetch(`/api/projects/${project.id}/preview`, {
                    method: "POST",
                    credentials: "same-origin",
                    headers: { "Content-Type": "application/json" },
                    body: key,
                    signal: ctl.signal,
                });
                const text = await response.text();
                if (!response.ok) {
                    setGot({ error: text.trim() || `the server answered ${response.status}` });
                    return;
                }
                setGot({ ...JSON.parse(text), key });
            } catch (err) {
                if (err.name !== "AbortError") setGot({ error: "the preview did not come: the network is unavailable" });
            }
        }, 180);
        return () => {
            clearTimeout(timer);
            ctl.abort();
        };
    }, [key, base]);
    return got && (!got.key || got.key === key) ? got : null;
}

function previewBody(draft) {
    const out = body(draft);
    const ask = {};
    if (out.launchSet) ask.launchSet = out.launchSet;
    if (out.launchUnset) ask.launchUnset = out.launchUnset;
    if ("session" in out) ask.session = out.session;
    if ("path" in out) ask.path = out.path;
    return Object.keys(ask).length > 0 ? ask : null;
}

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

// Where renders the two places a session lives, the chosen one first in
// weight: under each, what living there means for this project.
function Where({ param, draft, project, effective, contourEffective, params, onPick }) {
    const mine = own(draft, project, "transport");
    const eff = valueOf(effective, "transport");
    const chosen = mine !== null ? mine : eff.value || "tmux";
    const inherited = mine === null;
    const below = valueOf(contourEffective, "transport");
    return html`
        <div class="pzwhere">
            ${param.options.map((o) => {
                const on = chosen === o.value;
                const said = consequences(params, o.value, effective);
                return html`
                    <button
                        key=${o.value}
                        class="pzcard"
                        type="button"
                        aria-pressed=${on ? "true" : "false"}
                        data-inherited=${on && inherited ? "1" : "0"}
                        onClick=${() => onPick(on && !inherited ? null : o.value)}
                    >
                        <span class="pzcardhead">
                            <span class="pzcardname">${o.label}</span>
                            ${on && inherited && html`<span class="pzcardfrom">${below.layer === "claude" ? "default" : sourceOf(below.layer)}</span>`}
                            ${on && touched(draft, "transport") && html`<span class="pzdraft">not saved</span>`}
                        </span>
                        <span class="pzhelp">${o.meaning}</span>
                        ${on && said.length > 0 && html`
                            <ul class="pzsaid">${said.map((line) => html`<li key=${line}>${line}</li>`)}</ul>
                        `}
                    </button>
                `;
            })}
        </div>
    `;
}

// Bar is the one bar of the draft: how many changes and Save, or what holds
// Save and the one press out of it.
function Bar({ changes, problem, busy, onSave, onDiscard, onExit }) {
    if (changes === 0 && !problem) return null;
    return html`
        <div class="pzbar" data-blocked=${problem ? "1" : "0"}>
            ${problem
                ? html`
                    <span class="pzbarsay">${problem.text}</span>
                    ${problem.exit && html`<button class="btn" type="button" onClick=${onExit}>${problem.exit.text}</button>`}
                `
                : html`<span class="pzbarsay">${changes} ${changes === 1 ? "change" : "changes"}</span>`}
            ${changes > 0 && html`
                <button class="btn" type="button" disabled=${busy} onClick=${onDiscard}>Discard</button>
                <button class="btn primary" type="button" disabled=${busy || Boolean(problem)} onClick=${onSave}>Save</button>
            `}
        </div>
    `;
}

// problemOf returns the first thing that holds Save: a field the map needs,
// a value the launch would refuse, a value the model does not take.
function problemOf({ draft, project, params, preview, blocked }) {
    for (const p of fieldProblems(draft, project)) {
        return { text: p.why, exit: touched(draft, p.key) ? { text: "Undo the change", run: (d) => revert(d, p.key) } : null };
    }
    for (const p of (preview && preview.problems) || []) {
        const param = params.find((x) => x.key === p.key);
        return { text: `${param ? param.label : p.key}: ${p.why}`, exit: exit(draft, param, p.key) };
    }
    if (preview && preview.error) return { text: preview.error, exit: null };
    for (const b of blocked) return b;
    return null;
}

export function ProjectSettings({ project, contour, group, catalog, order, onClose, onDone, onRemove }) {
    const { schema, error } = useSchema();
    const run = useAction();
    const [draft, setDraft] = useState(emptyDraft);
    const [modelOpen, setModelOpen] = useState(false);
    const [leaving, setLeaving] = useState(false);
    const [busy, setBusy] = useState(false);
    const [conflict, setConflict] = useState("");
    const preview = usePreview(project, draft);
    const changes = count(draft);

    const leave = () => {
        if (modelOpen || leaving) return;
        if (count(draft) > 0) setLeaving(true);
        else onClose();
    };
    useBackClose(true, onClose, () => {
        if (count(draft) === 0) return false;
        setLeaving(true);
        return true;
    });
    const wide = useWide();
    const leaveRef = useRef(leave);
    leaveRef.current = leave;
    useEffect(() => {
        if (!wide) return undefined;
        const onKey = (event) => {
            if (event.key === "Escape" && !event.defaultPrevented) leaveRef.current();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [wide]);

    if (!schema) {
        return html`
            <${BackHead} onBack=${onClose} label="back"><h2>${project.name}</h2><//>
            <p class=${error ? "hint crit" : "empty"}>${error || "Loading…"}</p>
        `;
    }

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

    // What the model does not take: the project's own value holds Save, the
    // account's or the contour's is said under the row and starts without it.
    const blocked = [];
    const strikes = {};
    for (const key of ["effort", "permissionMode"]) {
        const param = paramOf(schema, key);
        const value = valueOf(effective, key).value;
        const why = param && value !== null ? struck(param, value, trait, model.value) : "";
        if (!why) continue;
        if (own(draft, project, key) !== null) {
            blocked.push({ text: `${why} — ${label(param, value)} would not start`, exit: exit(draft, param, key) });
        } else {
            strikes[key] = key === "permissionMode" ? `${why}: ${label(param, value)} from ${sourceOf(valueOf(effective, key).layer)} would start as Manual`
                : `${why}: ${label(param, value)} from ${sourceOf(valueOf(effective, key).layer)} is dropped at the start`;
        }
    }
    const problem = conflict ? { text: conflict, exit: null } : problemOf({ draft, project, params, preview, blocked });

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
        project,
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
    const groups = (contour.groups || []).filter((g) => g && g.id);
    const groupNow = fieldOf(draft, project, "groupId") ?? project.groupId;

    return html`
        <${BackHead} onBack=${leave} label="back">
            <h2>${fieldOf(draft, project, "name") || project.name}</h2>
            <span class="where">${contour.name} · ${(groups.find((g) => g.id === groupNow) || group || {}).name || ""}</span>
        <//>

        <div class="pzlineblock">
            <${LaunchLine} line=${line} changed=${new Set([...Object.keys(draft.launch), ...("session" in draft.fields ? ["session"] : [])])} />
            <${Account} contour=${contour} effective=${effective} stream=${transport === "stream"} />
        </div>

        <div class="pfsub">where it lives</div>
        <${Where}
            param=${paramOf(schema, "transport")}
            draft=${draft}
            project=${project}
            effective=${effective}
            contourEffective=${contourEffective}
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

        ${order && html`
            <div class="pfsub">place in the group</div>
            <div class="pforder">
                <span class="pfhelp">${order.index + 1} of ${order.total}</span>
                <button class="btn" type="button" disabled=${order.index === 0} onClick=${() => order.move("up")}>Up</button>
                <button class="btn" type="button" disabled=${order.index === order.total - 1}
                        onClick=${() => order.move("down")}>Down</button>
            </div>
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

        <${Sheet} open=${leaving} onClose=${() => setLeaving(false)} label="changes not saved">
            <div class="pzleave">
                <h3>${changes} ${changes === 1 ? "change" : "changes"} not saved</h3>
                ${problem && html`<p class="pfhelp warn">${problem.text}</p>`}
                <div class="btnrow">
                    <button class="btn" type="button" onClick=${() => { setLeaving(false); onClose(); }}>Discard</button>
                    <button class="btn primary" type="button" disabled=${busy || Boolean(problem)} onClick=${async () => {
                        if (await save()) {
                            setLeaving(false);
                            onClose();
                        }
                    }}>Save</button>
                </div>
            </div>
        <//>
    `;
}

// LaunchRow is one launch parameter drawn by its kind.
function LaunchRow({ p, mine, catalog, trait, model, strike, onSet, onModel }) {
    const { param, eff } = p;
    let control = null;
    let foot = null;
    if (param.kind === "model") {
        control = html`<${ModelRow} param=${param} eff=${eff} mine=${mine} catalog=${catalog} onOpen=${onModel} />`;
    } else if (param.kind === "enum" || param.kind === "bool") {
        const why = (value) => struck(param, value, trait, model.value);
        control = html`<${Options} param=${param} eff=${eff} mine=${mine} options=${optionsOf(param)} why=${why}
            onPick=${onSet} />`;
        const chosen = mine !== null ? mine : eff.value;
        foot = html`
            ${mine === null && html`<span class="pzhelp pzfrom">${outcome(param, eff)}</span>`}
            ${param.kind === "enum" && html`<${Meaning} param=${param} value=${chosen} />`}
            ${strike && html`<span class="pzhelp warn">${strike}</span>`}
        `;
    } else if (param.kind === "int") {
        control = html`<${CapChips} param=${param} eff=${eff} mine=${mine} onSet=${onSet} />`;
        foot = mine === null ? html`<span class="pzhelp pzfrom">${outcome(param, eff)}</span>` : null;
    } else if (param.kind === "text") {
        control = html`<${TextRow} param=${param} eff=${eff} mine=${mine} onSet=${onSet} />`;
        foot = mine === null ? html`<span class="pzhelp pzfrom">${outcome(param, eff)}</span>` : null;
    } else if (param.kind === "kv") {
        control = html`<${EnvRows} eff=${eff} mine=${mine} onSet=${onSet} />`;
    } else if (param.kind === "tokens") {
        control = html`<${ArgTokens} eff=${eff} mine=${mine} onSet=${onSet} />`;
        foot = html`<span class="pzhelp">${param.help}</span>`;
    }
    return html`<${Row} ...${p} foot=${foot}>${control}<//>`;
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
    const wide = useWide();
    const page = html`<${ProjectSettings} ...${props} />`;
    if (!wide) return html`<div class="pzpage">${page}</div>`;
    return html`
        <div class="dkscrim">
            <div class="dkmodal pzmodal" role="dialog" aria-modal="true" aria-label=${props.project.name}
                 onClick=${(event) => event.stopPropagation()}>
                <div class="dkmodalbody">${page}</div>
            </div>
        </div>
    `;
}

