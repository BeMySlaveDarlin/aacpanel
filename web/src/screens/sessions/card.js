// The session card: live and archived in one shell.

import { html } from "../../html.js";
import { ago, plural, since } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { ContextBar } from "../../ui/bar.js";
import { waitText } from "../../ui/waits.js";
import { PERSONAL } from "../../contour.js";

// PastRow renders the card of a session from the archive.
export function PastRow({ row, action, dim = false, onOpen }) {
    const lines = html`
        <div class="mline">
            ${row.tokensMax > 0 && html`<span>up to ${Math.round(row.tokensMax / 1000)}k</span>`}
            ${row.model && html`<span>${shortModel(row.model)}</span>`}
            ${row.home && html`<span>in the home directory</span>`}
        </div>
        <div class="mline">
            ${row.messages > 0
                ? html`<span>${row.messages} ${plural(row.messages, "message", "messages")}</span>`
                : html`<span class="warn">not a word said</span>`}
            ${row.startedAt && row.lastAt && html`<span>ran ${spanText(stamp(row.startedAt), stamp(row.lastAt))}</span>`}
        </div>
        <div class="mline">
            <span>last time ${when(stamp(row.lastAt))}</span>
            ${row.nameGuessed && html`<span class="dim">name from the directory</span>`}
            ${row.stale && html`<span class="warn">context from before the compaction</span>`}
        </div>
    `;
    return html`
        <${SessionCard}
            name=${row.name}
            group=${row.project && row.project.group}
            pct=${row.pctMax}
            pctNote="peak"
            peak
            dim=${dim}
            lines=${lines}
            action=${action}
            onOpen=${onOpen ? () => onOpen(row.name, row.sessionId) : null}
        />
    `;
}

// stamp returns a transcript timestamp in seconds of the epoch.
export function stamp(iso) {
    if (!iso) return 0;
    const ms = Date.parse(iso);
    return Number.isFinite(ms) ? Math.round(ms / 1000) : 0;
}

function shortModel(model) {
    return model.replace(/^claude-/, "");
}

function spanText(from, to) {
    const sec = Math.max(0, (to || 0) - (from || 0));
    if (sec < 3600) return `${Math.max(1, Math.round(sec / 60))} min`;
    if (sec < 86400) return `${Math.round(sec / 3600)} h`;
    const days = Math.floor(sec / 86400);
    const hours = Math.round((sec % 86400) / 3600);
    return hours > 0 ? `${days} d ${hours} h` : `${days} d`;
}

// when renders a timestamp from the history.
export function when(sec) {
    if (!sec) return "—";
    return new Date(sec * 1000).toLocaleString("ru-RU", {
        day: "numeric", month: "short", hour: "2-digit", minute: "2-digit",
    });
}

function modelName(id) {
    if (!id) return "";
    const parts = id.replace(/^claude-/, "").split("-").filter((p) => !/^\d{8}$/.test(p));
    const [name, ...version] = parts;
    return version.length ? `${name} ${version.join(".")}` : name;
}

function Waiting({ task, what }) {
    const sec = Math.max(0, Math.round((Date.now() - task.since) / 1000));
    return html`
        <div class="busy" role="status">
            <span class="spin"></span>
            <span>${what} · ${sec} s</span>
        </div>
    `;
}

// Ghost renders the place of a console being raised in the list of live ones.
export function Ghost({ task }) {
    return html`
        <section class="srow ghost">
            <div class="sbody">
                <div class="r1">
                    <span class="nm">${task.target}</span>
                    <span class="pct blank">starting</span>
                </div>
                <div class="meta"><div class="mline"><span>the console is starting on the host</span></div></div>
            </div>
            <div class="rowacts"></div>
            <${Waiting} task=${task} what="starting" />
        </section>
    `;
}

function SessionCard({ name, group, contour, state, live, pct, pctNote, peak, blank, dim, lines, action, waiting, onOpen }) {
    const body = html`
        <div class="sbody">
            <div class="r1">
                ${state && html`<span class=${`sdot ${state}`}></span>`}
                <span class="nm">
                    ${group && html`<span class="nmgroup">${group} · </span>`}${name}
                </span>
                ${contour && html`<span class="cmark">${contour}</span>`}
                ${blank
                    ? html`<span class="pct blank">${blank}</span>`
                    : html`<span class="pct">
                        ${pct.toFixed(1)}%${pctNote && html`<span class="pctnote">${pctNote}</span>`}
                    </span>`}
            </div>
            <div class="meta">${lines}</div>
        </div>
    `;

    return html`
        <section class="srow ${dim ? "past" : ""} ${live ? "live" : ""} ${action ? "acts" : ""}">
            ${!blank && html`<${ContextBar} pct=${pct} peak=${peak} edge />`}
            ${onOpen
                ? html`<button class="sopen" type="button" onClick=${onOpen}
                    aria-label=${`open conversation ${name}`}>${body}</button>`
                : body}
            <div class="rowacts">${action}</div>
            ${waiting}
        </section>
    `;
}

// workText returns what the session has in progress, as short lines.
export function workText(work) {
    if (!work) return [];
    const out = [];
    if (work.tasks > 0) {
        out.push({
            kind: "tasks",
            text: `${work.tasks} background ${plural(work.tasks, "command", "commands")}`,
        });
    }
    if (work.agents > 0) {
        out.push({ kind: "agents", text: `${work.agents} ${plural(work.agents, "subagent", "subagents")}` });
    }
    return out;
}

export function LiveRow({ session, where, notes, exec, wait, onOpen }) {
    const run = useAction();
    const closing = wait ? wait.of("close", session.session) : null;
    const work = workText(session.work);
    const dot = session.ask || session.waitingFor ? "waiting" : session.status || "";
    const contour = session.profile || (where && where.profile) || "";

    const empty = Boolean(session.noRequests);
    const ready = knows(exec, "session.close");
    const why = whyNot(exec, "session.close");
    const place = session.home
        ? html`<span class="place">the main session of the host</span>`
        : where === null && html`<span class="warn">outside the profile map</span>`;

    const lines = html`
        <div class="mline">
            ${empty
                ? html`<span class="tok">no requests yet</span>`
                : html`<span class="tok">${Math.round(session.tokens / 1000)}k / ${Math.round(session.limit / 1000)}k</span>`}
            ${session.model && html`<span class="model">${modelName(session.model)}${session.effort ? ` · ${session.effort}` : ""}</span>`}
            ${!empty && !session.limitKnown && html`<span class="warn">limit unknown</span>`}
        </div>
        <div class="mline">
            <span class="msgs">${session.messages} ${plural(session.messages, "message", "messages")}</span>
            ${session.compacts > 0 && html`<span class="compacts">compactions: ${session.compacts}</span>`}
            <span class="lived">alive for ${since(session.startedAt)}</span>
        </div>
        <div class="mline">
            ${!empty && html`<span class="reqago">request ${ago(session.lastRequestAt)}</span>`}
            ${place}
            ${session.stale && html`<span class="warn">the data is stale</span>`}
        </div>
        ${(session.waitingFor || session.status === "waiting") && html`
            <div class="mline"><span class="waits">${waitText(session.waitingFor)}</span></div>
        `}
        ${session.ask && html`
            <div class="mline">
                <span class="waits">asking${session.ask.header ? `: ${session.ask.header}` : ""}</span>
                ${session.ask.count > 1 && html`<span>${session.ask.count} ${plural(session.ask.count, "question", "questions")}</span>`}
            </div>
        `}
        ${((session.status === "busy" && !session.waitingFor) || work.length > 0) && html`
            <div class="mline">
                ${session.status === "busy" && !session.waitingFor
                    && html`<span class="working">handling the request</span>`}
                ${work.map((item) => html`<span key=${item.kind} class=${`w${item.kind}`}>${item.text}</span>`)}
            </div>
        `}
        ${(notes || []).map((note) => html`
            <div class="mline" key=${note}><span class="warn">${note}</span></div>
        `)}
    `;

    const action = !session.home && html`
        <button
            class="iconbtn danger"
            type="button"
            aria-label=${`close session ${session.session}`}
            disabled=${!ready}
            title=${ready ? "close the session" : why}
            onClick=${async () => {
                await run("session.close", session.session, {});
            }}
        >${Icon.close()}</button>
    `;

    return html`
        <${SessionCard}
            name=${session.session}
            group=${where && where.group}
            contour=${contour === PERSONAL ? "" : contour}
            state=${dot}
            live
            pct=${session.pct}
            blank=${empty ? "context empty" : null}
            lines=${lines}
            action=${action}
            onOpen=${onOpen && (() => onOpen(session.session, session.sessionId))}
            waiting=${closing && html`<${Waiting} task=${closing} what="closing" />`}
        />
    `;
}
