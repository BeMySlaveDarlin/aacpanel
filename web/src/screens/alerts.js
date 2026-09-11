// The alerts screen: what broke, what degraded, and how to turn notifications on.
import { html } from "../html.js";
import { BackHead } from "../ui/back.js";
import { ago, bytes, pct } from "../format.js";
import { Icon } from "../ui/icons.js";
import { Trouble } from "../ui/trouble.js";
import { SEVERITY } from "../alerts.js";
import { BASIS, METRIC, sinceText, subjectText, useDegradations } from "../degradations.js";
import { usePush } from "../push.js";
import { useAction } from "../actions/gate.js";
import { actionName } from "../actions/registry.js";

export function Alerts({ alerts, onAction, onBack }) {
    return html`
        <${BackHead} onBack=${onBack} label="to the settings">
            <h2>Alerts</h2>
            <span class="where">what broke and what of it reaches the phone</span>
        <//>

        <${Notifications} />
        <${Engine} state=${alerts} onAction=${onAction} />
        <${Degradations} />
    `;
}

function Engine({ state, onAction }) {
    const run = useAction();

    return html`
        <div class="grouphead">alerts</div>

        ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
        ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
        ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
        ${state.kind === "ready" && state.alerts.length === 0 && html`
            <p class="empty">There are no open alerts.</p>
        `}

        ${[...(state.alerts || []), ...(state.older || [])].map((alert) => html`
            <section
                class="alert ${SEVERITY[alert.severity] || ""} ${alert.closedAt ? "closed" : ""} ${!alert.closedAt && alert.ackedAt ? "acked" : ""}"
                key=${alert.id}
            >
                <div>
                    <div class="t">${alert.rule}</div>
                    <div class="d">
                        ${subjectText(alert.subject)}
                        ${alert.value != null && html` · now ${alert.value.toFixed(1)}`}
                        ${alert.worst != null && html` · peak ${alert.worst.toFixed(1)}`}
                    </div>
                    <div class="when">
                        ${alert.closedAt
                            ? `closed ${ago(new Date(alert.closedAt * 1000).toISOString())}`
                            : `opened ${ago(new Date(alert.openedAt * 1000).toISOString())}`}
                        ${rule(alert.payload) && ` · ${rule(alert.payload)}`}
                    </div>

                    ${!alert.closedAt && html`
                        <div class="act">
                            ${alert.suggested && html`
                                <button
                                    class="btn primary"
                                    type="button"
                                    onClick=${async () => {
                                        const result = await run(alert.suggested.kind, alert.suggested.target, {});
                                        if (result.ok && onAction) onAction();
                                    }}
                                >${actionLabel(alert.suggested)}</button>
                            `}
                            ${!alert.ackedAt && html`
                                <button
                                    class="btn"
                                    type="button"
                                    onClick=${async () => {
                                        const result = await run("alert.ack", alert.id, {});
                                        if (result.ok && onAction) onAction();
                                    }}
                                >Got it</button>
                            `}
                            ${alert.ackedAt && html`
                                <span class="acked-mark">seen ${ago(new Date(alert.ackedAt * 1000).toISOString())}</span>
                            `}
                        </div>
                    `}
                </div>
            </section>
        `)}

        ${state.kind === "ready" && state.alerts.length > 0 && html`
            <button class="ghost wide" type="button" onClick=${state.more}>show more</button>
        `}
    `;
}

function rule(payload) {
    if (!payload || payload.threshold === undefined) return "";
    const sign = { ">": "above", ">=": "above", "<": "below", "<=": "below", "=": "equals" }[payload.op] || payload.op;
    const unitLabel = { "%": "%", bytes: " bytes", "байты": " bytes", flag: "", "флаг": "" }[payload.unit];
    const unit = unitLabel === undefined ? payload.unit || "" : unitLabel;
    const forSec = payload.forSec ? ` longer than ${payload.forSec >= 60 ? `${Math.round(payload.forSec / 60)} min` : `${payload.forSec} s`}` : "";
    return `${sign} ${payload.threshold}${unit}${forSec}`;
}

function actionLabel(suggested) {
    return `${actionName(suggested.kind)} ${suggested.target}`;
}

function Degradations() {
    const state = useDegradations();

    return html`
        <div class="grouphead">degradations</div>

        ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
        ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
        ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
        ${state.kind === "ready" && state.items.length === 0 && html`
            <p class="empty">Nothing stands out from the usual.</p>
        `}

        ${state.kind === "ready" && state.items.map((item) => html`
            <section class="alert warn" key=${`${item.subject}:${item.metric}`}>
                <div>
                    <div class="t">
                        ${subjectText(item.subject)} — ${METRIC[item.metric] || item.metric}
                        is ${item.ratio.toFixed(1)} times more than usual
                    </div>
                    <div class="d">
                        now ${value(item.metric, item.current)}, usually ${value(item.metric, item.norm)}
                        · lasting ${sinceText(item.since)}
                    </div>
                    <div class="when">${BASIS[item.basis] || item.basis} · ${item.samples} samples</div>
                </div>
            </section>
        `)}
    `;
}

function value(metric, n) {
    if (metric === "mem") return bytes(n);
    if (metric === "load") return n.toFixed(2);
    return pct(n);
}

function Notifications() {
    const push = usePush();
    const run = useAction();

    const disable = async () => {
        const result = await run("push.disable", "this device", {});
        if (result.ok) await push.forget();
    };

    if (push.state === "unsupported") {
        return html`
            <div class="grouphead">notifications</div>
            <p class="hint">The browser cannot do push — the panel has to be watched by hand.</p>
        `;
    }

    return html`
        <div class="grouphead">notifications</div>
        <section class="card">
            ${push.state === "denied"
                ? html`
                    <p class="numbers"><span class="dot crit"></span> blocked</p>
                    <p class="hint">
                        The permission was denied in the browser. It can only be given back in the site settings —
                        the app cannot ask a second time.
                    </p>
                `
                : push.state === "on"
                    ? html`
                        <p class="numbers"><span class="dot ok"></span> on for this device</p>
                        <p class="hint">
                            They arrive even when the app is closed. There is nothing to tune: once about the
                            reason and once about it being over — there will be no reminders.
                        </p>
                        <div class="btnrow">
                            <button class="btn" type="button" disabled=${push.busy} onClick=${disable}>Turn off</button>
                            <button class="btn primary" type="button" disabled=${push.busy}
                                    onClick=${() => run("push.test", "subscribed devices", {})}>
                                Test
                            </button>
                        </div>
                    `
                    : html`
                        <p class="numbers"><span class="dot off"></span> off</p>
                        <p class="hint">
                            The app will send a notification when something breaks: a container went down,
                            the subscription limit is running out. Without them you only find out by opening the panel.
                        </p>
                        <div class="btnrow">
                            <button class="btn primary" type="button" disabled=${push.busy} onClick=${push.enable}>
                                ${push.busy ? "…" : "Turn notifications on"}
                            </button>
                        </div>
                    `}

            ${push.error && html`<p class="hint crit">${push.error}</p>`}
        </section>
    `;
}
