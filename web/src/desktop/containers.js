// The containers section: stacks on the left, the picked stack in the centre.
// A right click on a stack or a container opens its menu: what its buttons
// do, through the same code, and the name of a container to copy.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { bytes, pct } from "../format.js";
import { knows, whyNot } from "../exec.js";
import { useAction } from "../actions/gate.js";
import { MenuItem, MenuList, useMenu } from "../ui/menu.js";
import { useToast } from "../ui/toasts.js";
import { copyName } from "../screens/chat/copy.js";

function stateWord(c) {
    if (c.state !== "running") return c.state === "exited" ? "stopped" : c.state;
    if (c.health === "unhealthy") return "unhealthy";
    if (c.health === "starting") return "starting";
    if (c.health === "healthy") return "healthy";
    return "running";
}

function stateClass(c) {
    if (c.state !== "running") return "off";
    if (c.health === "unhealthy") return "bad";
    return "";
}

function upFor(status) {
    return String(status || "")
        .replace(/^Up /, "")
        .replace(/ \(.*\)$/, "");
}

function numClass(v, warn, crit) {
    if (v >= crit) return " dkcrit";
    if (v >= warn) return " dkwarn";
    return "";
}

// flipOf is what the last button of a container's row does: a running one
// stops, any other starts.
function flipOf(c) {
    return c.state === "running" ? "container.stop" : "container.start";
}

// StackMenu brings the stack up or down as the buttons of its head and its
// row do; down waits for something of it to run, as the row's button does.
function StackMenu({ st, exec, act }) {
    return html`<${MenuList} groups=${[[
        html`<${MenuItem} key="up" label="Bring up" off=${whyNot(exec, "stack.up")}
                          onPick=${() => act("stack.up", st.name)} />`,
        html`<${MenuItem} key="down" label="Bring down"
                          off=${whyNot(exec, "stack.down") || (st.running > 0 ? "" : "no container of the stack is running")}
                          onPick=${() => act("stack.down", st.name)} />`,
    ]]} />`;
}

// ContainerMenu is what the buttons of a container's row do, and its name to
// copy, as the phone copies it.
function ContainerMenu({ c, exec, act, onLogs }) {
    const toast = useToast();
    const flip = flipOf(c);
    return html`<${MenuList} groups=${[
        [
            html`<${MenuItem} key="restart" label="Restart" off=${whyNot(exec, "container.restart")}
                              onPick=${() => act("container.restart", c.name)} />`,
            html`<${MenuItem} key="flip" label=${c.state === "running" ? "Stop" : "Start"} off=${whyNot(exec, flip)}
                              onPick=${() => act(flip, c.name)} />`,
        ],
        [
            onLogs && html`<${MenuItem} key="logs" label="Logs" onPick=${() => onLogs(c)} />`,
            html`<${MenuItem} key="copy" label="Copy the name" onPick=${() => copyName(c.name, toast)} />`,
        ],
    ]} />`;
}

function Hp({ running, total }) {
    return html`
        <span class="dkhp">
            ${Array.from({ length: Math.min(total || 0, 16) }, (_, i) => html`
                <i key=${i} class=${i < (running || 0) ? "up" : (running === 0 ? "bad" : "down")}></i>
            `)}
        </span>
    `;
}

export function StackColumn({ tree, current, onPick, exec, onDone }) {
    const run = useAction();
    const menu = useMenu();
    const stacks = (tree && tree.stacks) || [];
    const running = stacks.filter((s) => s.running > 0).length;
    const can = knows(exec, "stack.down");
    const act = async (kind, name) => {
        const done = await run(kind, name, {});
        if (done && done.ok && onDone) onDone();
    };

    return html`
        <aside class="dkleft">
            <div class="dkcontour">
                <span class="dkcontourname">stacks</span>
                <span class="dkcontournum">${running} of ${stacks.length} running</span>
            </div>
            <div class="dkscroll">
                ${stacks.map((st) => html`
                    <button
                        key=${st.name}
                        class=${`dksess${current === st.name ? " on" : ""}`}
                        type="button"
                        style=${`--fill:${st.total ? Math.round((st.running / st.total) * 100) : 0}%`}
                        onClick=${() => onPick(st.name)}
                        onContextMenu=${menu && menu(StackMenu, { st, exec, act }, `stack ${st.name}`)}
                    >
                        <span class=${`dkdot ${st.running > 0 ? "dkok" : "dkoff"} dkside`}></span>
                        <span class="dksessbody">
                            <span class="dksessmain">
                                <span class="dkname">${st.name}</span>
                                <span class="dknum">${st.hasStats ? pct(st.cpu) : "—"}</span>
                                <span class="dkrowacts" onClick=${(e) => e.stopPropagation()}>
                                    <i
                                        class=${`dkact danger${can && st.running > 0 ? "" : " off"}`}
                                        data-tip=${can ? undefined : whyNot(exec, "stack.down")}
                                        data-tipside="left"
                                        onClick=${() => {
                                            if (!can || st.running === 0) return;
                                            act("stack.down", st.name);
                                        }}
                                    ><${Icon.stop} /></i>
                                </span>
                            </span>
                            <span class="dksesssub">
                                <span class="dkhpnum">${st.running}/${st.total}</span>
                                <span class="dklast">${st.hasStats ? bytes(st.mem) : "no samples"}</span>
                            </span>
                        </span>
                        <span class="dksessbar dksegbar">
                            ${Array.from({ length: Math.min(st.total || 0, 16) }, (_, i) => html`
                                <i key=${i} class=${i < (st.running || 0) ? "up" : (st.running === 0 ? "bad" : "down")}></i>
                            `)}
                        </span>
                    </button>
                `)}
                ${stacks.length === 0 && html`<p class="dkempty">there are no stacks</p>`}
            </div>
            <${DockerUsage} tree=${tree} />
        </aside>
    `;
}

export function DockerUsage({ tree }) {
    const stacks = (tree && tree.stacks) || [];
    const cpu = stacks.reduce((a, s) => a + (s.cpu || 0), 0);
    const mem = stacks.reduce((a, s) => a + (s.mem || 0), 0);
    const disk = stacks.reduce((a, s) => a + (s.disk || 0), 0);
    const row = (name, value, label) => html`
        <div class="dkuse">
            <span class="dkusename">${name}</span>
            <span class="dkusenum">${value}</span>
            <span class="dkuselabel">${label}</span>
        </div>
    `;
    return html`
        <div class="dklimits">
            ${row("docker", tree ? `${tree.running}/${tree.total}` : "—", "containers")}
            ${row("cpu", pct(cpu), "summed over the stacks")}
            ${row("memory", bytes(mem), "resident")}
            ${row("disk", bytes(disk), "layers and volumes")}
        </div>
    `;
}

export function ContainersCenter({ tree, stack, current, onPick, exec, onDone, onLogs }) {
    const run = useAction();
    const menu = useMenu();
    const stacks = (tree && tree.stacks) || [];
    const st = stacks.find((x) => x.name === stack) || stacks[0] || null;
    const list = (st && st.containers) || [];
    const [busy, setBusy] = useState(null);

    const act = async (kind, target) => {
        if (!knows(exec, kind)) return;
        setBusy(target);
        const done = await run(kind, target, {});
        setBusy(null);
        if (done && done.ok && onDone) onDone();
    };

    const stackBtn = (kind, icon, danger) => html`
        <i
            class=${`dkact${danger ? " danger" : ""}${knows(exec, kind) ? "" : " off"}`}
            data-tip=${knows(exec, kind) ? undefined : whyNot(exec, kind)}
            data-tipside="left"
            onClick=${() => st && act(kind, st.name)}
        >${icon}</i>
    `;

    return html`
        <section class="dkcenter">
            <div class="dkhead">
                <div class="dkheadtop">
                    <span class=${`dkdot ${st && st.running > 0 ? "dkok" : "dkoff"}`}></span>
                    <span class="dkheadname">${st ? st.name : "no stack picked"}</span>
                    <span class="dkheadpath">${st ? `${st.running} of ${st.total} running` : ""}</span>
                    <span class="dkheadacts">
                        ${stackBtn("stack.up", html`<${Icon.play} />`, false)}
                        ${stackBtn("stack.down", html`<${Icon.stop} />`, true)}
                    </span>
                </div>
                ${st && html`
                    <div class="dkheadbot">
                        <span class="dkfact"><b>${st.hasStats ? pct(st.cpu) : "—"}</b><span>cpu</span></span>
                        <span class="dkfact"><b>${st.hasStats ? bytes(st.mem) : "—"}</b><span>memory</span></span>
                        <span class="dkfact"><b>${st.hasDisk ? bytes(st.disk) : "—"}</b><span>disk</span></span>
                        <span class="dkfact"><${Hp} running=${st.running} total=${st.total} /></span>
                    </div>
                `}
            </div>

            <div class="dksplit">
                <div class="dkhalf">
                    <div class="dktable">
                        <div class="dktr dkth">
                            <span class="dkc-state"></span>
                            <span class="dkc-name">container</span>
                            <span class="dkc-status">state</span>
                            <span class="dkc-num">cpu</span>
                            <span class="dkc-num">memory</span>
                            <span class="dkc-num">disk</span>
                            <span class="dkc-acts"></span>
                        </div>
                        ${list.map((c) => html`
                            <button
                                class=${`dktr dkcont${current === c.id ? " on" : ""}${busy === c.name ? " wait" : ""}`}
                                type="button"
                                key=${c.id}
                                onClick=${() => onPick(c.id)}
                                onContextMenu=${menu && menu(ContainerMenu, { c, exec, act, onLogs }, `container ${c.name}`)}
                            >
                                <span class="dkc-state">
                                    <i class=${`dkdot ${stateClass(c) === "bad" ? "dkbad" : stateClass(c) === "off" ? "dkoff" : "dkok"}`}></i>
                                </span>
                                <span class="dkc-name">${c.name}</span>
                                <span class="dkc-status" title=${c.status}>
                                    <span class=${`dkstate ${stateClass(c)}`}>${stateWord(c)}</span>
                                    <span class="dkuptime">${upFor(c.status)}</span>
                                </span>
                                <span class=${`dkc-num${c.hasStats ? numClass(c.cpu, 60, 85) : ""}`}>${c.hasStats ? pct(c.cpu) : "—"}</span>
                                <span class=${`dkc-num${c.hasStats ? numClass(c.memPct || 0, 60, 85) : ""}`}>${c.hasStats ? bytes(c.mem) : "—"}</span>
                                <span class="dkc-num">${c.disk ? bytes(c.disk) : "—"}</span>
                                <span class="dkc-acts shown" onClick=${(e) => e.stopPropagation()}>
                                    <i
                                        class=${`dkact${knows(exec, "container.restart") ? "" : " off"}`}
                                        data-tip=${knows(exec, "container.restart") ? undefined : whyNot(exec, "container.restart")}
                                        data-tipside="left"
                                        onClick=${() => act("container.restart", c.name)}
                                    ><${Icon.resume} /></i>
                                    <i
                                        class="dkact"
                                        onClick=${() => onLogs && onLogs(c)}
                                    ><${Icon.list} /></i>
                                    <i
                                        class=${`dkact${c.state === "running" ? " danger" : ""}${knows(exec, flipOf(c)) ? "" : " off"}`}
                                        data-tip=${knows(exec, flipOf(c)) ? undefined : whyNot(exec, flipOf(c))}
                                        data-tipside="left"
                                        onClick=${() => act(flipOf(c), c.name)}
                                    >${c.state === "running" ? html`<${Icon.stop} />` : html`<${Icon.play} />`}</i>
                                </span>
                            </button>
                        `)}
                        ${list.length === 0 && html`<p class="dkempty">the stack has no containers</p>`}
                    </div>
                </div>
                <div class="dkhalf dklater">
                    <span>room for stack charts, ports and volumes</span>
                </div>
            </div>
        </section>
    `;
}
