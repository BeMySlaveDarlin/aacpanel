// The settings page of a contour: its groups in the order they stand, the
// defaults its projects start with — the same rows a project has, over what
// the account says — with the map's hints about values its projects all repeat,
// the account and its files as the host has them, the journal of the map and
// the deletion of an empty contour. One draft and one bar, as on a project.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useWide } from "../../ui/wide.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { PERSONAL } from "../../contour.js";
import { plural } from "../../format.js";
import { body, count, field, fieldOf, label, overlay, own, pins, put, touched, valueOf } from "./draft.js";
import { ModelPopover, ModelSheet, catalogRows, traitOf } from "./controls.js";
import { paramOf, useSchema } from "./schema.js";
import { authState } from "./pick.js";
import { hooksWarning } from "./page.js";
import {
    Bar, DragRows, LAUNCH_ORDER, LaunchRow, Layer, LeaveSheet, Where, launchAsk, modelHolds, problemOf, useDraft, usePreview,
} from "./kit.js";

// The button says what the confirmation sheet will say: profile.remove.
const DELETE = "Delete profile";

const ACCOUNT_KEYS = ["model", "effort", "permissionMode"];

// accountLayer returns what a contour's projects start with where the contour
// says nothing either: the account's settings, the panel's own default, or
// claude's.
export function accountLayer(params, contour) {
    const account = contour.account || {};
    return params.map((p) => {
        if (ACCOUNT_KEYS.includes(p.key) && account[p.key] !== undefined) {
            return { key: p.key, value: account[p.key], layer: "account" };
        }
        if (p.default !== null && p.default !== undefined) return { key: p.key, value: p.default, layer: "panel" };
        return { key: p.key, value: null, layer: "claude" };
    });
}

export function projectsOf(contour) {
    return (contour.groups || []).flatMap((g) => g.projects || []);
}

function has(launch, key) {
    return Boolean(launch) && Object.prototype.hasOwnProperty.call(launch, key);
}

// raises returns the values most of a contour's projects store the same:
// made the contour's default, they stop being a copy in every project. A
// value the contour already gives is not offered again.
export function raises(params, contour, effective) {
    const projects = projectsOf(contour);
    const out = [];
    for (const param of params) {
        const tally = new Map();
        for (const p of projects) {
            if (!has(p.launch, param.key)) continue;
            const key = JSON.stringify(p.launch[param.key]);
            tally.set(key, (tally.get(key) || 0) + 1);
        }
        let best = null;
        for (const [key, n] of tally) if (!best || n > best.n) best = { key, n };
        if (!best || best.n < 3 || best.n * 2 <= projects.length) continue;
        const value = JSON.parse(best.key);
        if (JSON.stringify(valueOf(effective, param.key).value) === best.key) continue;
        out.push({
            key: param.key,
            value,
            text: `${param.label} is ${label(param, value)} in ${best.n} of ${projects.length} projects — make it this contour's default?`,
        });
    }
    return out;
}

// copies returns the values the contour stores that some of its projects
// still store the same themselves: the copy stops following the contour.
export function copies(params, contour) {
    const own = contour.launch || {};
    const out = [];
    for (const param of params) {
        if (!has(own, param.key)) continue;
        const same = projectsOf(contour).filter((p) => has(p.launch, param.key)
            && JSON.stringify(p.launch[param.key]) === JSON.stringify(own[param.key]));
        if (same.length === 0) continue;
        out.push({
            key: param.key,
            label: `${param.label} ${label(param, own[param.key])}`,
            names: same.map((p) => p.name),
            text: `${same.length} ${plural(same.length, "project", "projects")} still ${same.length === 1 ? "stores" : "store"} ${param.label} ${label(param, own[param.key])} ${same.length === 1 ? "itself" : "themselves"} — remove the copies?`,
        });
    }
    return out;
}

// followers says, for a change of the contour's value, how many of its
// projects take it and how many keep their own.
export function followers(contour, key) {
    const projects = projectsOf(contour);
    const own = projects.filter((p) => has(p.launch, key)).length;
    const follow = projects.length - own;
    if (projects.length === 0) return "no projects yet: new ones get it";
    return `${follow} of ${projects.length} ${plural(projects.length, "project", "projects")} follow it`
        + (own > 0 ? ` · ${own} ${own === 1 ? "sets its" : "set their"} own and ${own === 1 ? "stays" : "stay"}` : "");
}

function absolute(path) {
    return path.startsWith("/") && !path.split("/").includes("..");
}

// fieldIssues returns what in the contour's own fields Save must not send.
function fieldIssues(draft, contour) {
    const out = [];
    const name = String(fieldOf(draft, contour, "name") || "").trim();
    const dir = String(fieldOf(draft, contour, "configDir") || "").trim();
    const prefix = String(fieldOf(draft, contour, "prefix") || "").trim();
    const bin = String(fieldOf(draft, contour, "claudeBin") || "").trim();
    if (!name) out.push({ key: "name", why: "without a name a contour cannot be told from its neighbour" });
    if (!dir) out.push({ key: "configDir", why: "the config directory is required: the account's token lives in it" });
    else if (!absolute(dir)) out.push({ key: "configDir", why: "the config directory has to be an absolute path" });
    if (prefix && !absolute(prefix)) out.push({ key: "prefix", why: "the path prefix has to be an absolute path" });
    if (bin && !absolute(bin)) out.push({ key: "claudeBin", why: "the path to claude has to be absolute: a relative one is picked by PATH" });
    return out;
}

// Groups lists the contour's groups in their order; a handle drags a group
// to another place, and the new order goes into the draft.
function Groups({ groups, order, onOrder, onOpen, onAdd, moved }) {
    return html`
        <${DragRows}
            items=${groups}
            order=${order}
            onOrder=${onOrder}
            name=${(g) => `group ${g.name}`}
            row=${(g) => html`
                <button class="pzgroupmain" type="button" onClick=${() => onOpen(g)}>
                    <span class="pzgroupname">${g.name}</span>
                    <span class="count">${(g.projects || []).length}</span>
                    <span class="chev">${Icon.chevron()}</span>
                </button>
            `}
        />
        <div class="pzgroupfoot">
            ${moved && html`<span class="pzdraft">order not saved</span>`}
            <button class="pzadd" type="button" onClick=${onAdd}>+ Group</button>
        </div>
    `;
}

// Files says how the contour's account is set up on the host. Where the
// router's registry holds the account, the map only mirrors it: a session
// started under the prefix goes into the account whatever the map says.
function Files({ contour, draft, catalog, setField }) {
    const auth = authState(contour);
    const account = contour.account || {};
    const defaults = ACCOUNT_KEYS.map((k) => account[k]).filter(Boolean);
    const models = catalogRows(catalog).length;
    const warn = hooksWarning(contour);
    const routed = Boolean(contour.route);
    const locked = contour.name === PERSONAL;
    const fieldRow = (name, title, placeholder, help) => html`
        <label class="pffield">
            <span class="pflabel">${title} ${touched(draft, name) && html`<span class="pzdraft">not saved</span>`}</span>
            <input class="search" spellcheck=${false} placeholder=${placeholder}
                   value=${fieldOf(draft, contour, name) || ""} onInput=${(e) => setField(name, e.target.value)} />
            <span class="pfhelp">${help}</span>
        </label>
    `;
    return html`
        <label class="pffield">
            <span class="pflabel">Name ${touched(draft, "name") && html`<span class="pzdraft">not saved</span>`}</span>
            <input class="search" spellcheck=${false} disabled=${locked}
                   value=${fieldOf(draft, contour, "name") || ""} onInput=${(e) => setField("name", e.target.value)} />
            <span class="pfhelp">${locked
                ? `the personal contour is called ${PERSONAL}: by that name the panel and the collector find its directory`
                : "unique across the machine: it is how the contour is recognised"}</span>
        </label>
        <div class="pfprops pzfacts">
            <div class="kv"><span class="k">authorization</span><span class="v"><span class=${`dot ${auth.dot}`}></span> ${auth.text}</span></div>
            <div class="kv"><span class="k">account defaults</span><span class="v">${defaults.length > 0
                ? defaults.join(" · ") : html`<span class="pfnone">the settings of the account were not read</span>`}</span></div>
            <div class="kv"><span class="k">context guard hook</span><span class="v">${contour.contextGuard === true
                ? "installed" : contour.contextGuard === false ? "not installed — Auto restart does nothing here" : html`<span class="pfnone">not known</span>`}</span></div>
            <div class="kv"><span class="k">model catalogue</span><span class="v">${models > 0
                ? `${models} ${plural(models, "model", "models")}` : html`<span class="pfnone">not read — aliases only</span>`}</span></div>
            ${routed && html`
                <div class="kv"><span class="k">config directory</span><span class="v pfpath">${contour.configDir}</span></div>
                <div class="kv"><span class="k">routed by</span><span class="v pfpath">${contour.route.prefix === "*" ? "every directory no other contour takes" : contour.route.prefix}</span></div>
                <div class="kv"><span class="k">claude at</span><span class="v pfpath">${contour.claudeBin || html`<span class="pfnone">what the launcher finds</span>`}</span></div>
            `}
        </div>
        ${warn && html`<p class="pfhelp warn">${warn}</p>`}
        ${routed
            ? html`<p class="pfhelp">${"the account and its files are the host's: the router's registry holds them, and a session "
                + `started ${contour.route.prefix === "*" ? "in a directory no other contour takes" : `under ${contour.route.prefix}`} `
                + "goes into this account whatever the map says — they are changed on the host, not here"}</p>`
            : html`
                ${fieldRow("configDir", "Config directory", "~/.claude", "the token, the settings and the conversation archive of this contour live there")}
                ${fieldRow("prefix", "Path prefix", "/srv/proj", "the wrapper picks the contour by it for the directory it was called from")}
                ${fieldRow("claudeBin", "What to launch with", "/usr/local/bin/claude", String(fieldOf(draft, contour, "claudeBin") || "").trim()
                    ? "the host checks the file at launch: the service lives in a container and does not see the files of the machine"
                    : "empty — the launcher looks for claude itself: AACP_CLAUDE first, then the wrapper from the delivery, then the "
                        + "first claude in PATH; the last one brings the session up in the personal account, whatever the contour is")}
            `}
    `;
}

export function ContourSettings({ contour, catalog, order, onClose, onDone, onRemove, onForm, onJournal, onDirty }) {
    const { schema, error } = useSchema();
    const run = useAction();
    const [modelOpen, setModelOpen] = useState(false);
    const [busy, setBusy] = useState(false);
    const [conflict, setConflict] = useState("");
    const { draft, setDraft, leaving, setLeaving, leave, hold, topRef } = useDraft(onClose, modelOpen, onDirty);
    topRef.current = useBackClose(true, onClose, hold).isTop;
    const wide = useWide();
    const preview = usePreview("contour", contour.id, JSON.stringify(contour.launch || {}), launchAsk(draft));
    const changes = count(draft);
    const groups = (contour.groups || []).filter((g) => g && g.id);
    const baseOrder = groups.map((g) => g.id);
    const groupOrder = fieldOf(draft, { order: baseOrder }, "order") || baseOrder;

    const auth = authState(contour);
    const projects = projectsOf(contour);
    const head = html`
        <${BackHead} onBack=${leave} label="back">
            <h2>${fieldOf(draft, contour, "name") || contour.name}</h2>
            <span class="where">${auth.text} · ${projects.length} ${plural(projects.length, "project", "projects")}</span>
        <//>
    `;
    if (!schema) return html`${head}<p class=${error ? "hint crit" : "empty"}>${error || "Loading…"}</p>`;

    const params = schema.params || [];
    const below = accountLayer(params, contour);
    const effective = (preview && preview.effective) || overlay(contour.effective, draft, below);
    const transport = valueOf(effective, "transport").value || "tmux";
    const model = valueOf(effective, "model");
    const trait = traitOf(schema.traits, catalog, model.value);

    const set = (key, value) => {
        setConflict("");
        setDraft((d) => put(d, contour, key, value));
    };
    const setField = (name, value) => {
        setConflict("");
        setDraft((d) => field(d, contour, name, value));
    };
    const setOrder = (ids) => setDraft((d) => field(d, { order: baseOrder }, "order", ids));
    const effOf = (key) => ({ ...valueOf(effective, key), below: valueOf(below, key) });

    const { blocked, strikes } = modelHolds({ schema, draft, owner: contour, effective, trait, model });
    const problem = conflict ? { text: conflict, exit: null }
        : problemOf({ draft, params, preview, blocked, fields: fieldIssues(draft, contour) });

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
        const { order: ids, ...fields } = body(draft);
        const name = String(fieldOf(draft, contour, "name") || contour.name).trim();
        let result = { ok: true, data: null };
        if (Object.keys(fields).length > 0) {
            const weighty = ["configDir", "prefix", "claudeBin"].some((k) => k in fields);
            result = await run(weighty ? "profile.edit" : "profile.save", name, { id: contour.id, fields });
        }
        if (result && result.ok && ids) {
            result = await run("group.reorder", name, { profileId: contour.id, ids });
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

    const unpin = async (copy) => {
        const result = await run("profile.unpin", contour.name, {
            id: contour.id, key: copy.key, label: copy.label, names: copy.names,
        });
        if (result && result.ok) onDone(result.data);
    };

    const rowProps = (key) => ({
        param: paramOf(schema, key),
        draft,
        owner: contour,
        layer: "contour",
        eff: effOf(key),
        transport,
        off: offOf[key] || "",
        onUnset: () => set(key, null),
    });

    const modelPicker = () => html`<${ModelPopover}
        open=${modelOpen}
        param=${paramOf(schema, "model")}
        eff=${effOf("model")}
        mine=${own(draft, contour, "model")}
        catalog=${catalog}
        contour=${contour.name}
        onPick=${(value) => set("model", value)}
        onClose=${() => setModelOpen(false)}
        bar=${bar()}
    />`;
    const bar = () => html`<${Bar} changes=${changes} problem=${problem} busy=${busy}
        onSave=${save} onDiscard=${() => { setConflict(""); setDraft({ fields: {}, launch: {} }); }} onExit=${runExit} />`;

    const raised = raises(params, contour, effective);
    const copied = copies(params, contour).filter((c) => !touched(draft, c.key));
    const pinned = pins(params, draft, contour, below);
    const empty = groups.length === 0;

    return html`
        ${head}

        <div class="pfsub">groups</div>
        ${groups.length === 0 && html`<p class="pfhelp">no groups yet: a group is a shelf to put projects on</p>`}
        <${Groups}
            groups=${groups}
            order=${groupOrder}
            moved=${touched(draft, "order")}
            onOrder=${setOrder}
            onOpen=${(group) => onForm({ kind: "group", mode: "edit", profile: contour, group })}
            onAdd=${() => onForm({ kind: "group", mode: "add", profile: contour })}
        />

        <div class="pfsub">defaults for its projects</div>
        ${raised.map((h) => html`
            <div class="pzhint" key=${`raise-${h.key}`}>
                <span>${h.text}</span>
                <button class="btn" type="button" onClick=${() => set(h.key, h.value)}>Make it the default</button>
            </div>
        `)}
        ${copied.map((c) => html`
            <div class="pzhint" key=${`copy-${c.key}`}>
                <span>${c.text}</span>
                <button class="btn" type="button" onClick=${() => unpin(c)}>Remove the copies</button>
            </div>
        `)}
        <${Where}
            param=${paramOf(schema, "transport")}
            draft=${draft}
            owner=${contour}
            effective=${effective}
            below=${below}
            params=${params}
            onPick=${(value) => set("transport", value)}
        />
        ${touched(draft, "transport") && html`<p class="pzhelp pznote">${followers(contour, "transport")}</p>`}
        ${LAUNCH_ORDER.map((key) => {
            const p = rowProps(key);
            if (!p.param) return null;
            const mine = own(draft, contour, key);
            return html`<${LaunchRow} key=${key} p=${p} mine=${mine} catalog=${catalog} trait=${trait} model=${model}
                strike=${strikes[key]} picker=${key === "model" && wide ? modelPicker() : null} note=${touched(draft, key) ? followers(contour, key) : ""}
                onSet=${(value) => set(key, value)} onModel=${() => setModelOpen(true)} />`;
        })}
        ${pinned.map((h) => html`
            <div class="pzhint" key=${`pin-${h.key}`}>
                <span>${h.text}</span>
                <button class="btn" type="button" onClick=${() => set(h.key, null)}>Remove</button>
            </div>
        `)}

        <div class="pfsub">account and files</div>
        <${Files} contour=${contour} draft=${draft} catalog=${catalog} setField=${setField} />

        ${order && html`
            <div class="pfsub">place among the contours</div>
            <div class="pforder">
                <span class="pfhelp">${order.index + 1} of ${order.total}</span>
                <button class="btn" type="button" disabled=${order.index === 0} onClick=${() => order.move("up")}>Up</button>
                <button class="btn" type="button" disabled=${order.index === order.total - 1}
                        onClick=${() => order.move("down")}>Down</button>
            </div>
        `}

        <button class="pfloose pzjournal" type="button" onClick=${onJournal}>
            <span class="pfloosetext">changes of the map</span>
            <span class="chev">${Icon.chevron()}</span>
        </button>

        <div class="pzdanger">
            <span class="pzdangertitle">Delete the contour</span>
            <span class="pfhelp">${contour.name === PERSONAL
                ? "the personal contour stays: the panel finds its own account by it"
                : empty
                    ? "the contour leaves the map; its account, its directory and its token stay on the host"
                    : `only an empty contour is deleted: its ${groups.length} ${plural(groups.length, "group", "groups")} go first`}</span>
            <button class="btn danger" type="button" disabled=${!empty || contour.name === PERSONAL} onClick=${async () => {
                const result = await onRemove({ kind: "profile", profile: contour });
                if (result && result.ok) onClose();
            }}>${DELETE}</button>
        </div>

        ${bar()}

        ${!wide && html`
            <${ModelSheet}
            open=${modelOpen}
            param=${paramOf(schema, "model")}
            eff=${effOf("model")}
            mine=${own(draft, contour, "model")}
            catalog=${catalog}
            contour=${contour.name}
            onPick=${(value) => set("model", value)}
            onClose=${() => setModelOpen(false)}
            bar=${bar()}
        />
        `}

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

export function ContourLayer(props) {
    return html`<${Layer} label=${props.contour.name}><${ContourSettings} ...${props} /><//>`;
}
