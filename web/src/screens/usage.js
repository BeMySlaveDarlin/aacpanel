// Token usage on the phone: a layer page with a summary, one chart and two lists.
import { useCallback, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { area, Chart } from "../chart.js";
import { ago, bytes, plural, share, tokens } from "../format.js";
import { BackHead } from "../ui/back.js";
import { Chips } from "../ui/chips.js";
import { Chat } from "./chat.js";
import { agoText, CODEX_REACHED, CodexWeek, Limit, staleLimits } from "./sessions/limits.js";
import {
    BY_PROJECT,
    BY_SESSION,
    PERIODS,
    inboundOf,
    scanProgress,
    stepFor,
    useUsageBreakdown,
    useUsageContours,
    useUsageScan,
    useUsageSeries,
    useUsageSummary,
} from "../data/usage.js";

const SPANS = PERIODS.filter((p) => p.id !== "14d");

const ANY = "";

const OUTSIDE_LABEL = "outside the map";

const OUTSIDE = "@outside";

const SERIES = [{}, area("--accent", "#63a8ff")];

const TOP_SESSIONS = 20;

// Usage renders the token usage page.
export function Usage({ snapshot, exec, onBack }) {
    const [period, setPeriod] = useState("7d");
    const [contour, setContour] = useState(ANY);
    const [project, setProject] = useState("");
    const [chat, setChat] = useState(null);

    const scan = useUsageScan();
    const fresh = scan.rounds;

    const filter = useMemo(() => ({
        period,
        contours: contour === ANY ? [] : [contour],
        project: project === OUTSIDE ? "" : project,
        outside: project === OUTSIDE,
    }), [period, contour, project]);

    const whole = useMemo(() => ({
        period: "all",
        contours: contour === ANY ? [] : [contour],
    }), [contour]);

    const names = useUsageContours(fresh);
    const options = useUsageBreakdown(whole, BY_PROJECT, 0, fresh);
    const totals = useUsageSummary(filter, fresh);
    const step = stepFor(period);
    const points = useUsageSeries(filter, step, fresh);
    const projects = useUsageBreakdown(filter, BY_PROJECT, 0, fresh);
    const sessions = useUsageBreakdown(filter, BY_SESSION, TOP_SESSIONS, fresh);

    const open = useCallback((row) => {
        const live = ((snapshot && snapshot.sessions) || [])
            .find((s) => s.sessionId === row.key) || null;
        setChat({ id: row.key, name: live ? live.session : row.label, live });
    }, [snapshot]);

    if (chat) {
        return html`<${Chat}
            name=${chat.name}
            id=${chat.id}
            live=${chat.live}
            archive=${null}
            snapshot=${snapshot}
            exec=${exec}
            onBack=${() => setChat(null)}
        />`;
    }

    const chips = [{ id: ANY, label: "all" }].concat(
        ((names.kind === "ready" && names.data.contours) || []).map((c) => ({ id: c, label: c })),
    );

    return html`
        <${BackHead} onBack=${onBack} label="back">
            <h2>Usage</h2>
            <span class="where">where the tokens went</span>
        <//>

        <section class="card ufilters">
            <${Chips} items=${chips} current=${contour} onSelect=${setContour} />
            <${Chips} items=${SPANS} current=${period} onSelect=${setPeriod} />
            <${Projects} state=${options} current=${project} onPick=${setProject} />
        </section>

        <${Limits} limits=${snapshot && snapshot.limits} />

        <${Totals} state=${totals} />

        <section class="card">
            <${Graph} state=${points} step=${step} />
        </section>

        <${Slices}
            title="Projects"
            state=${projects}
            contour=${contour}
            empty="Nothing has been spent in this period."
        />

        <${Slices}
            title="Sessions"
            state=${sessions}
            contour=${contour}
            empty="There are no conversations in this period."
            onOpen=${open}
        />

        <${Rescan} state=${scan} />
    `;
}

// Limits are what each contour may still spend: claude's five hours and
// seven days, and beside them codex's week where its account told one. A
// contour codex alone has spent in has a line of codex's alone.
function Limits({ limits }) {
    const known = !limits ? []
        : limits.contours && limits.contours.length ? limits.contours
        : limits.fiveHour || limits.sevenDay || limits.codex ? [limits] : [];
    if (!known.length) return null;
    return html`
        <section class="card ulimits">
            <h2>Limits</h2>
            ${known.map((c, i) => html`
                <div class="ulimit" key=${c.profile || c.configDir || i}>
                    ${c.profile && html`<div class="ulimitname">${c.profile}</div>`}
                    <div class="ulimitrows">
                        ${(c.fiveHour || c.sevenDay) && html`
                            <span class="agentword" data-agent="claude">Claude</span>
                            <div class=${`limits${staleLimits(c) ? " ulold" : ""}`}
                                 title=${staleLimits(c) ? `the numbers are from ${agoText(c.ageSec)}` : undefined}>
                                <${Limit} name="5 hours" data=${c.fiveHour} />
                                <${Limit} name="7 days" data=${c.sevenDay} />
                            </div>
                        `}
                        ${c.codex && html`
                            <span class="agentword" data-agent="codex">Codex</span>
                            <${CodexLine} codex=${c.codex} />
                        `}
                    </div>
                </div>
            `)}
            ${known.some((c) => c.codex) && html`
                <p class="hint ulimitnote">codex shows its week; the share is renewed while the panel holds a thread of the contour</p>
            `}
        </section>
    `;
}

// CodexLine is codex's line of a contour: its week, as wide as one of
// claude's windows and dimmed while its numbers are old, and the word that
// the account ran into its limit.
function CodexLine({ codex }) {
    const old = staleLimits(codex);
    return html`
        <div class=${`limits ulhalf${old ? " ulold" : ""}`} title=${old ? `the numbers are from ${agoText(codex.ageSec)}` : undefined}>
            <${CodexWeek} codex=${codex} name="7 days" />
        </div>
        ${codex.reached && html`<span></span><p class="lsub crit ulreached">${CODEX_REACHED}</p>`}
    `;
}

function Note({ state }) {
    if (state.kind === "loading") return html`<p class="hint">Loading…</p>`;
    if (state.kind === "unavailable") return html`<p class="hint warn">${state.error}</p>`;
    if (state.kind === "failed") return html`<p class="hint crit">${state.error}</p>`;
    return null;
}

function Projects({ state, current, onPick }) {
    const rows = state.kind === "ready" ? state.data.rows : [];
    const known = rows.some((r) => valueOf(r) === current);
    return html`
        <select class="search uproject" value=${current} onChange=${(e) => onPick(e.target.value)}>
            <option value="">All projects</option>
            ${!known && current !== "" && html`<option value=${current}>${current}</option>`}
            ${rows.map((row) => html`
                <option key=${valueOf(row)} value=${valueOf(row)}>${row.label}</option>
            `)}
        </select>
    `;
}

function valueOf(row) {
    return row.outside ? OUTSIDE : row.key;
}

function Totals({ state }) {
    if (state.kind !== "ready") return html`<section class="card"><${Note} state=${state} /></section>`;

    const { summary: sum, inbound, subShareIn, subShareOut } = state.data;
    return html`
        <section class="card uthree">
            <div class="utotal">
                <div class="tname">In</div>
                <div class="tval">${tokens(inbound)}</div>
            </div>
            <div class="utotal">
                <div class="tname">Out</div>
                <div class="tval">${tokens(sum.output)}</div>
            </div>
            <div class="utotal">
                <div class="tname">Answers</div>
                <div class="tval">${tokens(sum.answers)}</div>
            </div>
            <p class="sub usub">
                subagents ${share(subShareIn * 100)} in · ${share(subShareOut * 100)} out
                · ${sum.sessions} ${plural(sum.sessions, "conversation", "conversations")}
            </p>
        </section>
    `;
}

function Graph({ state, step }) {
    if (state.kind !== "ready") return html`<${Note} state=${state} />`;

    const grain = state.data.step || step;
    const data = plot(state.data.points, grain);
    if (!data) return html`<p class="hint">There is nothing to draw for this period.</p>`;
    return html`
        <${Chart} data=${data} series=${SERIES} height=${116} yFormat=${tokens} />
        <p class="hint">input tokens, ${grain === "hour" ? "by the hour" : "by the day"}</p>
    `;
}

// plot returns the series points in uPlot format.
export function plot(points, step) {
    if (!points || points.length === 0) return null;

    const stepSec = step === "day" ? 86400 : 3600;
    const t = [];
    const v = [];
    for (const p of points) {
        const at = Math.round(new Date(p.at).getTime() / 1000);
        for (let prev = t.length > 0 ? t[t.length - 1] + stepSec : at; at - prev >= stepSec / 2; prev += stepSec) {
            t.push(prev);
            v.push(0);
        }
        t.push(at);
        v.push(inboundOf(p));
    }
    return [t, v];
}

function Slices({ title, state, empty, contour, onOpen }) {
    const rows = state.kind === "ready" ? state.data.rows : [];
    const total = rows.reduce((sum, row) => sum + inboundOf(row), 0);

    return html`
        <section class="card uslices">
            <h2>${title}</h2>
            <${Note} state=${state} />
            ${state.kind === "ready" && rows.length === 0 && html`<p class="hint">${empty}</p>`}

            ${rows.map((row) => {
                const value = inboundOf(row);
                const part = total > 0 ? (value / total) * 100 : 0;
                const where = row.outside || contour ? "" : row.contour;
                const body = html`
                    <div class="uline">
                        <span class="nm">
                            ${row.outside ? OUTSIDE_LABEL : row.label}
                            ${where && html`<span class="uwhere">${where}</span>`}
                        </span>
                        <span class="topval">${tokens(value)}</span>
                    </div>
                    <div class=${`topbar${row.outside ? " outside" : ""}`}>
                        <i class="mx" style=${`width:${Math.max(0, Math.min(100, part))}%`}></i>
                    </div>
                `;
                const key = valueOf(row);
                return onOpen
                    ? html`<button class="urow" type="button" key=${key} onClick=${() => onOpen(row)}>${body}</button>`
                    : html`<div class="urow" key=${key}>${body}</div>`;
            })}
        </section>
    `;
}

function Rescan({ state }) {
    const [error, setError] = useState("");

    const go = async () => {
        setError("");
        try {
            await state.start();
        } catch (err) {
            setError(err.message);
        }
    };

    if (state.kind !== "ready") return html`<section class="card"><${Note} state=${state} /></section>`;
    if (!state.scan.available) {
        return html`<section class="card"><p class="hint warn">${state.scan.reason}</p></section>`;
    }

    const { estimate, scannedAt, files, agentError } = state.scan;
    const price = estimate
        ? `${estimate.pendingFiles} ${plural(estimate.pendingFiles, "file", "files")} · ${bytes(estimate.pendingBytes)}`
        : "";
    const round = scanProgress(state.scan.state);

    return html`
        <section class="card uscan">
            <p class="sub">
                ${scannedAt
                    ? `collected ${ago(new Date(scannedAt * 1000).toISOString())}`
                    : "never collected"}
                ${files ? ` · ${files} ${plural(files, "file", "files")} on record` : ""}
            </p>
            ${agentError && html`<p class="hint warn">${agentError}</p>`}
            ${error && html`<p class="hint crit">${error}</p>`}
            <button class="btn primary uscango" type="button"
                    disabled=${state.running || state.starting} onClick=${go}>
                ${state.running
                    ? `Collecting… ${round.filesDone} of ${round.files}`
                    : `Rescan${price ? ` · ${price}` : ""}`}
            </button>
        </section>
    `;
}
