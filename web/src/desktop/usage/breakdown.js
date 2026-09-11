// Detailed usage breakdowns, opened by clicking a widget.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { pct, tokens } from "../../format.js";
import * as usage from "../../data/usage.js";
import { Layer, Trail } from "./layer.js";
import { Blank, Legend, Row, SEG_COLORS, SEG_REST, ShareBar, pctText, troubleOf } from "./parts.js";
import { shortModel } from "./tops.js";

const LIMIT = 200;

const TOOL_ROWS = 25;

const LEVEL_NAMES = {
    [usage.BY_CONTOUR]: "contours",
    [usage.BY_GROUP]: "groups",
    [usage.BY_PROJECT]: "projects",
    [usage.BY_SESSION]: "sessions",
    [usage.BY_CWD]: "directories",
};

// UsageLayer is the common entry: the view decides what to show.
export function UsageLayer({ view, onClose }) {
    const [period, setPeriod] = useState(view.period || "30d");
    const chips = view.pinned ? html`<span class="upinned">${view.pinned}</span>` : html`
        <span class="uchips">
            ${usage.PERIODS.map((p) => html`
                <button
                    key=${p.id}
                    class=${`uchip${p.id === period ? " on" : ""}`}
                    type="button"
                    onClick=${() => setPeriod(p.id)}
                >${p.label}</button>
            `)}
        </span>
    `;

    if (view.kind === "tools") return html`<${ToolsLayer} period=${period} chips=${chips} onClose=${onClose} />`;
    if (view.kind === "models") return html`<${ModelsLayer} period=${period} chips=${chips} onClose=${onClose} />`;
    return html`<${BreakdownLayer} view=${view} period=${period} chips=${chips} onClose=${onClose} />`;
}

function BreakdownLayer({ view, period, chips, onClose }) {
    const [trail, setTrail] = useState(view.trail);
    const step = trail[trail.length - 1];
    const filter = { ...step.filter, period };
    const state = usage.useUsageBreakdown(filter, step.by);

    const rows = ((state.kind === "ready" && state.data.rows) || []).slice();
    if (step.sort === "hit") rows.sort((a, b) => usage.hitOf(a) - usage.hitOf(b));
    const whole = rows.reduce((sum, r) => sum + usage.inboundOf(r), 0);

    const dive = (row) => {
        const next = nextStep(step, row);
        if (next) setTrail(trail.concat([next]));
    };

    return html`
        <${Layer}
            title="Usage breakdown"
            note=${`by ${LEVEL_NAMES[step.by] || step.by}`}
            trail=${html`<${Trail} steps=${trail} onPick=${(i) => setTrail(trail.slice(0, i + 1))} />`}
            actions=${chips}
            onClose=${onClose}
        >
            ${troubleOf(state) || (rows.length === 0
                ? html`<${Blank}>${`No ${LEVEL_NAMES[step.by] || "rows"} in this period.`}<//>`
                : html`
                        <table class="ustable">
                            <thead>
                                <tr>
                                    <th>${LEVEL_NAMES[step.by] || step.by}</th>
                                    ${step.by !== usage.BY_SESSION && html`<th class="num">sessions</th>`}
                                    <th class="num">inbound</th>
                                    <th class="num">output</th>
                                    <th class="num">answers</th>
                                    <th class="num">cache</th>
                                    <th class="share">share</th>
                                </tr>
                            </thead>
                            <tbody>
                                ${rows.map((row) => {
                                    const inbound = usage.inboundOf(row);
                                    const hit = usage.hitOf(row) * 100;
                                    const down = nextStep(step, row);
                                    return html`
                                        <tr
                                            key=${row.key || row.label}
                                            class=${`${row.outside ? "outside" : ""}${down ? " open" : ""}`}
                                            onClick=${down ? () => dive(row) : null}
                                        >
                                            <td>
                                                <span class="usrname" title=${row.label}>${nameOf(row, step.by)}</span>
                                                ${row.contour && html`<span class="usrsub">${row.contour}</span>`}
                                                ${step.by === usage.BY_SESSION && html`<span class="usrsub">${String(row.key).slice(0, 8)}</span>`}
                                            </td>
                                            ${step.by !== usage.BY_SESSION && html`<td class="num">${row.sessions}</td>`}
                                            <td class="num">${tokens(inbound)}</td>
                                            <td class="num">${tokens(row.output)}</td>
                                            <td class="num">${row.answers}</td>
                                            <td class="num">${inbound > 0 ? pct(hit) : "—"}</td>
                                            <td class="share">
                                                <span class="usrbar">
                                                    <i
                                                        class=${row.outside ? "warn" : ""}
                                                        style=${`width:${whole ? Math.min(100, (inbound / whole) * 100) : 0}%`}
                                                    ></i>
                                                </span>
                                                <span class="usrpct">${pctText(inbound, whole)}</span>
                                            </td>
                                        </tr>
                                    `;
                                })}
                            </tbody>
                        </table>
                        <p class="hint">
                            ${step.sort === "hit"
                                ? "Worst cache first among the heaviest rows of the period."
                                : "Rows are ordered by inbound tokens: fresh input plus cache read and written."}
                            ${rows.length >= LIMIT && html`<span> The server returns ${LIMIT} rows at most, so the tail is not shown.</span>`}
                            ${(step.by === usage.BY_PROJECT || step.by === usage.BY_GROUP) && html`
                                <span> "outside the map" stands last and stands always, even at zero:
                                the sum over projects has to match the sum over the contour.</span>
                            `}
                        </p>
                `)}
        <//>
    `;
}

// nextStep returns the level a click on a row leads to.
export function nextStep(step, row) {
    const filter = { ...step.filter };
    switch (step.by) {
        case usage.BY_CONTOUR:
            return { by: usage.BY_GROUP, label: row.label, filter: { ...filter, contours: [row.key] } };
        case usage.BY_GROUP:
            if (row.outside) return { by: usage.BY_CWD, label: "outside the map", filter: { ...filter, outside: true } };
            return { by: usage.BY_PROJECT, label: row.label, filter: { ...filter, group: row.key } };
        case usage.BY_PROJECT:
            if (row.outside) return { by: usage.BY_CWD, label: "outside the map", filter: { ...filter, outside: true } };
            return { by: usage.BY_SESSION, label: row.label, filter: { ...filter, project: row.key } };
        default:
            return null;
    }
}

function nameOf(row, by) {
    if (row.outside && by !== usage.BY_CWD) return "outside the map";
    return row.label || row.key || "—";
}

function ToolsLayer({ period, chips, onClose }) {
    const state = usage.useUsageTools({ period }, TOOL_ROWS);
    const data = state.kind === "ready" ? state.data : null;
    const top = (data && data.top) || [];
    const whole = data ? data.calls || 0 : 0;
    const overall = whole > 0 ? (data.errors || 0) / whole : 0;

    return html`
        <${Layer}
            title="Tools"
            note=${data ? `${data.calls} calls · ${pctText(data.errors || 0, whole)} errors overall` : ""}
            actions=${chips}
            onClose=${onClose}
        >
            ${troubleOf(state) || (top.length === 0
                ? html`<${Blank}>No tool calls in this period.<//>`
                : html`
                        <div class="uslist wide">
                            ${top.map((row) => {
                                const rate = row.calls > 0 ? row.errors / row.calls : 0;
                                const bad = row.errors > 0 && rate > overall;
                                return html`
                                    <${Row}
                                        key=${row.tool}
                                        name=${row.tool}
                                        sub=${row.errors > 0
                                            ? html`<span class=${bad ? "crit" : ""}>${row.errors} ${row.errors === 1 ? "error" : "errors"} · ${pctText(row.errors, row.calls)}</span>`
                                            : null}
                                        value=${`${row.calls} calls`}
                                    />
                                `;
                            })}
                            ${data.restNames > 0 && html`
                                <${Row}
                                    name=${`${data.restNames} more ${data.restNames === 1 ? "name" : "names"}`}
                                    value=${`${data.restCalls} calls`}
                                    tone="dim"
                                />
                            `}
                        </div>
                        <p class="hint">
                            Errors are painted only where their share is above the overall one: otherwise the
                            screen fills with colour and "bad" stops catching the eye. The tail stays one line
                            on purpose — MCP handles called once a month should not push Bash down.
                        </p>
                `)}
        <//>
    `;
}

function ModelsLayer({ period, chips, onClose }) {
    const models = usage.useUsageModels({ period });
    const summary = usage.useUsageSummary({ period });
    const rows = (models.kind === "ready" && models.data.models) || [];
    const whole = rows.reduce((sum, r) => sum + usage.inboundOf(r), 0);
    const sum = summary.kind === "ready" ? summary.data : null;
    const sub = sum ? sum.subShareIn || 0 : 0;
    const subParts = [
        { key: "main", color: SEG_COLORS[0], value: 1 - sub, label: "main sessions", note: pctText(1 - sub, 1) },
        { key: "sub", color: SEG_COLORS[3], value: sub, label: "subagents", note: pctText(sub, 1) },
    ];

    return html`
        <${Layer} title="Models and subagents" actions=${chips} onClose=${onClose}>
            ${troubleOf(models) || troubleOf(summary) || (rows.length === 0
                ? html`<${Blank}>No usage in this period.<//>`
                : html`
                        <div class="useg wide">
                            <${ShareBar} parts=${subParts} total=${1} />
                            <${Legend} parts=${subParts} />
                            ${sum && html`
                                <p class="hint">
                                    ${`Subagents took ${pctText(sub, 1)} of the inbound and `
                                        + `${pctText(sum.subShareOut || 0, 1)} of the output; `
                                        + `${sum.summary.agents} of them ran, of ${sum.summary.kinds} kinds.`}
                                </p>
                                <p class="hint">
                                    The share is counted against the full inbound — fresh input plus cache read
                                    and written. Against the input column alone the same number reads 95%: with
                                    the cache working, that column is what did not fit into it, not what was spent.
                                </p>
                            `}
                        </div>
                        <table class="ustable">
                            <thead>
                                <tr>
                                    <th>model</th>
                                    <th class="num">inbound</th>
                                    <th class="num">output</th>
                                    <th class="num">answers</th>
                                    <th class="num">cache</th>
                                    <th class="share">share</th>
                                </tr>
                            </thead>
                            <tbody>
                                ${rows.map((row, i) => {
                                    const inbound = usage.inboundOf(row);
                                    return html`
                                        <tr key=${row.model}>
                                            <td>
                                                <span class="useglead" style=${`background:var(${i < SEG_COLORS.length ? SEG_COLORS[i] : SEG_REST})`}></span>
                                                <span class="usrname" title=${row.model}>${shortModel(row.model)}</span>
                                            </td>
                                            <td class="num">${tokens(inbound)}</td>
                                            <td class="num">${tokens(row.output)}</td>
                                            <td class="num">${row.answers}</td>
                                            <td class="num">${inbound > 0 ? pct(usage.hitOf(row) * 100) : "—"}</td>
                                            <td class="share">
                                                <span class="usrbar"><i style=${`width:${whole ? Math.min(100, (inbound / whole) * 100) : 0}%`}></i></span>
                                                <span class="usrpct">${pctText(inbound, whole)}</span>
                                            </td>
                                        </tr>
                                    `;
                                })}
                            </tbody>
                        </table>
                `)}
        <//>
    `;
}
