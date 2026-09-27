// What the settings pages of a contour and of a project share: the draft that
// is asked of the service as it changes, where the session lives as two
// cards, a row per launch parameter, the one bar of the draft, the question
// before a draft is left and the frame of the page on a wide screen.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Sheet } from "../../ui/sheet.js";
import { useWide } from "../../ui/wide.js";
import { body, consequences, count, emptyDraft, exit, label, outcome, own, revert, sourceOf, touched, valueOf } from "./draft.js";
import {
    ArgTokens, CapChips, EnvRows, Meaning, ModelRow, Options, Row, TextRow, optionsOf, struck,
} from "./controls.js";
import { paramOf } from "./schema.js";

export const LAUNCH_ORDER = [
    "model", "effort", "permissionMode", "remoteControl", "intent",
    "contextCap", "autoRestart", "restartIntent", "env", "args",
];

// usePreview asks the service what a draft would come to if it were saved:
// the effective values, what the launch would refuse and, for a project, the
// command. With nothing asked, the stored answer stands. stamp names what the
// draft lies over, so a Save that changes it asks again.
export function usePreview(kind, id, stamp, ask) {
    const [got, setGot] = useState(null);
    const key = ask ? JSON.stringify(ask) : "";
    const url = kind === "project" ? `/api/projects/${id}/preview` : `/api/profiles/${id}/preview`;
    useEffect(() => {
        if (!ask) {
            setGot(null);
            return undefined;
        }
        const ctl = new AbortController();
        const timer = setTimeout(async () => {
            try {
                const response = await fetch(url, {
                    method: "POST",
                    credentials: "same-origin",
                    headers: { "Content-Type": "application/json" },
                    body: key,
                    signal: ctl.signal,
                });
                const text = await response.text();
                if (!response.ok) {
                    setGot({ error: text.trim() || `the server answered ${response.status}`, key });
                    return;
                }
                setGot({ ...JSON.parse(text), key });
            } catch (err) {
                if (err.name !== "AbortError") setGot({ error: "the preview did not come: the network is unavailable", key });
            }
        }, 180);
        return () => {
            clearTimeout(timer);
            ctl.abort();
        };
    }, [url, key, stamp]);
    return got && got.key === key ? got : null;
}

// launchAsk returns the launch keys of a draft as the preview is asked them,
// with the fields named that change what it answers; null for none.
export function launchAsk(draft, fields = []) {
    const out = body(draft);
    const ask = {};
    if (out.launchSet) ask.launchSet = out.launchSet;
    if (out.launchUnset) ask.launchUnset = out.launchUnset;
    for (const name of fields) {
        if (name in out) ask[name] = out[name];
    }
    return Object.keys(ask).length > 0 ? ask : null;
}

// Where renders the two places a session lives, the chosen one first in
// weight: under it, what living there means.
export function Where({ param, draft, owner, effective, below, params, onPick }) {
    const mine = own(draft, owner, "transport");
    const eff = valueOf(effective, "transport");
    const chosen = mine !== null ? mine : eff.value || "tmux";
    const inherited = mine === null;
    const from = valueOf(below, "transport");
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
                            ${on && inherited && html`<span class="pzcardfrom">${from.layer === "claude" ? "default" : sourceOf(from.layer)}</span>`}
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
export function Bar({ changes, problem, busy, onSave, onDiscard, onExit }) {
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
export function problemOf({ draft, params, preview, blocked, fields }) {
    for (const p of fields) {
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

// modelHolds says what the model does not take: a value the page's owner
// stores itself holds Save, one from below is said under its row and starts
// without it.
export function modelHolds({ schema, draft, owner, effective, trait, model }) {
    const blocked = [];
    const strikes = {};
    for (const key of ["effort", "permissionMode"]) {
        const param = paramOf(schema, key);
        const value = valueOf(effective, key).value;
        const why = param && value !== null ? struck(param, value, trait, model.value) : "";
        if (!why) continue;
        if (own(draft, owner, key) !== null) {
            blocked.push({ text: `${why} — ${label(param, value)} would not start`, exit: exit(draft, param, key) });
        } else {
            const from = sourceOf(valueOf(effective, key).layer);
            strikes[key] = key === "permissionMode"
                ? `${why}: ${label(param, value)} from ${from} would start as Manual`
                : `${why}: ${label(param, value)} from ${from} is dropped at the start`;
        }
    }
    return { blocked, strikes };
}

// LaunchRow is one launch parameter drawn by its kind.
export function LaunchRow({ p, mine, catalog, trait, model, strike, note, onSet, onModel }) {
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
        control = html`<${EnvRows} eff=${eff} mine=${mine} layer=${p.layer} onSet=${onSet} />`;
    } else if (param.kind === "tokens") {
        control = html`<${ArgTokens} eff=${eff} mine=${mine} layer=${p.layer} onSet=${onSet} />`;
        foot = html`<span class="pzhelp">${param.help}</span>`;
    }
    return html`<${Row} ...${p} foot=${html`${foot}${note && html`<span class="pzhelp pznote">${note}</span>`}`}>${control}<//>`;
}

// useDraft holds a page's draft and asks before it is left behind: by the
// arrow, by Escape on a wide screen, and — through hold, which the page gives
// its back handler — by the back gesture. A sheet open over the page takes
// the key first.
export function useDraft(onClose, sheetOpen) {
    const [draft, setDraft] = useState(emptyDraft);
    const [leaving, setLeaving] = useState(false);
    const leave = () => {
        if (sheetOpen || leaving) return;
        if (count(draft) > 0) setLeaving(true);
        else onClose();
    };
    const hold = () => {
        if (count(draft) === 0) return false;
        setLeaving(true);
        return true;
    };
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
    return { draft, setDraft, leaving, setLeaving, leave, hold };
}

// LeaveSheet asks what to do with a draft the person is leaving.
export function LeaveSheet({ open, changes, problem, busy, onStay, onDiscard, onSave }) {
    return html`
        <${Sheet} open=${open} onClose=${onStay} label="changes not saved">
            <div class="pzleave">
                <h3>${changes} ${changes === 1 ? "change" : "changes"} not saved</h3>
                ${problem && html`<p class="pfhelp warn">${problem.text}</p>`}
                <div class="btnrow">
                    <button class="btn" type="button" onClick=${onDiscard}>Discard</button>
                    <button class="btn primary" type="button" disabled=${busy || Boolean(problem)} onClick=${onSave}>Save</button>
                </div>
            </div>
        <//>
    `;
}

// Layer is a settings page as a layer: full screen on the phone, a dialog on
// a wide screen.
export function Layer({ label: name, children }) {
    const wide = useWide();
    if (!wide) return html`<div class="pzpage">${children}</div>`;
    return html`
        <div class="dkscrim">
            <div class="dkmodal pzmodal" role="dialog" aria-modal="true" aria-label=${name}
                 onClick=${(event) => event.stopPropagation()}>
                <div class="dkmodalbody">${children}</div>
            </div>
        </div>
    `;
}
