// Home-screen breakdowns: projects, models and subagents, tools.
import { html } from "../../html.js";
import { tokens } from "../../format.js";
import * as usage from "../../data/usage.js";
import { Blank, Legend, Row, SEG_COLORS, SEG_REST, ShareBar, Widget, pctText, troubleOf } from "./parts.js";

const PERIOD = "30d";

const PROJECT_ROWS = 6;

// Projects renders the top projects plus the mandatory "outside the map" row.
export function Projects({ rounds, scan, onOpen }) {
    const state = usage.useUsageBreakdown({ period: PERIOD }, usage.BY_PROJECT, 0, rounds);
    const rows = (state.kind === "ready" && state.data.rows) || [];
    const outside = rows.find((r) => r.outside) || null;
    const named = rows.filter((r) => !r.outside);
    const top = named.slice(0, PROJECT_ROWS);
    const rest = named.slice(PROJECT_ROWS);
    const restInbound = rest.reduce((sum, r) => sum + usage.inboundOf(r), 0);
    const whole = named.reduce((sum, r) => sum + usage.inboundOf(r), 0) + usage.inboundOf(outside);

    return html`
        <${Widget}
            span="w4 h2"
            title="Top projects"
            note="30 days"
            onOpen=${scan.collected ? () => onOpen(null) : null}
        >
            ${!scan.collected
                ? html`<${Blank}>Nothing collected yet.<//>`
                : troubleOf(state) || (rows.length === 0
                    ? html`<${Blank}>No usage in this period.<//>`
                    : html`
                            <div class="uslist">
                                ${top.map((row) => html`
                                    <${Row}
                                        key=${row.key || row.label}
                                        name=${row.label}
                                        sub=${row.contour}
                                        value=${tokens(usage.inboundOf(row))}
                                        share=${whole ? (usage.inboundOf(row) / whole) * 100 : 0}
                                        onOpen=${() => onOpen(row)}
                                    />
                                `)}
                                ${rest.length > 0 && html`
                                    <${Row}
                                        name=${`${rest.length} more ${rest.length === 1 ? "project" : "projects"}`}
                                        value=${tokens(restInbound)}
                                        share=${whole ? (restInbound / whole) * 100 : 0}
                                        tone="dim"
                                        onOpen=${() => onOpen(null)}
                                    />
                                `}
                                ${outside && html`
                                    <${Row}
                                        name="outside the map"
                                        sub=${`${pctText(usage.inboundOf(outside), whole)} of the period`}
                                        value=${tokens(usage.inboundOf(outside))}
                                        share=${whole ? (usage.inboundOf(outside) / whole) * 100 : 0}
                                        tone="warn"
                                        onOpen=${() => onOpen(outside)}
                                    />
                                `}
                            </div>
                    `)}
        <//>
    `;
}

const MODEL_SEGS = 4;

// Models renders the model shares and the main-session / subagent split.
export function Models({ rounds, scan, onOpen }) {
    const models = usage.useUsageModels({ period: PERIOD }, rounds);
    const summary = usage.useUsageSummary({ period: PERIOD }, rounds);

    const rows = (models.kind === "ready" && models.data.models) || [];
    const whole = rows.reduce((sum, r) => sum + usage.inboundOf(r), 0);
    const head = rows.slice(0, MODEL_SEGS);
    const tail = rows.slice(MODEL_SEGS);
    const tailInbound = tail.reduce((sum, r) => sum + usage.inboundOf(r), 0);

    const parts = head.map((row, i) => ({
        key: row.model,
        color: SEG_COLORS[i % SEG_COLORS.length],
        value: usage.inboundOf(row),
        label: shortModel(row.model),
        note: pctText(usage.inboundOf(row), whole),
    }));
    if (tail.length > 0) {
        parts.push({
            key: "rest",
            color: SEG_REST,
            value: tailInbound,
            label: `${tail.length} more`,
            note: pctText(tailInbound, whole),
        });
    }

    const sum = summary.kind === "ready" ? summary.data : null;
    const sub = sum ? sum.subShareIn || 0 : 0;
    const subParts = [
        { key: "main", color: SEG_COLORS[0], value: 1 - sub, label: "main sessions", note: pctText(1 - sub, 1) },
        { key: "sub", color: SEG_COLORS[3], value: sub, label: "subagents", note: pctText(sub, 1) },
    ];
    const agents = sum ? sum.summary.agents : 0;

    return html`
        <${Widget}
            span="w6 h1"
            title="Models and subagents"
            note="30 days · share of inbound"
            onOpen=${scan.collected ? onOpen : null}
        >
            ${!scan.collected
                ? html`<${Blank}>Nothing collected yet.<//>`
                : troubleOf(models) || troubleOf(summary) || (rows.length === 0
                    ? html`<${Blank}>No usage in this period.<//>`
                    : html`
                            <div class="useg">
                                <${ShareBar} parts=${parts} total=${whole} />
                                <${Legend} parts=${parts} />
                            </div>
                            <div class="useg">
                                <${ShareBar} parts=${subParts} total=${1} />
                                <${Legend} parts=${subParts} />
                                <p class="uwgsub">
                                    ${`${agents} ${agents === 1 ? "subagent" : "subagents"} ran · `}
                                    ${`${pctText(sum.subShareOut || 0, 1)} of the output`}
                                </p>
                            </div>
                    `)}
        <//>
    `;
}

const TOOL_ROWS = 5;

// Tools renders the top tools by call count.
export function Tools({ rounds, scan, onOpen }) {
    const state = usage.useUsageTools({ period: PERIOD }, TOOL_ROWS, rounds);
    const data = state.kind === "ready" ? state.data : null;
    const top = (data && data.top) || [];
    const whole = data ? data.calls || 0 : 0;
    const overall = whole > 0 ? (data.errors || 0) / whole : 0;

    return html`
        <${Widget}
            span="w6 h1"
            title="Tools"
            note=${scan.collected && data ? `30 days · ${pctText(data.errors || 0, whole)} errors overall` : "30 days"}
            onOpen=${scan.collected ? onOpen : null}
        >
            ${!scan.collected
                ? html`<${Blank}>Nothing collected yet.<//>`
                : troubleOf(state) || (top.length === 0
                    ? html`<${Blank}>No tool calls in this period.<//>`
                    : html`
                            <div class="uslist">
                                ${top.map((row) => html`
                                    <${ToolRow} key=${row.tool} row=${row} overall=${overall} />
                                `)}
                                ${data.restNames > 0 && html`
                                    <${Row}
                                        name=${`${data.restNames} more ${data.restNames === 1 ? "name" : "names"}`}
                                        value=${`${data.restCalls} calls`}
                                        tone="dim"
                                        onOpen=${onOpen}
                                    />
                                `}
                            </div>
                    `)}
        <//>
    `;
}

// ToolRow renders one tool row, painting the error share only when above average.
export function ToolRow({ row, overall }) {
    const rate = row.calls > 0 ? row.errors / row.calls : 0;
    const bad = row.errors > 0 && rate > overall;
    return html`
        <${Row}
            name=${row.tool}
            sub=${row.errors > 0
                ? html`<span class=${bad ? "crit" : ""}>${pctText(row.errors, row.calls)} errors</span>`
                : null}
            value=${`${row.calls} calls`}
        />
    `;
}

// shortModel returns the model name without the common prefix.
export function shortModel(name) {
    return String(name || "").replace(/^claude-/, "") || "unknown";
}
