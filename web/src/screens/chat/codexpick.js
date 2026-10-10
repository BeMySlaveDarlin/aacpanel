// How a codex thread thinks and what it may do, picked from the band under the
// field: its model, effort and plan behind one word, its permissions behind
// another. A phone opens them as sheets, a wide screen as menus over the
// composer. Each pick is one setting sent on its own, as the host takes them.
//
// The models and the efforts each takes come from the catalogue the codex
// daemon lists. The mode and the plan come from the row of the session, which
// carries them only where the daemon told them: a thread the panel only reads
// carries neither, and then they are shown as not known rather than guessed.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";
import { useToast } from "../../ui/toasts.js";
import { shortPath } from "./head.js";
import { EffortScale, effortName, PickRow } from "./picker.js";
import { GoalPane, RenamePane, ReviewPane, ThreadMenu, ThreadPane } from "./codexthread.js";

// The permission modes the panel sets a thread to, in the order the lists
// offer them. Full access is not among them: it is chosen when the thread
// starts, in the map or in codex itself.
export const CODEX_MODES = [
    { value: "read-only", name: "Read only", desc: "Reads and answers; asks before any change", icon: Icon.search, tone: "manual" },
    { value: "ask", name: "Ask", desc: "Edits in the project; asks for anything else", icon: Icon.braces, tone: "edits" },
    { value: "auto", name: "Approve for me", desc: "A second pass of codex checks each request instead of you", icon: Icon.bolt, tone: "auto" },
];

const READ_ONLY = "the panel only reads this thread";

// MENUS are the lists the words of the band open: menus over the composer on a
// wide screen. The panes they lead to are sheets there too.
const MENUS = new Set(["think", "perm", "thread"]);

// The panes of the thread open from its list, and go back to it.
const THREAD_PANES = new Set(["review", "rename", "goal"]);

// modeOf says what the row of a session knows of the permissions of a thread:
// one of the modes, settings of its own that match none of them, or nothing.
export function modeOf(mode) {
    const preset = CODEX_MODES.find((m) => m.value === mode) || null;
    if (preset) return { preset, word: preset.name, why: "" };
    if (mode === "custom") {
        return {
            preset: null,
            word: "Custom",
            why: "the thread runs with permissions of its own, set when it started — the panel does not change them",
        };
    }
    return { preset: null, word: "Unknown", why: `what codex may do is not known: ${READ_ONLY}` };
}

// useCodexPick holds what was picked until the row of the session says it,
// and sends a pick. A model sent alone keeps the effort of the thread where
// the model takes it and starts at its own otherwise, and the words say so at
// once.
export function useCodexPick(name, live, catalog) {
    const run = useAction();
    const toast = useToast();
    const [chosen, setChosen] = useState({});
    useEffect(() => { setChosen({}); }, [name, live.model, live.effort, live.mode, live.plan]);
    const now = {
        model: chosen.model || live.model || "",
        effort: chosen.effort || live.effort || "",
        mode: chosen.mode || live.mode || "",
        plan: "plan" in chosen ? chosen.plan : live.plan,
    };
    const set = async (setting, said) => {
        const result = await run("session.set", name, setting);
        if (!result.ok) return result;
        const detail = result.data && result.data.detail;
        toast(said, detail || "from the next message");
        const next = { ...setting };
        if (setting.model) {
            const found = modelsOf(catalog).find((m) => m.model === setting.model);
            if (found && !(found.efforts || []).includes(now.effort) && found.effort) next.effort = found.effort;
        }
        setChosen((was) => ({ ...was, ...next }));
        return result;
    };
    return { now, set };
}

function modelsOf(catalog) {
    return (catalog && catalog.state === "ok" && catalog.models) || [];
}

// levelsOf names the efforts the model takes, as the catalogue lists them.
function levelsOf(catalog, model) {
    const found = modelsOf(catalog).find((m) => m.model === model);
    return found ? found.efforts || [] : [];
}

function catalogNote(catalog) {
    if (!catalog) return "asking codex for its models…";
    if (catalog.state === "ok") return modelsOf(catalog).length ? "" : "codex lists no models";
    return `the models of codex were not read: ${catalog.reason || "the panel did not answer"}`;
}

// ThinkWord is how the thread thinks, in the band: the model and the effort,
// or the plan and the effort while it plans first — a phone has no room for
// the model beside it, and the header names the model anyway.
function ThinkWord({ now, wide }) {
    const effort = now.effort ? html`<span class="pkeff">· ${effortName(now.effort)}</span>` : "";
    if (now.plan === true) {
        return html`${Icon.plan()}<span class="cxword"><span class="cxplanword">Plan</span>${wide ? html`<span class="pkeff">· ${now.model}</span>` : ""}${effort}</span>`;
    }
    return html`${Icon.thinking()}<span class="cxword">${now.model || "model"}${effort}</span>`;
}

// CodexStrip is the band under the field of a codex thread: how it thinks and
// what it may do, each a word that opens its list. On a wide screen the lists
// are menus over the composer, held here.
export function CodexStrip({ wide, exec, live, pct, now, open, onOpen, onCompact, catalog, set, cwd }) {
    const can = knows(exec, "session.set");
    const why = can ? "" : whyNot(exec, "session.set");
    const mode = modeOf(now.mode);
    const box = useRef(null);
    const close = () => onOpen("");
    const menu = wide && MENUS.has(open) ? open : "";
    const models = modelsOf(catalog);
    // A pick from a menu puts it down, as claude's menus do; the plan is a
    // switch, and the menu stays to show where it stands.
    const choose = (setting, said) => { close(); return set(setting, said); };

    // The listeners go on in the commit that draws the menu, not a frame
    // later: a press outside that lands in between would leave the menu open.
    useLayoutEffect(() => {
        if (!menu) return undefined;
        const away = (event) => { if (box.current && !box.current.contains(event.target)) close(); };
        const keys = (event) => {
            if (event.key === "Escape") { close(); return; }
            const n = Number(event.key);
            if (!Number.isInteger(n) || n < 1) return;
            if (menu === "thread") {
                const kinds = ["compact", "review", "rename", "goal"];
                if (!kinds[n - 1]) return;
                event.preventDefault();
                if (kinds[n - 1] === "compact") {
                    close();
                    onCompact();
                } else {
                    onOpen(kinds[n - 1]);
                }
                return;
            }
            const one = menu === "perm" ? CODEX_MODES[n - 1] : menu === "think" ? models[n - 1] : null;
            if (!one) return;
            event.preventDefault();
            if (menu === "perm") choose({ mode: one.value }, `Permissions: ${one.name}`);
            else choose({ model: one.model }, `Model: ${one.name || one.model}`);
        };
        document.addEventListener("pointerdown", away);
        document.addEventListener("keydown", keys);
        return () => {
            document.removeEventListener("pointerdown", away);
            document.removeEventListener("keydown", keys);
        };
    }, [menu, catalog, now.model, now.effort, now.mode]);

    const toggle = (which) => onOpen(open === which ? "" : which);
    const chips = html`
        <button type="button" class=${`pkchip cxchip${menu === "think" ? " open" : ""}`} data-pick="think"
                disabled=${!can} title=${why || undefined} aria-expanded=${menu === "think" ? "true" : "false"}
                aria-label=${can ? "how codex thinks — the model, the effort and the plan" : "how codex thinks"}
                onClick=${() => toggle("think")}><${ThinkWord} now=${now} wide=${wide} /></button>
        <button type="button" class=${`pkchip cxchip${menu === "perm" ? " open" : ""}`} data-pick="perm"
                disabled=${!can || !mode.preset} title=${why || mode.why || undefined}
                aria-expanded=${menu === "perm" ? "true" : "false"}
                aria-label=${mode.preset && can ? `what codex may do: ${mode.word} — pick another` : `what codex may do: ${mode.word}`}
                onClick=${() => toggle("perm")}>${Icon.key()}<span class="cxword">${mode.word}</span></button>
        <button type="button" class=${`pkchip cxchip cxthread${menu === "thread" ? " open" : ""}`} data-pick="thread"
                aria-expanded=${menu === "thread" ? "true" : "false"} aria-label="the thread: compact, review, rename, goal"
                onClick=${() => toggle("thread")}>${Icon.list()}<span class="cxword">Thread</span></button>
    `;
    if (!wide) return chips;
    return html`
        <div class="pickbar cxbar" ref=${box}>
            ${chips}
            ${menu === "think" && html`
                <div class="pkmenu left cxmenu" role="menu" aria-label="how codex thinks">
                    <div class="pkmenuhead">How codex thinks<span class="cxwhen">from the next message</span></div>
                    <${PlanSwitch} plan=${now.plan} set=${set} menu />
                    <div class="pkmenusep"></div>
                    ${catalogNote(catalog) && html`<p class="pknote cxnote">${catalogNote(catalog)}</p>`}
                    ${models.map((m, i) => html`
                        <${PickRow} key=${m.model} on=${m.model === now.model} name=${m.name || m.model} number=${i + 1}
                                onPick=${() => choose({ model: m.model }, `Model: ${m.name || m.model}`)} />
                    `)}
                    <div class="pkmenusep"></div>
                    <div class="pkmenuhead"><span>Effort</span><b>${effortName(now.effort)}</b></div>
                    <div class="cxscale"><${Effort} catalog=${catalog} now=${now} set=${choose} /></div>
                </div>
            `}
            ${menu === "thread" && html`
                <${ThreadMenu} live=${live} pct=${pct}
                               onPick=${(kind) => (kind === "compact" ? (close(), onCompact()) : onOpen(kind))} />
            `}
            ${menu === "perm" && html`
                <div class="pkmenu left cxmenu" role="menu" aria-label="what codex may do">
                    <div class="pkmenuhead">What codex may do</div>
                    ${CODEX_MODES.map((m, i) => html`
                        <${PickRow} key=${m.value} on=${m.value === now.mode} name=${m.name} desc=${m.desc} number=${i + 1}
                                onPick=${() => choose({ mode: m.value }, `Permissions: ${m.name}`)} />
                    `)}
                    <div class="pkmenusep"></div>
                    <p class="pknote cxnote">${sandboxOf(now.mode, cwd).map(([what, how]) => `${what} ${how}`).join(" · ")}</p>
                </div>
            `}
        </div>
    `;
}

// PlanSwitch turns the plan on and off: codex asks its questions and writes a
// plan first, and changes nothing until told to go. Where the row of the
// session does not say whether the thread plans, the switch stands still and
// says so.
function PlanSwitch({ plan, set, menu = false }) {
    const known = typeof plan === "boolean";
    const sub = known
        ? (menu ? "questions and a plan, no changes" : "Asks questions with options and writes a plan; changes nothing until you say go")
        : `not known: ${READ_ONLY}`;
    const row = html`
        <button class=${`nfrow cxplan${menu ? " pkrow" : ""}`} type="button" role="switch"
                aria-checked=${plan === true ? "true" : "false"} disabled=${!known} data-lock=${known ? "0" : "1"}
                onClick=${() => set({ plan: !plan }, plan ? "Plan first: off" : "Plan first: on")}>
            <span class="pkicon t-plan">${Icon.plan()}</span>
            <span class="nfbody">
                <span class="nftitle">Plan first</span>
                <span class="nfsub">${sub}</span>
            </span>
            <span class="nfsw" aria-hidden="true"></span>
        </button>
    `;
    return menu ? row : html`<div class="pklist"><div class="cxplanbox">${row}</div></div>`;
}

// Effort is the scale of the efforts the model takes. A model the catalogue
// does not list has no levels to offer, and the line says why.
function Effort({ catalog, now, set }) {
    const levels = levelsOf(catalog, now.model);
    if (!levels.length) {
        const note = catalogNote(catalog) || `codex lists no efforts for ${now.model || "this model"}`;
        return html`<p class="pknote cxnote">${note}</p>`;
    }
    return html`<${EffortScale} levels=${levels} value=${now.effort} ultra=${false}
                                onPick=${(level) => set({ effort: level }, `Effort: ${effortName(level)}`)} />`;
}

// sandboxOf says where the sandbox of the mode ends: what codex writes without
// asking, who answers past that, and that full access is no pick of the panel.
function sandboxOf(mode, cwd) {
    const where = cwd ? shortPath(cwd) : "the directory of the thread";
    return [
        ["writes", mode === "read-only" ? "nothing" : where],
        ["beyond it", mode === "auto" ? "a second pass of codex decides" : "codex asks you"],
        ["full access", "only when a thread starts"],
    ];
}

// CodexSheets are the phone's lists behind the words of the band.
export function CodexSheets({ wide, name, live, exec, pct, open, onOpen, onClose, onCompact, now, catalog, set, cwd }) {
    const pane = wide && MENUS.has(open) ? "" : open;
    const labels = { think: "how codex thinks", perm: "what codex may do", thread: "the thread",
        review: "review", rename: "rename the thread", goal: "goal" };
    // On a phone a pane of the thread goes back to its list; a wide screen
    // opened it from a menu, and there is no list to go back to.
    const back = !wide && THREAD_PANES.has(pane) ? () => onOpen("thread") : null;
    return html`
        <${Sheet} open=${Boolean(pane)} onClose=${onClose} label=${labels[pane] || "codex"} inner>
            ${pane === "thread" && html`<${ThreadPane} live=${live} pct=${pct} onPane=${onOpen}
                                                       onCompact=${() => { onClose(); onCompact(); }} />`}
            ${pane === "review" && html`<${ReviewPane} name=${name} live=${live} exec=${exec} cwd=${cwd}
                                                       onBack=${back} onDone=${onClose} />`}
            ${pane === "rename" && html`<${RenamePane} name=${name} live=${live} exec=${exec} onBack=${back} onDone=${onClose} />`}
            ${pane === "goal" && html`<${GoalPane} name=${name} live=${live} exec=${exec} onBack=${back} onDone=${onClose} />`}
            ${pane === "think" && html`
                <div class="shead pkhead">
                    <div class="pktitles">
                        <span class="stitle">How codex thinks</span>
                        <span class="ssub">From the next message</span>
                    </div>
                </div>
                <${PlanSwitch} plan=${now.plan} set=${set} />
                <div class="pkgroup">Model</div>
                ${catalogNote(catalog) && html`<p class="hint pknote">${catalogNote(catalog)}</p>`}
                ${modelsOf(catalog).length > 0 && html`
                    <div class="pklist">
                        ${modelsOf(catalog).map((m) => html`
                            <${PickRow} key=${m.model} on=${m.model === now.model} name=${m.name || m.model}
                                    desc=${m.name && m.name !== m.model ? m.model : ""}
                                    onPick=${() => set({ model: m.model }, `Model: ${m.name || m.model}`)} />
                        `)}
                    </div>
                `}
                <div class="pkgroup">Effort · <span class="pkval">${effortName(now.effort)}</span></div>
                <${Effort} catalog=${catalog} now=${now} set=${set} />
            `}
            ${pane === "perm" && html`
                <div class="shead pkhead">
                    <div class="pktitles">
                        <span class="stitle">What codex may do</span>
                        <span class="ssub">Which of its requests come to you</span>
                    </div>
                </div>
                <div class="pklist">
                    ${CODEX_MODES.map((m) => html`
                        <${PickRow} key=${m.value} on=${m.value === now.mode} icon=${m.icon} tone=${m.tone}
                                name=${m.name} desc=${m.desc}
                                onPick=${() => set({ mode: m.value }, `Permissions: ${m.name}`)} />
                    `)}
                </div>
                <section class="cmdsec cxsandbox">
                    <div class="cmdsechead"><span>The sandbox of this thread</span></div>
                    <ul class="cmdrows mcpfacts">
                        ${sandboxOf(now.mode, cwd).map(([what, how]) => html`
                            <li key=${what}><span class="cmdname">${what}</span><span class="cmdtok">${how}</span></li>
                        `)}
                    </ul>
                </section>
            `}
        <//>
    `;
}

// useCatalog asks for what the thread can be switched to each time the list of
// how it thinks opens: the catalogue of the daemon of its own contour, which
// updates codex apart from the others. Nothing is asked while the list is
// shut, and another thread starts with nothing.
export function useCatalog(name, open) {
    const [got, setGot] = useState(null);
    const wanted = open === "think";
    useEffect(() => { setGot(null); }, [name]);
    useEffect(() => {
        if (!wanted || !name) return undefined;
        let alive = true;
        (async () => {
            let body;
            try {
                const r = await fetch(`/api/session/models?name=${encodeURIComponent(name)}`, { credentials: "same-origin" });
                body = r.ok ? await r.json() : { state: "unknown", reason: `the server answered ${r.status}` };
            } catch {
                body = { state: "unknown", reason: "the network is unavailable" };
            }
            if (alive) setGot(catalogOf(body));
        })();
        return () => { alive = false; };
    }, [name, wanted]);
    return got;
}

// catalogOf reads the models of a session's answer in the shape the lists
// draw from: the name codex takes, the one a person reads, the efforts.
function catalogOf(body) {
    if (!body || body.state !== "ok" || !body.session) {
        return { state: "unknown", reason: (body && body.reason) || "the panel did not answer" };
    }
    const list = body.session.list || [];
    return { state: "ok", models: list.map((m) => ({ model: m.value, name: m.name || m.value, efforts: m.efforts || [], effort: m.effort || "" })) };
}
