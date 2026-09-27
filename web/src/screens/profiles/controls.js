// The rows of a project's settings page: one per launch parameter, each drawn
// from the schema — options as chips, the model from a sheet, the environment
// as KEY = value lines and the extra arguments as words. A row shows what the
// project gets when it says nothing, marks its own value and the draft, and
// says when a running session takes the change.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";
import { COMMANDS } from "../../actions/registry.js";
import { label, liveOf, outcome, own, sourceOf, tokens, touched } from "./draft.js";

const ALIASES = (COMMANDS.model.args || []).filter((value) => value !== "default");

const FAMILY = /(opus|sonnet|haiku|fable)/i;

function familyOf(model) {
    const found = FAMILY.exec(String(model || ""));
    return found ? found[1].toLowerCase() : "";
}

function windowShort(tokensN) {
    if (!tokensN) return "";
    if (tokensN >= 1000000) return `${Math.round(tokensN / 100000) / 10}M`.replace(".0", "");
    return `${Math.round(tokensN / 1000)}K`;
}

function catalogRows(catalog) {
    return (catalog && catalog.state === "ok" && catalog.models) || [];
}

// modelRows returns what the model sheet offers: the aliases first — they
// follow the newest model of their family — then the catalogue's models, each
// a version that stays put. Without a catalogue only the aliases are there.
export function modelRows(catalog) {
    const rows = catalogRows(catalog);
    const out = ALIASES.map((alias) => {
        const behind = rows.find((row) => familyOf(row.id) === familyOf(alias));
        const wide = alias.includes("[1m]") ? "1M" : behind ? windowShort(behind.window) : "";
        return {
            value: alias,
            name: alias,
            meaning: behind
                ? `the newest ${titled(familyOf(alias))}: ${behind.name || behind.id}${wide ? ` · ${wide}` : ""}`
                : "an alias: claude picks the model behind it",
        };
    });
    for (const row of rows) {
        const wide = windowShort(row.window);
        out.push({ value: row.id, name: row.name || row.id, meaning: `this version and no other${wide ? ` · ${wide}` : ""}` });
    }
    return out;
}

function titled(word) {
    return word ? word[0].toUpperCase() + word.slice(1) : word;
}

// traitOf returns what a model takes — its efforts and whether it has Auto —
// when a session on the stream has said so; null where nobody has.
export function traitOf(traits, catalog, model) {
    if (!traits || !model) return null;
    if (traits[model]) return traits[model];
    const row = catalogRows(catalog).find((r) => familyOf(r.id) === familyOf(model));
    return (row && traits[row.id]) || null;
}

// struck returns why a model does not take an option, or "".
export function struck(param, value, trait, modelName) {
    if (!trait) return "";
    const name = titled(familyOf(modelName)) || modelName;
    if (param.key === "effort") {
        if (!trait.effort) return `${name} has no effort`;
        if ((trait.efforts || []).length > 0 && !trait.efforts.includes(value)) return `${name} does not take it`;
    }
    if (param.key === "permissionMode" && value === "auto" && !trait.autoMode) return `${name} has no Auto`;
    return "";
}

// Row is the frame of one parameter: its name, when a running session takes
// a change, the draft mark and the way back to what the layers below give.
export function Row({ param, draft, project, eff, transport, off, onUnset, children, foot }) {
    const mine = own(draft, project, param.key);
    const live = liveOf(param, transport);
    return html`
        <div class="pzrow" data-off=${off ? "1" : "0"} data-draft=${touched(draft, param.key) ? "1" : "0"}>
            <div class="pzrowhead">
                <span class="pzname">${param.label}</span>
                ${touched(draft, param.key) && html`<span class="pzdraft">not saved</span>`}
                ${live && !off && html`<span class="pzlive" data-live=${live}>${live}</span>`}
                ${mine !== null && !off && html`
                    <button class="pzback" type="button" onClick=${onUnset}
                            aria-label=${`use what ${param.label} is below this project`}>
                        ${belowText(param, eff)}
                    </button>
                `}
            </div>
            ${off
                ? html`
                    <span class="pzhelp">${off}</span>
                    ${mine !== null && html`<button class="btn" type="button" onClick=${onUnset}>Remove ${label(param, mine)}</button>`}
                `
                : html`
                    ${children}
                    ${foot}
                `}
        </div>
    `;
}

const BACK = { account: "use the account's", contour: "use the contour's", panel: "use the default" };

function belowText(param, eff) {
    return (eff && eff.below && BACK[eff.below.layer]) || "use the default";
}

// Options renders an enum or a switch as chips: every value visible, the
// chosen one filled when it is the project's own and dashed when it comes
// from below, a value the model does not take struck through with why.
export function Options({ param, eff, mine, options, why, onPick }) {
    const chosen = mine !== null ? mine : eff.value;
    const inherited = mine === null;
    return html`
        <div class="pzopts" role="group" aria-label=${param.label}>
            ${options.map((o) => {
                const on = JSON.stringify(chosen) === JSON.stringify(o.value);
                const strike = why ? why(o.value) : "";
                return html`
                    <button
                        key=${String(o.value)}
                        class="pzopt"
                        type="button"
                        aria-pressed=${on ? "true" : "false"}
                        data-inherited=${on && inherited ? "1" : "0"}
                        data-struck=${strike ? "1" : "0"}
                        title=${strike || o.meaning || undefined}
                        onClick=${() => onPick(on && !inherited ? null : o.value)}
                    >${o.label}</button>
                `;
            })}
        </div>
    `;
}

// optionsOf returns the chips of a parameter: the schema's options, On and
// Off for a switch.
export function optionsOf(param) {
    if (param.kind === "bool") return [{ value: true, label: "On" }, { value: false, label: "Off" }];
    return param.options || [];
}

// Meaning says under the chips what the chosen option means.
export function Meaning({ param, value }) {
    const option = (param.options || []).find((o) => o.value === value);
    if (!option || !option.meaning) return null;
    return html`<span class="pzhelp">${option.label}: ${option.meaning}</span>`;
}

// ModelRow shows the model as its outcome and opens the sheet of choices.
export function ModelRow({ param, eff, mine, catalog, onOpen }) {
    const value = mine !== null ? mine : eff.value;
    const row = modelRows(catalog).find((r) => r.value === value);
    return html`
        <button class="pzpick" type="button" onClick=${onOpen}>
            <span class="pzpickval" data-inherited=${mine === null ? "1" : "0"}>
                ${mine !== null ? (row ? row.name : mine) : outcome(param, eff)}
            </span>
            ${mine !== null && row && html`<span class="pzhelp">${row.meaning}</span>`}
            <span class="chev">${Icon.chevron()}</span>
        </button>
    `;
}

// ModelSheet is the list of models: a check, the name and what choosing it
// means; the last row goes back to what the layers below give. The draft's
// bar is at its foot — Save there saves the whole draft, not the model alone.
export function ModelSheet({ open, param, eff, mine, catalog, contour, onPick, onClose, bar }) {
    const rows = modelRows(catalog);
    const fromCatalogue = catalogRows(catalog).length > 0;
    const below = eff.below || { layer: "claude", value: null };
    return html`
        <${Sheet} open=${open} onClose=${onClose} label=${param.label}>
            <div class="pzsheet">
                <h3>${param.label}</h3>
                <p class="pzhelp">${fromCatalogue
                    ? `aliases follow the newest of a family; the catalogue of the host pins a version`
                    : `account ${contour}: the catalogue was not read — aliases, not checked against this account`}</p>
                <div class="pzlist" role="listbox" aria-label=${param.label}>
                    ${rows.map((r) => html`
                        <button key=${r.value} class="pzline" type="button" role="option"
                                aria-selected=${mine === r.value ? "true" : "false"}
                                onClick=${() => onPick(r.value)}>
                            <span class="pzcheck">${mine === r.value ? Icon.check() : ""}</span>
                            <span class="pzlinebody">
                                <span class="pzlinename">${r.name}</span>
                                <span class="pzhelp">${r.meaning}</span>
                            </span>
                        </button>
                    `)}
                    <button class="pzline" type="button" role="option"
                            aria-selected=${mine === null ? "true" : "false"}
                            onClick=${() => onPick(null)}>
                        <span class="pzcheck">${mine === null ? Icon.check() : ""}</span>
                        <span class="pzlinebody">
                            <span class="pzlinename">${below.layer === "claude"
                                ? "Leave it to claude"
                                : `Use ${sourceOf(below.layer)}'s value (${label(param, below.value)})`}</span>
                            <span class="pzhelp">${below.layer === "claude" ? param.unset : "the project follows it when it changes"}</span>
                        </span>
                    </button>
                </div>
                ${bar}
            </div>
        <//>
    `;
}

// TextRow edits a text: empty is "+ Add"; a text the layers below give is
// shown as what the project gets.
export function TextRow({ param, eff, mine, onSet }) {
    const [editing, setEditing] = useState(false);
    const text = mine === null ? "" : String(mine);
    if (mine === null && !editing) {
        return html`
            <div class="pztextline">
                <button class="pzadd" type="button" onClick=${() => setEditing(true)}>+ Add</button>
            </div>
        `;
    }
    return html`
        <textarea class="search pztext" rows="2" spellcheck="false" maxlength=${param.maxLen || undefined}
                  placeholder=${outcome(param, eff)}
                  value=${text}
                  onInput=${(e) => onSet(e.target.value === "" ? null : e.target.value)}
                  onBlur=${() => setEditing(false)}></textarea>
    `;
}

// EnvRows are the variables as KEY = value lines: the project's own with a
// cross, the ones from below dashed; a key typed again here overrides it.
export function EnvRows({ eff, mine, onSet }) {
    const [key, setKey] = useState("");
    const [value, setValue] = useState("");
    const [adding, setAdding] = useState(false);
    const ownEnv = mine || {};
    const all = eff.value && typeof eff.value === "object" ? eff.value : {};
    const from = eff.from || {};
    const below = Object.keys(all).filter((k) => !(k in ownEnv) && from[k] !== "project");
    const add = () => {
        const k = key.trim();
        if (!k) return;
        onSet({ ...ownEnv, [k]: value });
        setKey("");
        setValue("");
        setAdding(false);
    };
    const drop = (k) => {
        const next = { ...ownEnv };
        delete next[k];
        onSet(Object.keys(next).length > 0 ? next : null);
    };
    return html`
        <div class="pzkv">
            ${Object.entries(ownEnv).map(([k, v]) => html`
                <div class="pzkvrow" key=${k}>
                    <code class="pzkvk">${k}</code><span class="pzkveq">=</span><code class="pzkvv">${v}</code>
                    <button class="pzx" type="button" aria-label=${`remove ${k}`} onClick=${() => drop(k)}>${Icon.close()}</button>
                </div>
            `)}
            ${below.map((k) => html`
                <div class="pzkvrow" key=${k} data-inherited="1">
                    <code class="pzkvk">${k}</code><span class="pzkveq">=</span><code class="pzkvv">${all[k]}</code>
                    <span class="pzhelp">${sourceOf(from[k])}</span>
                </div>
            `)}
            ${adding
                ? html`
                    <div class="pzkvadd">
                        <input class="search" spellcheck="false" placeholder="KEY" value=${key}
                               onInput=${(e) => setKey(e.target.value)} />
                        <input class="search" spellcheck="false" placeholder="value" value=${value}
                               onInput=${(e) => setValue(e.target.value)}
                               onKeyDown=${(e) => { if (e.key === "Enter") add(); }} />
                        <button class="btn" type="button" onClick=${add}>Add</button>
                    </div>
                `
                : html`<button class="pzadd" type="button" onClick=${() => setAdding(true)}>+ Add</button>`}
        </div>
    `;
}

// ArgTokens are the extra arguments a word each: the project's list replaces
// the contour's whole, so the contour's words are shown dashed until then.
export function ArgTokens({ eff, mine, onSet }) {
    const [text, setText] = useState("");
    const list = Array.isArray(mine) ? mine : [];
    const below = mine === null && Array.isArray(eff.value) && eff.layer !== "project" ? eff.value : [];
    const add = () => {
        const words = tokens(text);
        if (words.length === 0) return;
        onSet([...list, ...words]);
        setText("");
    };
    const drop = (at) => {
        const next = list.filter((_, i) => i !== at);
        onSet(next.length > 0 ? next : null);
    };
    return html`
        <div class="pztokens">
            ${list.map((word, i) => html`
                <span class="pztoken" key=${`${i}:${word}`}>
                    <code>${word}</code>
                    <button class="pzx" type="button" aria-label=${`remove ${word}`} onClick=${() => drop(i)}>${Icon.close()}</button>
                </span>
            `)}
            ${below.map((word, i) => html`<span class="pztoken" data-inherited="1" key=${`b${i}`}><code>${word}</code></span>`)}
            <span class="pztokenadd">
                <input class="search" spellcheck="false" placeholder="+ Add" value=${text}
                       onInput=${(e) => setText(e.target.value)}
                       onKeyDown=${(e) => { if (e.key === "Enter") add(); }} />
                ${text.trim() && html`<button class="btn" type="button" onClick=${add}>Add</button>`}
            </span>
        </div>
    `;
}

const CAPS = [70, 80, 90];

// CapChips offers the usual caps and a field for another.
export function CapChips({ param, eff, mine, onSet }) {
    const value = mine !== null ? mine : eff.value;
    const [other, setOther] = useState(() => value !== null && !CAPS.includes(value));
    const options = CAPS.map((n) => ({ value: n, label: `${n}%` }));
    return html`
        <div class="pzopts" role="group" aria-label=${param.label}>
            ${options.map((o) => {
                const on = !other && value === o.value;
                return html`
                    <button key=${o.value} class="pzopt" type="button"
                            aria-pressed=${on ? "true" : "false"}
                            data-inherited=${on && mine === null ? "1" : "0"}
                            onClick=${() => { setOther(false); onSet(on && mine !== null ? null : o.value); }}>${o.label}</button>
                `;
            })}
            <button class="pzopt" type="button" aria-pressed=${other ? "true" : "false"}
                    data-inherited=${other && mine === null ? "1" : "0"}
                    onClick=${() => setOther(true)}>other</button>
            ${other && html`
                <input class="search pzpct" type="number" inputmode="numeric" min=${param.min} max=${param.max} step="1"
                       aria-label=${`${param.label}, percent`}
                       value=${value === null ? "" : String(value)}
                       onInput=${(e) => {
                           const raw = e.target.value.trim();
                           const n = Number(raw);
                           onSet(raw === "" ? null : Number.isInteger(n) ? n : raw);
                       }} />
            `}
        </div>
    `;
}
