// The model, the effort and the permission mode of a live session, picked
// from a list: sheets on a phone, menus over the composer on a wide screen.
//
// What there is to pick comes from two places. A session on the stream lists
// its models as its claude does, with the efforts each takes. A terminal lists
// nothing, and the aliases it takes are named after the catalogue of the
// account. The catalogue also gives the models no alias reaches — the older
// ones, taken by id.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { COMMANDS } from "../../actions/registry.js";
import { Sheet } from "../../ui/sheet.js";
import { useBackClose } from "../../ui/back.js";
import { knows, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { modelTitle } from "./head.js";

// Ultracode is not a level of its own: it is xhigh with workflows standing by,
// set for a session and never saved as a default. It sits past the scale,
// behind a dashed line, and only where the model takes xhigh.
export const ULTRA = "ultracode";

export const EFFORTS = COMMANDS.effort.args.filter((level) => level !== ULTRA);

export const EFFORT_NAMES = {
    low: "Low", medium: "Medium", high: "High", xhigh: "Extra", max: "Max", ultracode: "Ultracode",
};

// The modes in the order a phone lists them; a wide screen puts the one most
// sessions run in first. The two that stop a session asking at all are not
// here: they are chosen at the launch or not at all.
export const MODE_OPTIONS = [
    { value: "default", name: "Manual", desc: "Always ask before making changes", icon: Icon.hand, tone: "manual" },
    { value: "acceptEdits", name: "Accept edits", desc: "Automatically accept all file edits", icon: Icon.braces, tone: "edits" },
    { value: "plan", name: "Plan", desc: "Create a plan before making changes", icon: Icon.plan, tone: "plan" },
    { value: "auto", name: "Auto", desc: "Claude handles permission decisions", icon: Icon.bolt, tone: "auto" },
];

const DESK_MODES = ["auto", "default", "acceptEdits", "plan"];

// What a terminal takes as an alias, and which family of the catalogue names it.
const ALIASES = [
    { value: "opus[1m]", family: "opus", wide: true },
    { value: "fable", family: "fable" },
    { value: "sonnet", family: "sonnet" },
    { value: "haiku", family: "haiku" },
];

// The two modes that stop a session asking at all. Nothing here switches to
// them, but a session launched in one says so loudly wherever its mode shows.
const LOUD_MODES = { bypassPermissions: "Bypass", dontAsk: "Don't ask" };

export function modeName(mode) {
    const found = MODE_OPTIONS.find((m) => m.value === mode);
    return found ? found.name : (LOUD_MODES[mode] || mode || "mode");
}

export function modeLoud(mode) {
    return Object.hasOwn(LOUD_MODES, mode || "");
}

export function effortName(effort) {
    return EFFORT_NAMES[effort] || effort || "effort";
}

function base(id) {
    return String(id || "").replace(/\[1m\]$/i, "").replace(/-\d{8}$/, "");
}

function family(id) {
    const found = /^claude-([a-z]+)-/.exec(String(id || ""));
    return found ? found[1] : "";
}

// claude describes a model as "Opus 5.5 with 1M context · Best for everyday,
// complex tasks": the name is on the row already, the second half is the news.
function tail(description) {
    const text = String(description || "");
    const at = text.indexOf(" · ");
    return at < 0 ? text : text.slice(at + 3);
}

function title(id, fallback) {
    const named = modelTitle(id, { withWindow: false });
    return named && named !== id ? named : (fallback || id);
}

// modelChoices lays out what the model list offers: the main models, the other
// ones of the catalogue, and which of them the session runs now.
export function modelChoices(data, live) {
    const listed = (data && data.session && data.session.list) || [];
    const catalog = (data && data.catalog && data.catalog.state === "ok" && data.catalog.models) || [];
    let main = [];
    if (listed.length) {
        // claude lists its default as an entry of its own, and it stands for a
        // model another entry already names: one row per model, in its order.
        const seen = new Set();
        const named = listed.filter((m) => m.value !== "default").concat(listed.filter((m) => m.value === "default"));
        for (const m of named) {
            const key = base(m.resolved || m.value);
            if (seen.has(key)) continue;
            seen.add(key);
            main.push({ value: m.value, id: m.resolved || "", title: title(m.resolved, m.name),
                        desc: tail(m.description), efforts: m.efforts || [] });
        }
        const order = listed.map((m) => m.value);
        main.sort((a, b) => order.indexOf(a.value) - order.indexOf(b.value));
    } else {
        main = ALIASES.map((alias) => {
            const hit = catalog.find((c) => family(c.id) === alias.family);
            return { value: alias.value, id: hit ? hit.id : "", title: hit ? title(hit.id) : alias.value,
                     desc: "", efforts: EFFORTS };
        });
    }
    const taken = new Set(main.map((m) => base(m.id)).filter(Boolean));
    const other = catalog
        .filter((c) => !taken.has(base(c.id)))
        .map((c) => ({ value: c.id, id: c.id, title: title(c.id, c.name), desc: "", efforts: EFFORTS }));
    const all = main.concat(other);
    const picked = data && data.session && data.session.picked;
    const current = (picked && all.find((m) => m.value === picked))
        || all.find((m) => m.id && live && base(m.id) === base(live.model))
        || null;
    return { main, other, current };
}

// useModels asks the host what the session can be switched to, each time the
// list is opened: the mode and the effort may have changed since.
export function useModels(name, open) {
    const [data, setData] = useState(null);
    useEffect(() => {
        if (!open || !name) return undefined;
        let alive = true;
        (async () => {
            try {
                const r = await fetch(`/api/session/models?name=${encodeURIComponent(name)}`);
                const body = r.ok ? await r.json() : { state: "unknown", reason: `the server answered ${r.status}` };
                if (alive) setData(body);
            } catch (err) {
                if (alive) setData({ state: "unknown", reason: String(err.message || err) });
            }
        })();
        return () => { alive = false; };
    }, [name, open]);
    return data;
}

// pickSub says where a pick went. A terminal saves a model and an effort
// typed there as the default for new sessions, and so does the stream when
// asked to; claude keeps max and ultracode for the session alone either way.
export function pickSub(setting, transport) {
    if (setting.effort === ULTRA) return "from the next request, for this session only";
    const saved = !setting.mode && (transport === "console" || setting.scope === "default");
    if (!saved) return "from the next request";
    if (setting.effort === "max") return "from the next request; claude keeps max for this session only";
    return "from the next request, and as the default for new sessions";
}

// usePick changes one setting and says what changed. Nothing waits for the
// session to confirm: it takes the setting with its next request.
function usePick(name, onDone) {
    const run = useAction();
    const toast = useToast();
    return async (setting, said, transport) => {
        const { scope, ...value } = setting;
        const result = await run("session.set", name, setting);
        if (result.ok) {
            toast(said, pickSub(setting, transport));
            if (onDone) onDone(value);
        }
        return result;
    };
}

const SCOPES = [
    { value: "session", name: "This session" },
    { value: "default", name: "Default" },
];

// Scope says where a pick goes. On the stream claude takes a model or an
// effort for the session alone, or saves it as the default for new sessions
// as well. A terminal has no such choice to offer: whatever is typed there is
// saved as the default.
export function Scope({ transport, value, onChange, effort }) {
    if (transport === "console") {
        return html`<p class="hint pknote">In a terminal the pick is saved as the default for new sessions.</p>`;
    }
    if (transport !== "stream") return null;
    return html`
        <div class="pkscope" role="radiogroup" aria-label="where the pick goes">
            ${SCOPES.map((s) => html`
                <button key=${s.value} type="button" role="radio" class=${`pkseg${value === s.value ? " on" : ""}`}
                        aria-checked=${value === s.value ? "true" : "false"}
                        onClick=${() => onChange(s.value)}>${s.name}</button>
            `)}
        </div>
        ${effort && value === "default" && html`
            <p class="hint pknote">Ultracode is set per session, and claude keeps max for this session only.</p>
        `}
    `;
}

// scoped is what a pick carries about where it goes: only the stream is asked.
function scoped(transport, scope) {
    return transport === "stream" ? { scope } : {};
}

function transportOf(data) {
    return (data && data.session && data.session.transport) || "";
}

function Row({ on, icon, tone, name, desc, onPick, disabled, number }) {
    return html`
        <button type="button" class=${`pkrow${on ? " on" : ""}`} disabled=${disabled}
                aria-pressed=${on ? "true" : "false"} onClick=${onPick}>
            ${icon && html`<span class=${`pkicon t-${tone}`}>${icon()}</span>`}
            <span class="pkbody">
                <span class="pkname">${name}</span>
                ${desc && html`<span class="pkdesc">${desc}</span>`}
            </span>
            ${on && html`<span class="pkcheck">${Icon.check()}</span>`}
            ${number && html`<span class="pknum">${number}</span>`}
        </button>
    `;
}

// EffortScale is the effort as a scale from faster to smarter: a stop per
// level the model takes, the one the session runs at under the knob. A model
// that takes xhigh takes ultracode too, and it gets the stop past the line;
// where the pick would be saved as a default, that stop is off — ultracode is
// set per session.
export function EffortScale({ levels, value, onPick, disabled, ultraOff }) {
    const base = levels.filter((level) => level !== ULTRA);
    const stops = base.includes("xhigh") ? [...base, ULTRA] : base;
    const at = stops.indexOf(value);
    return html`
        <div class="pkscale">
            <div class="pkscalehead"><span>Faster</span><span>Smarter</span></div>
            <div class="pktrack" role="radiogroup" aria-label="effort">
                ${stops.map((level, i) => {
                    const off = disabled || (level === ULTRA && Boolean(ultraOff));
                    return html`
                        <button key=${level} type="button" role="radio" disabled=${off}
                                class=${`pkstop${level === ULTRA ? " pkultra" : ""}${i === at ? " on" : ""}${i < at ? " past" : ""}`}
                                aria-checked=${i === at ? "true" : "false"} aria-label=${effortName(level)}
                                title=${level === ULTRA && ultraOff ? ultraOff : effortName(level)}
                                onClick=${() => onPick(level)}>
                            <i></i>
                        </button>
                    `;
                })}
            </div>
            ${stops.includes(ULTRA) && html`
                <div class="pkscalefoot"><span>${value === ULTRA ? "Ultracode · " : ""}xhigh + workflows</span></div>
            `}
        </div>
    `;
}

const ULTRA_OFF = "ultracode is set per session";

// PickSheet is the phone's way in: one sheet, with the model list, the effort
// behind a row of it, and the mode list, as the native client lays them out.
export function PickSheet({ what, onClose, name, live, exec }) {
    const [pane, setPane] = useState(what);
    useEffect(() => { if (what) setPane(what); }, [what]);
    // The effort opens over the model list, inside the same sheet: the gesture
    // back takes it off and leaves the list, not the whole sheet.
    useBackClose(Boolean(what) && what !== "effort" && pane === "effort", () => setPane("model"));
    const can = knows(exec, "session.set");
    const data = useModels(name, Boolean(what));
    const [chosen, setChosen] = useState({});
    useEffect(() => { setChosen({}); }, [what, name]);
    const [scope, setScope] = useState("session");
    useEffect(() => { setScope("session"); }, [name]);
    const pick = usePick(name, (setting) => setChosen((was) => ({ ...was, ...setting })));
    const labels = { model: "select model", effort: "effort", mode: "select mode" };
    return html`
        <${Sheet} open=${Boolean(what)} onClose=${onClose} label=${labels[pane] || "settings"} inner>
            ${what && html`<${PickPane} pane=${pane} setPane=${setPane} data=${data} live=${live}
                                        chosen=${chosen} pick=${pick} off=${!can} why=${whyNot(exec, "session.set")}
                                        scope=${scope} setScope=${setScope} />`}
        <//>
    `;
}

function PickPane({ pane, setPane, data, live, chosen, pick, off, why, scope, setScope }) {
    const choices = modelChoices(data, live);
    const model = chosen.model || (choices.current && choices.current.value);
    const effort = chosen.effort || (data && data.session && data.session.effort) || live.effort;
    const mode = chosen.mode || (data && data.session && data.session.mode) || live.mode;
    const running = [...choices.main, ...choices.other].find((m) => m.value === model) || choices.current;
    const levels = (running && running.efforts && running.efforts.length ? running.efforts : EFFORTS);
    const transport = transportOf(data);
    const terminal = transport === "console";
    const refusal = off && html`<p class="hint warn pknote">${why}</p>`;
    const where = scoped(transport, scope);

    if (pane === "effort") {
        return html`
            <div class="shead pkhead">
                <button class="iconbtn pkback" type="button" aria-label="back to the models"
                        onClick=${() => setPane("model")}><span class="chev back">${Icon.chevron()}</span></button>
                <span class="stitle">Effort</span>
                <span class="pkheadval">${effortName(effort)}</span>
            </div>
            ${refusal}
            <${Scope} transport=${transport} value=${scope} onChange=${setScope} effort />
            <${EffortScale} levels=${levels} value=${effort} disabled=${off}
                            ultraOff=${where.scope === "default" ? ULTRA_OFF : ""}
                            onPick=${(level) => pick({ effort: level, ...where }, `Effort: ${effortName(level)}`, transport)} />
        `;
    }

    if (pane === "mode") {
        return html`
            <div class="shead pkhead">
                <div class="pktitles">
                    <span class="stitle">Select mode</span>
                    <span class="ssub">Choose how Claude should work</span>
                </div>
            </div>
            ${refusal}
            ${terminal && html`
                <p class="hint pknote">In a terminal the mode is switched on its own screen with shift+tab;
                    the panel does not press it.</p>
            `}
            <div class="pklist">
                ${MODE_OPTIONS.map((m) => html`
                    <${Row} key=${m.value} on=${m.value === mode} icon=${m.icon} tone=${m.tone}
                            name=${m.name} desc=${m.desc} disabled=${terminal || off}
                            onPick=${() => pick({ mode: m.value }, `Mode: ${m.name}`, transport)} />
                `)}
            </div>
        `;
    }

    return html`
        <div class="shead pkhead"><span class="stitle">Select model</span></div>
        ${refusal}
        ${data && data.state !== "ok" && html`<p class="hint warn pknote">${data.reason}</p>`}
        <${Scope} transport=${transport} value=${scope} onChange=${setScope} />
        <div class="pklist">
            ${choices.main.map((m) => html`
                <${Row} key=${m.value} on=${m.value === model} name=${m.title} desc=${m.desc} disabled=${off}
                        onPick=${() => pick({ model: m.value, ...where }, `Model: ${m.title}`, transport)} />
            `)}
        </div>
        <div class="pklist">
            <button type="button" class="pkrow pkmore" onClick=${() => setPane("effort")}>
                <span class="pkround">${Icon.clock()}</span>
                <span class="pkbody">
                    <span class="pkname">Effort</span>
                    <span class="pkdesc pkval">${effortName(effort)}</span>
                </span>
                <span class="crgo">${Icon.chevron()}</span>
            </button>
        </div>
        ${choices.other.length > 0 && html`
            <div class="pkgroup">Other models</div>
            <div class="pklist">
                ${choices.other.map((m) => html`
                    <${Row} key=${m.value} on=${m.value === model} name=${m.title} disabled=${off}
                            onPick=${() => pick({ model: m.value, ...where }, `Model: ${m.title}`, transport)} />
                `)}
            </div>
        `}
    `;
}

// PickWords are the phone's way in, inside the frame of the composer: the
// model, its effort and the mode on the left, each opening its sheet, after
// what leads the row. A host whose executor predates the list has nothing to
// change a setting with: the words stay, and nothing pretends to open.
export function PickWords({ live, exec, onPick, lead = null }) {
    const can = knows(exec, "session.set") && Boolean(onPick);
    const why = can ? "" : whyNot(exec, "session.set");
    const word = (what, label, body, loud) => html`
        <button type="button" class=${`pkchip${loud ? " crit" : ""}`} disabled=${!can} data-pick=${what}
                aria-label=${can ? `${label} — pick another` : label} title=${why || undefined}
                onClick=${() => onPick(what)}>${body}</button>
    `;
    return html`
        ${lead}
        ${word("model", "model", title(live.model || ""))}
        ${word("effort", "effort", effortName(live.effort))}
        ${word("mode", "permission mode", html`${Icon.bolt()}${modeName(live.mode)}`, modeLoud(live.mode))}
    `;
}

// PickBar is the wide screen's way in: inside the frame of the composer, the
// model, its effort and the mode on the left, each opening its menu above it,
// after what leads the row.
export function PickBar({ name, live, exec, lead = null }) {
    const [menu, setMenu] = useState("");
    const [more, setMore] = useState(false);
    const box = useRef(null);
    const data = useModels(name, Boolean(menu));
    const [chosen, setChosen] = useState({});
    const pick = usePick(name, (setting) => setChosen((was) => ({ ...was, ...setting })));
    useEffect(() => { setChosen({}); }, [name, live.model, live.effort, live.mode]);
    const [scope, setScope] = useState("session");
    useEffect(() => { setScope("session"); }, [name]);

    const choices = modelChoices(data, live);
    const model = chosen.model || (choices.current && choices.current.value);
    const shown = [...choices.main, ...choices.other].find((m) => m.value === model) || choices.current;
    const effort = chosen.effort || live.effort;
    const mode = chosen.mode || live.mode;
    const levels = shown && shown.efforts && shown.efforts.length ? shown.efforts : EFFORTS;
    const transport = transportOf(data);
    const terminal = transport === "console";
    const where = scoped(transport, scope);
    const modes = DESK_MODES.map((v) => MODE_OPTIONS.find((m) => m.value === v));

    const close = () => { setMenu(""); setMore(false); };
    const choose = async (setting, said) => {
        close();
        await pick(setting, said, transport);
    };

    useEffect(() => {
        if (!menu) return undefined;
        const away = (event) => { if (box.current && !box.current.contains(event.target)) close(); };
        const keys = (event) => {
            if (event.key === "Escape") { close(); return; }
            const n = Number(event.key);
            if (!Number.isInteger(n) || n < 1) return;
            if (menu === "mode" && modes[n - 1] && !terminal) {
                event.preventDefault();
                choose({ mode: modes[n - 1].value }, `Mode: ${modes[n - 1].name}`);
            }
            if (menu === "model" && choices.main[n - 1]) {
                event.preventDefault();
                choose({ model: choices.main[n - 1].value, ...where }, `Model: ${choices.main[n - 1].title}`);
            }
        };
        document.addEventListener("pointerdown", away);
        document.addEventListener("keydown", keys);
        return () => {
            document.removeEventListener("pointerdown", away);
            document.removeEventListener("keydown", keys);
        };
    }, [menu, data, terminal, scope]);

    const toggle = (which) => { setMore(false); setMenu(menu === which ? "" : which); };

    if (!knows(exec, "session.set")) return html`<${PickWords} live=${live} exec=${exec} lead=${lead} />`;

    return html`
        <div class="pickbar" ref=${box}>
            ${lead}
            <button type="button" class=${`pkchip${menu === "model" ? " open" : ""}`} data-pick="model"
                    aria-expanded=${menu === "model" ? "true" : "false"} onClick=${() => toggle("model")}>
                ${shown ? shown.title : title(live.model || "")}
            </button>
            <button type="button" class=${`pkchip${menu === "effort" ? " open" : ""}`} data-pick="effort"
                    aria-expanded=${menu === "effort" ? "true" : "false"} onClick=${() => toggle("effort")}>
                ${effortName(effort)}
            </button>
            <button type="button" class=${`pkchip${menu === "mode" ? " open" : ""}${modeLoud(mode) ? " crit" : ""}`} data-pick="mode"
                    aria-expanded=${menu === "mode" ? "true" : "false"} onClick=${() => toggle("mode")}>
                ${Icon.bolt()}${modeName(mode)}
            </button>

            ${menu === "mode" && html`
                <div class="pkmenu left" role="menu" aria-label="mode">
                    <div class="pkmenuhead">Mode</div>
                    ${terminal && html`<p class="pknote">In a terminal the mode is switched on its own screen with shift+tab.</p>`}
                    ${modes.map((m, i) => html`
                        <${Row} key=${m.value} on=${m.value === mode} name=${m.name} desc=${m.desc}
                                number=${i + 1} disabled=${terminal}
                                onPick=${() => choose({ mode: m.value }, `Mode: ${m.name}`)} />
                    `)}
                </div>
            `}
            ${menu === "model" && html`
                <div class="pkmenu left" role="menu" aria-label="model">
                    ${data && data.state !== "ok" && html`<p class="pknote">${data.reason}</p>`}
                    <${Scope} transport=${transport} value=${scope} onChange=${setScope} />
                    ${choices.main.map((m, i) => html`
                        <${Row} key=${m.value} on=${m.value === model} name=${m.title} number=${i + 1}
                                onPick=${() => choose({ model: m.value, ...where }, `Model: ${m.title}`)} />
                    `)}
                    ${choices.other.length > 0 && html`
                        <div class="pkmenusep"></div>
                        <button type="button" class=${`pkrow pkmorerow${more ? " open" : ""}`}
                                aria-expanded=${more ? "true" : "false"} onClick=${() => setMore(!more)}>
                            <span class="pkbody"><span class="pkname">More models</span></span>
                            <span class="crgo">${Icon.chevron()}</span>
                        </button>
                    `}
                    ${more && html`
                        <div class="pkmenu pksub" role="menu" aria-label="more models">
                            ${choices.other.map((m) => html`
                                <${Row} key=${m.value} on=${m.value === model} name=${m.title}
                                        onPick=${() => choose({ model: m.value, ...where }, `Model: ${m.title}`)} />
                            `)}
                        </div>
                    `}
                </div>
            `}
            ${menu === "effort" && html`
                <div class="pkmenu left pkeffort" role="dialog" aria-label="effort">
                    <div class="pkmenuhead"><span>Effort</span><b>${effortName(effort)}</b></div>
                    <${Scope} transport=${transport} value=${scope} onChange=${setScope} effort />
                    <${EffortScale} levels=${levels} value=${effort}
                                    ultraOff=${where.scope === "default" ? ULTRA_OFF : ""}
                                    onPick=${(level) => choose({ effort: level, ...where }, `Effort: ${effortName(level)}`)} />
                </div>
            `}
        </div>
    `;
}
