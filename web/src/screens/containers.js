// The “Containers” tab: stacks as cards, dense two-storey rows inside them.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { bytes } from "../format.js";
import { Icon } from "../ui/icons.js";
import { Sheet } from "../ui/sheet.js";
import { useToast } from "../ui/toasts.js";
import { Metric } from "../ui/metric.js";
import { Trouble } from "../ui/trouble.js";
import { SegBar } from "../ui/bar.js";
import { useAction } from "../actions/gate.js";
import { knows, whyNot } from "../exec.js";

const NO_STACK = "no stack";
const NO_STACK_LABEL = "No stack";

// stackLabel returns what a stack is called on the screen.
export function stackLabel(name) {
    return name === NO_STACK ? NO_STACK_LABEL : name;
}

function down(stack) {
    return stack.containers.filter((c) => c.state !== "running").length;
}

function up(stack) {
    return stack.containers.filter((c) => c.state === "running").length;
}

export const WORD = { ok: "Running", warn: "partly", crit: "broken", off: "stopped" };

// containerState returns the indicator state of one container.
export function containerState(c) {
    if (c.state === "restarting") return "crit";
    if (c.state !== "running") return "off";
    if (c.health === "unhealthy") return "crit";
    if (c.health === "starting") return "warn";
    return "ok";
}

// stackState returns the indicator state of a whole stack.
export function stackState(stack) {
    const states = stack.containers.map(containerState);
    if (states.includes("crit")) return "crit";
    if (states.every((s) => s === "off")) return "off";
    if (states.includes("warn") || states.includes("off")) return "warn";
    return "ok";
}

export const FILTERS = {
    all: () => true,
    running: (c) => c.state === "running",
    down: (c) => c.state !== "running",
    unhealthy: (c) => c.health === "unhealthy" || c.state === "restarting",
};

export function filterChips(tree) {
    const all = ((tree && tree.stacks) || []).flatMap((s) => s.containers);
    const count = (id) => all.filter(FILTERS[id]).length;
    return [
        { id: "all", label: "All", count: all.length },
        { id: "running", label: "Running", count: count("running") },
        { id: "down", label: "Down", count: count("down") },
        { id: "unhealthy", label: "Unhealthy", count: count("unhealthy") },
    ];
}

// hostUsage returns how much of the host the containers eat in total.
export function hostUsage(tree, host) {
    const all = ((tree && tree.stacks) || []).flatMap((s) => s.containers);
    if (!all.length || !host) return null;
    const cpu = all.reduce((sum, c) => sum + (c.cpu || 0), 0);
    const mem = all.reduce((sum, c) => sum + (c.mem || 0), 0);
    const cores = host.cpus || 0;
    const memTotal = (host.mem && host.mem.total) || 0;
    return {
        cpu, cores, mem, memTotal,
        cpuPct: cores > 0 ? cpu / cores : null,
        memPct: memTotal > 0 ? (mem / memTotal) * 100 : null,
    };
}

export function Containers({ tree, error, filter, query, open, onToggle, onLogs, onDone, exec }) {
    const [details, setDetails] = useState(null);
    const toast = useToast();
    const run = useAction();

    if (error) {
        return html`<${Trouble}
            what="the container tree"
            error=${error}
            hint="socket-proxy or docker is not answering. Live host data and sessions keep working."
        />`;
    }
    if (!tree) return html`<p class="empty">The container tree has not arrived yet.</p>`;

    const match = FILTERS[filter] || FILTERS.all;
    const needle = query.trim().toLowerCase();

    const stacks = tree.stacks
        .map((stack) => ({
            ...stack,
            kind: stackState(stack),
            use: usage(stack),
            shown: stack.containers.filter((c) => match(c) && hit(stack, c, needle)),
        }))
        .filter((stack) => stack.shown.length > 0);

    const nothing = stacks.length === 0;

    const forced = filter !== "all" || needle !== "";

    return html`
        ${nothing && html`<p class="empty">Nothing matched the filter.</p>`}
        ${stacks.map((stack) => html`
            <section
                class="stack"
                key=${stack.name}
                data-open=${forced || open.has(stack.name) ? "1" : "0"}
                data-kind=${stack.kind}
            >
                <${SegBar} total=${stack.containers.length} done=${up(stack)} kind=${stack.kind} />
                <div class="stackhead">
                    <button class="stacktoggle" type="button" onClick=${() => onToggle(stack.name)} disabled=${forced}>
                        <span class="dot ${stack.kind}" title=${WORD[stack.kind]}></span>
                        <span class="stackcol">
                            <span class="stackname">${stackLabel(stack.name)}</span>
                            ${stack.use && html`<span class="stackuse">${stack.use}</span>`}
                        </span>
                        <span class="chev">${Icon.chevron()}</span>
                    </button>
                    <div class="stackacts">
                    ${stack.name !== NO_STACK && down(stack) > 0 && html`
                        <button
                            class="iconbtn accent"
                            type="button"
                            aria-label=${`bring up stack ${stack.name}`}
                            disabled=${!knows(exec, "stack.up")}
                            title=${knows(exec, "stack.up") ? `bring up ${down(stack)} of ${stack.containers.length}` : whyNot(exec, "stack.up")}
                            onClick=${async () => {
                                const result = await run("stack.up", stack.name, {});
                                if (result.ok && onDone) onDone();
                            }}
                        >${Icon.play()}</button>
                    `}
                    ${stack.name !== NO_STACK && up(stack) > 0 && html`
                        <button
                            class="iconbtn danger"
                            type="button"
                            aria-label=${`bring down stack ${stack.name}`}
                            disabled=${!knows(exec, "stack.down")}
                            title=${knows(exec, "stack.down") ? `bring down ${up(stack)} of ${stack.containers.length}` : whyNot(exec, "stack.down")}
                            onClick=${async () => {
                                const result = await run("stack.down", stack.name, {});
                                if (result.ok && onDone) onDone();
                            }}
                        >${Icon.stop()}</button>
                    `}
                    </div>
                </div>
                <div class="rows">
                    ${stack.shown.map((c) => html`
                        <button class="crow" type="button" key=${c.id} onClick=${() => setDetails(c)}>
                            <span class="dot ${containerState(c)}" title=${WORD[containerState(c)]}></span>
                            <span class="cname">${c.name}</span>
                            <span class="cmeta">${meta(c)}</span>
                        </button>
                    `)}
                </div>
            </section>
        `)}

        <${Sheet} open=${Boolean(details)} onClose=${() => setDetails(null)} label="container details" side>
            ${details && html`
                <${Details}
                    container=${details}
                    exec=${exec}
                    onCopy=${() => copyName(details.name, toast)}
                    onLogs=${() => { onLogs(details); setDetails(null); }}
                    onRun=${async (kind) => {
                        const result = await run(kind, details.name, {});
                        if (result.ok) {
                            setDetails(null);
                            if (onDone) onDone();
                        }
                    }}
                />
            `}
        <//>
    `;
}

function usage(stack) {
    if (stack.hasStats) return `${(stack.cpu || 0).toFixed(1)}% cpu · ${bytes(stack.mem || 0)}`;
    return up(stack) > 0 ? "load not collected yet" : "";
}

function meta(c) {
    if (c.state !== "running") return c.status || "stopped";

    const parts = [`${c.cpu.toFixed(1)}% cpu`, bytes(c.mem)];
    if (c.health) parts.push(c.health);
    return parts.join(" · ");
}

function hit(stack, c, needle) {
    if (!needle) return true;
    return stack.name.toLowerCase().includes(needle) || c.name.toLowerCase().includes(needle);
}

async function copyName(name, toast) {
    try {
        await navigator.clipboard.writeText(name);
        toast("Name copied", name);
    } catch (err) {
        toast("Could not copy", "the clipboard is unavailable", true);
    }
}

function Details({ container, exec, onCopy, onLogs, onRun }) {
    const kind = containerState(container);
    const running = container.state === "running";
    const can = (kind) => knows(exec, kind);
    const ready = can("container.start") || can("container.stop") || can("container.restart");
    const why = whyNot(exec, running ? "container.stop" : "container.start");
    return html`
        <div class="shead">
            <span class="dot ${kind}"></span>
            <div>
                <div class="stitle">${container.name}</div>
                <div class="ssub">${WORD[kind]}</div>
            </div>
        </div>

        <div class="kv"><span class="k">image</span><span class="v">${container.image}</span></div>
        <div class="kv"><span class="k">service</span><span class="v">${container.service || "—"}</span></div>
        <div class="kv"><span class="k">state</span><span class="v">${container.status}</span></div>
        ${container.health && html`<div class="kv"><span class="k">health</span><span class="v">${container.health}</span></div>`}
        ${container.state === "running" && html`
            <div class="kv"><span class="k">load</span><span class="v">${container.cpu.toFixed(1)}% · ${bytes(container.mem)}</span></div>
        `}
        ${container.ports.length > 0 && html`
            <div class="kv"><span class="k">ports</span><span class="v">${container.ports.join(", ")}</span></div>
        `}

        <div class="btnrow">
            ${running
                ? html`
                    <button class="btn danger" type="button" disabled=${!can("container.restart")}
                        title=${whyNot(exec, "container.restart")}
                        onClick=${() => onRun("container.restart")}>Restart</button>
                    <button class="btn danger" type="button" disabled=${!can("container.stop")}
                        title=${whyNot(exec, "container.stop")}
                        onClick=${() => onRun("container.stop")}>Stop</button>
                `
                : html`
                    <button class="btn primary" type="button" disabled=${!can("container.start")}
                        title=${whyNot(exec, "container.start")}
                        onClick=${() => onRun("container.start")}>Start</button>
                `}
        </div>
        ${!ready && html`<p class="hint warn">${why}</p>`}

        <button class="item" type="button" onClick=${onLogs}>Logs</button>
        <button class="item" type="button" onClick=${onCopy}>Copy the name</button>

        <${Metric}
            title="load"
            subject=${`container:${container.name}`}
            metric="cpu"
            hint="cpu, %"
        />
    `;
}
