// The default launch parameters: what the panel passes claude when it raises the
// console of a profile or a project.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { COMMANDS } from "../../actions/registry.js";

export const MODELS = (COMMANDS.model.args || []).filter((value) => value !== "default");
export const EFFORTS = COMMANDS.effort.args || [];

// Ultracode is not a level of thinking but xhigh with workflows standing by;
// the list says so, the way the picker of a live session does.
function effortLabel(id) {
    return id === "ultracode" ? "ultracode · xhigh + workflows" : id;
}

function family(id) {
    const found = /^claude-([a-z]+)/.exec(String(id || ""));
    return found ? found[1] : "";
}

function windowShort(tokens) {
    if (!tokens) return "";
    if (tokens >= 1000000) return `${Math.round(tokens / 100000) / 10}M`.replace(".0", "");
    if (tokens >= 1000) return `${Math.round(tokens / 1000)}K`;
    return String(tokens);
}

// modelLabel returns the list row: the alias, the model behind it and its window.
export function modelLabel(alias, catalog) {
    const rows = (catalog && catalog.state === "ok" && catalog.models) || [];
    const want = family(`claude-${String(alias).split("[")[0]}`);
    const found = rows.find((row) => family(row.id) === want);
    if (!found) return alias;
    const parts = [alias, found.name || found.id];
    const short = windowShort(found.window);
    if (short) parts.push(short);
    return parts.join(" · ");
}

function catalogNote(catalog) {
    if (!catalog || catalog.state === "ok") return "";
    return "the model catalogue was not read — aliases are shown";
}

export const MODES = ["default", "acceptEdits", "auto", "plan"];

// Where a session lives. A terminal in tmux is what a session gets when
// nothing is said; on the stream it is answered with structure — questions,
// permissions, the model — and the console is a switch away rather than the
// place the session is.
export const TRANSPORTS = [
    ["tmux", "a terminal in tmux"],
    ["stream", "the stream — answered by the panel, no terminal"],
];

const EMPTY = {};

// clean returns what goes into the database: fields with a value and nothing else.
export function clean(launch) {
    const out = {};
    for (const [key, value] of Object.entries(launch || EMPTY)) {
        if (key === "intent" && value === "") {
            out[key] = value;
            continue;
        }
        if (value === "" || value === null || value === undefined || value === false) continue;
        if (Array.isArray(value) && value.length === 0) continue;
        if (typeof value === "object" && !Array.isArray(value) && Object.keys(value).length === 0) continue;
        out[key] = value;
    }
    return out;
}

// merged returns the project launch parameters laid over the profile ones.
export function merged(profile, project) {
    const base = clean(profile);
    const own = clean(project);
    const out = { ...base, ...own };
    if (base.env || own.env) out.env = { ...(base.env || EMPTY), ...(own.env || EMPTY) };
    return out;
}

function origin(key, profile, project) {
    if (clean(project)[key] !== undefined) return "own";
    if (clean(profile)[key] !== undefined) return "from the profile";
    return "";
}

// The percentage the context guard hook works from when nothing else is said.
export const FINALIZE_DEFAULT = 80;

// guarded says whether the map names a threshold at all: zero and junk count,
// so that what the human typed is shown back rather than quietly dropped.
function guarded(launch) {
    const at = (launch || EMPTY).finalizeAt;
    return at !== undefined && at !== null;
}

function inRange(at) {
    return Number.isInteger(at) && at >= 1 && at <= 99;
}

// parseFinalizeAt reads the percentage back from the input field: a whole
// number as typed, and zero for anything else — the launcher refuses zero and
// says so, which is better than a field that shows one number and saves another.
export function parseFinalizeAt(text) {
    const n = Number(String(text || "").trim());
    return Number.isInteger(n) ? n : 0;
}

// summary returns the parameters as one line for a collapsed card.
export function summary(launch) {
    const l = clean(launch);
    const parts = [];
    if (l.model) parts.push(l.model);
    if (l.effort) parts.push(l.effort);
    if (l.permissionMode && l.permissionMode !== "default") {
        parts.push(l.permissionMode);
    }
    if (l.remoteControl) parts.push("remote control");
    if (l.transport === "stream") parts.push("on the stream");
    if (guarded(l)) parts.push(`finalize at ${l.finalizeAt}%`);
    if (l.intent) parts.push("intent");
    const env = Object.keys(l.env || EMPTY).length;
    if (env > 0) parts.push(`${env} vars`);
    if ((l.args || []).length > 0) parts.push(`${l.args.length} args`);
    return parts.join(" · ");
}

// LaunchView renders the parameters as key-value rows with their origin.
export function LaunchView({ launch, profile }) {
    const eff = merged(profile, launch);
    const env = Object.entries(eff.env || EMPTY);
    const args = eff.args || [];
    const mutedIntent = eff.intent === "" && Boolean(clean(profile).intent);
    const rows = [
        ["model", eff.model, origin("model", profile, launch)],
        ["effort", eff.effort, origin("effort", profile, launch)],
        ["permissions", eff.permissionMode, origin("permissionMode", profile, launch)],
        ["remote control", eff.remoteControl ? "on" : "", origin("remoteControl", profile, launch)],
        ["lives in", eff.transport, origin("transport", profile, launch)],
        ["finalize at", guarded(eff) ? `${eff.finalizeAt}%` : "", origin("finalizeAt", profile, launch)],
    ];
    return html`
        <div class="pfprops">
            ${rows.map(([key, value, from]) => html`
                <div class="kv" key=${key}>
                    <span class="k">${key}</span>
                    <span class="v">
                        ${value || html`<span class="pfnone">not set</span>`}
                        ${value && from === "from the profile" && html`<span class="pffrom">from the profile</span>`}
                    </span>
                </div>
            `)}
            ${(eff.intent || mutedIntent) && html`
                <div class="kv pfline">
                    <span class="k">intent</span>
                    <span class="v pfcode">
                        ${eff.intent || "no intent: the profile intent is cancelled"}
                        ${eff.intent && origin("intent", profile, launch) === "from the profile"
                            && html`<span class="pffrom">from the profile</span>`}
                    </span>
                </div>
            `}
            ${env.length > 0 && html`
                <div class="kv pfline">
                    <span class="k">environment</span>
                    <span class="v pfcode">${env.map(([k, v]) => `${k}=${v}`).join("\n")}</span>
                </div>
            `}
            ${args.length > 0 && html`
                <div class="kv pfline">
                    <span class="k">arguments</span>
                    <span class="v pfcode">${args.join(" ")}</span>
                </div>
            `}
        </div>
    `;
}

// envText renders the environment variables for the input field.
export function envText(env) {
    return Object.entries(env || EMPTY).map(([k, v]) => `${k}=${v}`).join("\n");
}

export function parseEnv(text) {
    const out = {};
    for (const line of String(text || "").split("\n")) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) continue;
        const eq = trimmed.indexOf("=");
        if (eq <= 0) continue;
        out[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1).trim();
    }
    return out;
}

// parseArgs reads the extra arguments back from the input field.
export function parseArgs(text) {
    return String(text || "").split(/\s+/).filter(Boolean);
}

function intentValue(text, muted) {
    if (text === "" && !muted) return undefined;
    return text;
}

function intentHelp(intent, muted, fromProfile) {
    if (intent !== "") return "goes out as the first message right after the launch — to a new session and to a resumed one alike";
    if (muted) return "the profile intent is cancelled — the conversation opens empty";
    if (fromProfile) return "not set — from the profile: " + fromProfile;
    return "not set — the conversation opens empty, as before";
}

function finalizeHelp(launch, fromProfile) {
    if (guarded(launch)) {
        if (!inRange(launch.finalizeAt)) return "outside 1–99: the launcher skips the threshold and says so";
        return "works only where the account has the context guard hook from the install; without it the field does nothing";
    }
    if (fromProfile) return `unchecked — as in the profile: ${fromProfile}%`;
    return "unchecked — the session is never told to finalize";
}

// LaunchFields renders the same parameters as form fields.
// StreamBlock says what a session on the stream is: what of the launch still
// holds there, what does not reach it, and where the rest is. A choice that
// quietly drops half of what the form offers reads as a form that lies.
function StreamBlock() {
    return html`
        <div class="pfstream">
            <span class="pflabel">On the stream</span>
            <span class="pfhelp">takes effect at the next start: the session is answered in the feed — there is no terminal and no window on the host</span>
            <span class="pfhelp">holds here too: the model, the effort, the permission mode, the starting intent, the environment and finalizing</span>
            <span class="pfhelp">remote control does not reach a session on the stream — claude.ai has no terminal to attach to</span>
            <span class="pfhelp warn">the extra arguments go to <code>claude -p</code>: one only the terminal knows stops the session at its start</span>
            <span class="pfhelp">what the feed cannot do yet — /model, the screens of commands — is in the console: the ⇄ button in the conversation header moves the session there and back</span>
        </div>
    `;
}

export function LaunchFields({ value, onChange, inherited, catalog }) {
    const l = value || EMPTY;
    const parent = clean(inherited);
    const set = (patch) => onChange({ ...l, ...patch });
    const none = (key, fallback) => {
        const from = parent[key];
        if (!from) return `not set — ${fallback}`;
        return `not set — from the profile: ${from}`;
    };

    const intent = l.intent === undefined || l.intent === null ? "" : String(l.intent);
    const muted = l.intent === "";
    const stream = (l.transport || parent.transport) === "stream";

    const [atDraft, setAtDraft] = useState(() => (guarded(l) ? String(l.finalizeAt) : ""));
    const [envDraft, setEnvDraft] = useState(() => envText(l.env));
    const [argsDraft, setArgsDraft] = useState(() => (l.args || []).join(" "));

    return html`
        <label class="pffield">
            <span class="pflabel">Model</span>
            <select class="search" value=${l.model || ""} onChange=${(e) => set({ model: e.target.value })}>
                <option value="">${none("model", "claude decides")}</option>
                ${MODELS.map((id) => html`
                    <option value=${id} key=${id}>${modelLabel(id, catalog)}</option>
                `)}
            </select>
            ${catalogNote(catalog) && html`<span class="pfhelp">${catalogNote(catalog)}</span>`}
        </label>

        <label class="pffield">
            <span class="pflabel">Effort</span>
            <select class="search" value=${l.effort || ""} onChange=${(e) => set({ effort: e.target.value })}>
                <option value="">${none("effort", "claude decides")}</option>
                ${EFFORTS.map((id) => html`<option value=${id} key=${id}>${effortLabel(id)}</option>`)}
            </select>
        </label>

        <label class="pffield">
            <span class="pflabel">Permission mode</span>
            <select class="search" value=${l.permissionMode || ""}
                    onChange=${(e) => set({ permissionMode: e.target.value })}>
                <option value="">${none("permissionMode", "default")}</option>
                ${MODES.map((id) => html`<option value=${id} key=${id}>${id}</option>`)}
            </select>
            ${l.permissionMode === "bypassPermissions" && html`
                <span class="pfhelp warn">bypassPermissions — the mode is off the list; while it stands, the session asks about nothing</span>
            `}
        </label>

        <label class="pffield">
            <span class="pflabel">Where the session lives</span>
            <select class="search" value=${l.transport || ""}
                    onChange=${(e) => set({ transport: e.target.value })}>
                <option value="">${none("transport", "a terminal in tmux")}</option>
                ${TRANSPORTS.map(([id, label]) => html`<option value=${id} key=${id}>${label}</option>`)}
            </select>
        </label>
        ${stream && html`<${StreamBlock} />`}

        <label class=${`row-switch${stream ? " pfconsole" : ""}`}>
            <input type="checkbox" checked=${Boolean(l.remoteControl)}
                   onChange=${(e) => set({ remoteControl: e.target.checked })} />
            start with remote control${stream ? " — in the console only" : ""}
        </label>
        <span class="pfhelp">
            ${stream
                ? "kept for the console: it is turned on when the session moves there"
                : parent.remoteControl ? "unchecked — as in the profile: on" : "unchecked — do not turn it on"}
        </span>

        <div class="pfguard">
            <label class="row-switch">
                <input type="checkbox" checked=${guarded(l)}
                       onChange=${(e) => {
                           const at = e.target.checked ? (parent.finalizeAt || FINALIZE_DEFAULT) : undefined;
                           setAtDraft(at === undefined ? "" : String(at));
                           set({ finalizeAt: at });
                       }} />
                finalize when the context fills up
            </label>
            <input class="search pfpct" type="number" inputmode="numeric" min="1" max="99" step="1"
                   disabled=${!guarded(l)}
                   value=${guarded(l) ? atDraft : String(parent.finalizeAt || FINALIZE_DEFAULT)}
                   onInput=${(e) => { setAtDraft(e.target.value); set({ finalizeAt: parseFinalizeAt(e.target.value) }); }} />
            <span class="pfpctsign">%</span>
        </div>
        <span class=${guarded(l) && !inRange(l.finalizeAt) ? "pfhelp warn" : "pfhelp"}>
            ${finalizeHelp(l, parent.finalizeAt)}
        </span>

        <label class="pffield">
            <span class="pflabel">Starting intent</span>
            <textarea class="search" rows="2" spellcheck="false"
                      placeholder="the first message of the session — a word, a phrase or a slash command"
                      value=${intent}
                      onInput=${(e) => set({ intent: intentValue(e.target.value, muted) })}></textarea>
            <span class="pfhelp">${intentHelp(intent, muted, parent.intent)}</span>
        </label>
        ${Boolean(parent.intent) && intent === "" && html`
            <label class="row-switch">
                <input type="checkbox" checked=${muted}
                       onChange=${(e) => set({ intent: e.target.checked ? "" : undefined })} />
                do not send the profile intent
            </label>
        `}

        <label class="pffield">
            <span class="pflabel">Environment variables</span>
            <textarea class="search" rows="3" spellcheck="false"
                      placeholder="KEY=value, one per line"
                      value=${envDraft}
                      onInput=${(e) => { setEnvDraft(e.target.value); set({ env: parseEnv(e.target.value) }); }}></textarea>
        </label>

        <label class="pffield">
            <span class="pflabel">Extra arguments</span>
            <input class="search" spellcheck="false" placeholder="--verbose"
                   value=${argsDraft}
                   onInput=${(e) => { setArgsDraft(e.target.value); set({ args: parseArgs(e.target.value) }); }} />
            <span class="pfhelp">split on spaces: there are no quotes and no escaping here</span>
        </label>
    `;
}
