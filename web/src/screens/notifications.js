// The notifications screen: whether this device gets pushes, and which news
// reaches the phone at all. The choice is kept once for every device and is
// laid out by kind of news — what happened — with the places a kind comes
// from as chips under it. Each press is saved at once.
import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Sheet } from "../ui/sheet.js";
import { Trouble } from "../ui/trouble.js";
import { useToast } from "../ui/toasts.js";
import { useAction } from "../actions/gate.js";
import { usePush } from "../push.js";
import { load, save } from "../data/pushprefs.js";

// The rule that a whole stack is down never pushes: the stack pushes the same
// fall itself, a quarter of an hour sooner.
const STACK_RULE = "stack.down";

export function Notifications({ onBack }) {
    useBackClose(true, onBack);
    const { state, change } = usePrefs();

    return html`
        <${BackHead} onBack=${onBack} label="to the alerts">
            <h2>Notifications</h2>
            <span class="where">what reaches the phone</span>
        <//>

        <${Device} />

        ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
        ${state.kind === "failed" && html`
            <${Trouble} what="the choice of pushes" error=${state.error}
                hint="while the choice cannot be read every push goes out — nothing is held back." />
        `}
        ${state.kind === "ready" && html`
            <${Choice} prefs=${state.prefs} sources=${state.sources} change=${change} />
        `}
    `;
}

// usePrefs reads the choice and saves every change of it whole, at once. The
// screen shows the change before the service has it; a change the service
// refuses is taken back to what it last kept, with a note. Saves go one after
// another, so the last press is the one that stays.
function usePrefs() {
    const toast = useToast();
    const [state, setState] = useState({ kind: "loading" });
    const now = useRef(null);
    const kept = useRef(null);
    const line = useRef(Promise.resolve());
    const turn = useRef(0);

    useEffect(() => {
        let alive = true;
        load()
            .then(({ prefs, sources }) => {
                if (!alive) return;
                now.current = kept.current = whole(prefs);
                setState({ kind: "ready", prefs: now.current, sources });
            })
            .catch((err) => alive && setState({ kind: "failed", error: String(err.message || err) }));
        return () => {
            alive = false;
        };
    }, []);

    const show = (prefs) => {
        now.current = prefs;
        setState((cur) => ({ ...cur, prefs }));
    };

    const change = useCallback((edit) => {
        const next = edit(now.current);
        const mine = ++turn.current;
        show(next);
        line.current = line.current.then(() => save(next)).then(
            (saved) => {
                kept.current = whole(saved);
                if (mine === turn.current) show(kept.current);
            },
            (err) => {
                if (mine !== turn.current) return;
                show(kept.current);
                toast("The choice was not saved", String(err.message || err), true);
            },
        );
    }, [toast]);

    return { state, change };
}

function whole(prefs) {
    const p = prefs || {};
    return {
        off: p.off || [],
        rules: p.rules || [],
        sessions: p.sessions || [],
        stacks: p.stacks || [],
        limits: p.limits || [],
    };
}

function flip(list, key) {
    return list.includes(key) ? list.filter((k) => k !== key) : [...list, key];
}

// tail names a directory nobody on the map is called by: its last part.
function tail(path) {
    return String(path).replace(/\/+$/, "").split("/").pop() || path;
}

// Device is this browser's own subscription: the one thing here that is not
// the same on every device.
function Device() {
    const push = usePush();
    const run = useAction();
    const name = useDeviceName();
    const shared = "the choice below is the same on every device";

    const disable = async () => {
        const result = await run("push.disable", "this device", {});
        if (result.ok) await push.forget();
    };

    if (push.state === "unsupported") {
        return html`
            <section class="card nfcard nfdevice">
                <div class="nfrow">
                    <span class="nfbody">
                        <span class="nftitle"><span class="dot off"></span>This browser cannot get pushes</span>
                        <span class="nfsub">the panel has to be watched by hand here · ${shared}</span>
                    </span>
                </div>
            </section>
        `;
    }

    const on = push.state === "on";
    const denied = push.state === "denied";
    const sub = denied
        ? "the permission was denied in the browser: it comes back only in the site settings, the app cannot ask again"
        : name ? `${name} · ${shared}` : shared;

    return html`
        <section class="card nfcard nfdevice">
            <button
                class="nfrow"
                type="button"
                role="switch"
                aria-checked=${on ? "true" : "false"}
                data-lock=${denied ? "1" : "0"}
                disabled=${denied || push.busy}
                onClick=${on ? disable : push.enable}
            >
                <span class="nfbody">
                    <span class="nftitle">
                        <span class=${`dot ${on ? "ok" : denied ? "crit" : "off"}`}></span>
                        ${on ? "This device gets them" : denied ? "Blocked in this browser" : "This device does not get them"}
                    </span>
                    <span class="nfsub">${sub}</span>
                </span>
                <span class="nfsw" aria-hidden="true"></span>
            </button>
            ${on && html`
                <div class="btnrow">
                    <button class="btn" type="button" disabled=${push.busy}
                            onClick=${() => run("push.test", "subscribed devices", {})}>Send a test</button>
                </div>
            `}
            ${push.error && html`<p class="hint crit">${push.error}</p>`}
        </section>
    `;
}

// useDeviceName is the name this device was registered under, or "" when the
// list of devices does not say which one is this.
function useDeviceName() {
    const [name, setName] = useState("");
    useEffect(() => {
        let alive = true;
        fetch("/api/devices", { credentials: "same-origin" })
            .then((r) => (r.ok ? r.json() : null))
            .then((body) => {
                const mine = body && (body.devices || []).find((d) => d.id === body.current);
                if (alive && mine) setName(mine.name || "");
            })
            .catch(() => {});
        return () => {
            alive = false;
        };
    }, []);
    return name;
}

// Choice is every kind of news with its switch. The sources come from the
// service with the choice: the contours of the map with their projects, the
// stacks the panel sees, the rules and the probes.
function Choice({ prefs, sources, change }) {
    const [picking, setPicking] = useState("");
    const contours = sources.contours || [];
    const rules = sources.rules || [];
    const probes = sources.probes || [];

    const heard = (kind) => !prefs.off.includes(kind);
    const kind = (id) => () => change((p) => ({ ...p, off: flip(p.off, id) }));
    const listed = (list) => (key) => change((p) => ({ ...p, [list]: flip(p[list], key) }));

    const pushing = rules.filter((r) => r.key !== STACK_RULE);
    const loud = pushing.filter((r) => !prefs.rules.includes(r.id));

    // The sheets stand inside a box of the screen's own: a page at a desk
    // narrows every box it holds directly, and a scrim narrowed with it would
    // leave the sides of the window pressable under an open sheet.
    return html`
        <div>
            <div class="nfgroups">
                <div class="nfgroup">
                    <div class="grouphead">needs you</div>
                    <section class="card nfcard">
                        <${Always} title="A session calls you" sub="it was asked to — always comes" />
                        <${Kind} title="A question" on=${heard("ask")} onFlip=${kind("ask")} />
                        <${Kind} title="Waits for a permission" on=${heard("wait")} onFlip=${kind("wait")} />
                        <${Kind} title="A brief is ready" on=${heard("brief")} onFlip=${kind("brief")} />
                    </section>
                </div>

                <div class="nfgroup">
                    <div class="grouphead">sessions</div>
                    <section class="card nfcard">
                        <${Kind} title="A long turn finished" sub="10 min and longer" on=${heard("done")} onFlip=${kind("done")} />
                        <${From}
                            word="from"
                            what="sessions"
                            idle=${!heard("done") && !heard("gone")}
                            chips=${sessionChips(prefs.sessions, contours)}
                            onFlip=${listed("sessions")}
                            add="+ project"
                            onAdd=${() => setPicking("project")}
                        />
                        <${Kind} title="A session closed by itself" sub="from the same places" on=${heard("gone")} onFlip=${kind("gone")} />
                    </section>
                </div>

                <div class="nfgroup">
                    <div class="grouphead">containers</div>
                    <section class="card nfcard">
                        <${Kind} title="A stack went down" sub="one push for the stack, not one per container"
                                 on=${heard("stack")} onFlip=${kind("stack")} />
                        <${Kind} title="A container fell or is unhealthy" sub="while the rest of its stack runs"
                                 on=${heard("container")} onFlip=${kind("container")} />
                        <${Kind} title="…and when it is back" on=${heard("back")} onFlip=${kind("back")} />
                        <${From}
                            word="not from"
                            what="stacks"
                            idle=${!heard("stack") && !heard("container")}
                            chips=${prefs.stacks.map((s) => ({ key: s, name: s, on: false }))}
                            onFlip=${listed("stacks")}
                            add="+ stack"
                            onAdd=${() => setPicking("stack")}
                        />
                    </section>
                </div>

                <div class="nfgroup">
                    <div class="grouphead">the host</div>
                    <section class="card nfcard">
                        <button class="nfrow" type="button" onClick=${() => setPicking("rules")}>
                            <span class="nfbody">
                                <span class="nftitle">Rules</span>
                                <span class="nfsub nfline">${ruleCount(loud, pushing)}</span>
                            </span>
                            <span class="nfmore">choose ›</span>
                        </button>
                        <${Kind} title="Probes stop answering" sub=${probeLine(probes)} on=${heard("probe")} onFlip=${kind("probe")} />
                        <${Kind} title="A claude limit past 80%" on=${heard("limit")} onFlip=${kind("limit")} />
                        <${From}
                            word="from"
                            what="limits"
                            idle=${!heard("limit")}
                            chips=${contours.map((c) => ({ key: c.configDir, name: c.name, on: !prefs.limits.includes(c.configDir) }))}
                            onFlip=${listed("limits")}
                        />
                        <${Always} title="The panel went blind" sub="otherwise a failure passes in silence" />
                    </section>
                </div>
            </div>

            <${RulesSheet} open=${picking === "rules"} rules=${rules} off=${prefs.rules}
                           onFlip=${listed("rules")} onClose=${() => setPicking("")} />
            <${ProjectSheet} open=${picking === "project"} contours=${contours} off=${prefs.sessions}
                             onPick=${(path) => { setPicking(""); listed("sessions")(path); }}
                             onClose=${() => setPicking("")} />
            <${StackSheet} open=${picking === "stack"} stacks=${sources.stacks || []} off=${prefs.stacks}
                           onPick=${(stack) => { setPicking(""); listed("stacks")(stack); }}
                           onClose=${() => setPicking("")} />
        </div>
    `;
}

// sessionChips are the places a session is heard from: every contour of the
// map, heard or not, and every directory that is not heard — a project picked
// here, or a place a push was quieted from.
function sessionChips(off, contours) {
    const dirs = new Map(contours.map((c) => [c.configDir, c.name]));
    const projects = new Map(contours.flatMap((c) => (c.projects || []).map((p) => [p.path, p.name])));
    return [
        ...contours.map((c) => ({ key: c.configDir, name: c.name, on: !off.includes(c.configDir) })),
        ...off.filter((key) => !dirs.has(key)).map((key) => ({ key, name: projects.get(key) || tail(key), on: false })),
    ];
}

function ruleCount(loud, all) {
    const names = loud.map((r) => r.name).join(", ");
    return `${loud.length} of ${all.length}${names ? ` · ${names}` : ""}`;
}

function probeLine(probes) {
    if (probes.length === 0) return "no probes are set up";
    return `${probes.length} · ${probes.map((p) => p.name).join(", ")}`;
}

// Kind is a row with a switch: the whole row is the switch.
function Kind({ title, sub, on, onFlip }) {
    return html`
        <button class="nfrow" type="button" role="switch" aria-checked=${on ? "true" : "false"} onClick=${onFlip}>
            <span class="nfbody">
                <span class="nftitle">${title}</span>
                ${sub && html`<span class="nfsub nfline">${sub}</span>`}
            </span>
            <span class="nfsw" aria-hidden="true"></span>
        </button>
    `;
}

// Always is a kind that cannot be turned off, drawn as a switch that is on and
// does not move.
function Always({ title, sub }) {
    return html`
        <button class="nfrow" type="button" role="switch" aria-checked="true" data-lock="1" disabled>
            <span class="nfbody">
                <span class="nftitle">${title}</span>
                <span class="nfsub">${sub}</span>
            </span>
            <span class="nflock">always</span>
            <span class="nfsw" aria-hidden="true"></span>
        </button>
    `;
}

// From is the chips of where a kind comes from: a chip that is not heard is
// struck through, and a press turns it the other way. The chips stand quiet
// when every kind they choose for is off.
function From({ word, what, idle, chips, onFlip, add, onAdd }) {
    return html`
        <div class="nffrom" data-idle=${idle ? "1" : "0"} role="group" aria-label=${`${what}: ${word}`}>
            <span class="nffromword">${word}</span>
            <div class="nfchips">
                ${chips.map((c) => html`
                    <button key=${c.key} class="nfchip" type="button" title=${c.key}
                            aria-pressed=${c.on ? "true" : "false"} onClick=${() => onFlip(c.key)}>${c.name}</button>
                `)}
                ${add && html`<button class="nfchip nfadd" type="button" onClick=${onAdd}>${add}</button>`}
            </div>
        </div>
    `;
}

// RulesSheet is every rule with its switch. The rule that a whole stack is
// down is shown off and locked; a rule that is itself off is dimmed — the
// choice stands for when it is turned on again.
function RulesSheet({ open, rules, off, onFlip, onClose }) {
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="rules">
            <div class="shead">
                <div>
                    <div class="stitle">Rules</div>
                    <div class="ssub">a rule that does not push still opens its alert on the Alerts screen</div>
                </div>
            </div>
            <div class="nfcard nflist">
                ${rules.length === 0 && html`<p class="hint">There are no rules.</p>`}
                ${rules.map((rule) => (rule.key === STACK_RULE
                    ? html`
                        <button key=${rule.id} class="nfrow" type="button" role="switch" aria-checked="false"
                                data-lock="1" data-rule=${rule.key} disabled>
                            <span class="nfbody">
                                <span class="nftitle">${rule.name}</span>
                                <span class="nfsub">never pushes: the stack pushes that itself</span>
                            </span>
                            <span class="nflock">never</span>
                            <span class="nfsw" aria-hidden="true"></span>
                        </button>
                    `
                    : html`
                        <button key=${rule.id} class="nfrow" type="button" role="switch"
                                aria-checked=${off.includes(rule.id) ? "false" : "true"}
                                data-rule=${rule.key} data-dim=${rule.enabled ? "0" : "1"}
                                onClick=${() => onFlip(rule.id)}>
                            <span class="nfbody">
                                <span class="nftitle">${rule.name}</span>
                                ${!rule.enabled && html`<span class="nfsub">the rule is off</span>`}
                            </span>
                            <span class="nfsw" aria-hidden="true"></span>
                        </button>
                    `))}
            </div>
        <//>
    `;
}

// ProjectSheet offers the projects of the map to be quieted, contour by
// contour. A project already quiet is not offered again, and one whose
// contour is quiet is shown but not pressed: it is quiet with its contour.
function ProjectSheet({ open, contours, off, onPick, onClose }) {
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="quiet a project">
            <div class="shead">
                <div>
                    <div class="stitle">Quiet a project</div>
                    <div class="ssub">its long turns and closings stop pushing; what needs you still comes</div>
                </div>
            </div>
            <div class="picklist nfpick">
                ${contours.map((c) => {
                    const left = (c.projects || []).filter((p) => !off.includes(p.path));
                    if (left.length === 0) return null;
                    const quiet = off.includes(c.configDir);
                    return html`
                        <div class="grouphead" key=${c.configDir}>${c.name}</div>
                        ${left.map((p) => html`
                            <button key=${p.path} class="item" type="button" disabled=${quiet} onClick=${() => onPick(p.path)}>
                                <span>${p.name}</span>
                                <span class="pickhint">${quiet ? "quiet with its contour" : p.path}</span>
                            </button>
                        `)}
                    `;
                })}
                ${contours.every((c) => (c.projects || []).every((p) => off.includes(p.path)))
                    && html`<p class="hint">There is no project left to quiet.</p>`}
            </div>
        <//>
    `;
}

// StackSheet offers the stacks the panel sees that are not quiet yet.
function StackSheet({ open, stacks, off, onPick, onClose }) {
    const left = stacks.filter((s) => !off.includes(s));
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="quiet a stack">
            <div class="shead">
                <div>
                    <div class="stitle">Quiet a stack</div>
                    <div class="ssub">its falls and recoveries stop pushing</div>
                </div>
            </div>
            <div class="picklist nfpick">
                ${left.map((s) => html`
                    <button key=${s} class="item" type="button" onClick=${() => onPick(s)}>${s}</button>
                `)}
                ${left.length === 0 && html`
                    <p class="hint">${stacks.length === 0 ? "The panel sees no stacks." : "Every stack is quiet already."}</p>
                `}
            </div>
        <//>
    `;
}
