// Shared pieces of the usage screens: the widget shell, share bars and rows.
import { html } from "../../html.js";

export const SEG_COLORS = ["--ctx-2", "--ctx-1", "--ctx-3", "--ctx-4"];
export const SEG_REST = "--ink-faint";

// Widget is the shell of a home tile: a header, a body and a click that opens it.
export function Widget({ span, title, note, actions, onOpen, children }) {
    const open = onOpen
        ? {
            onClick: onOpen,
            onKeyDown: (e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onOpen();
                }
            },
            role: "button",
            tabIndex: 0,
        }
        : {};
    return html`
        <section class=${`dkwidget ${span} uwg${onOpen ? " open" : ""}`} ...${open}>
            <header class="uwghead">
                <span class="uwgtitle">${title}</span>
                ${note && html`<span class="uwgnote">${note}</span>`}
                ${actions && html`<span class="uwgacts" onClick=${stop}>${actions}</span>`}
            </header>
            <div class="uwgbody">${children}</div>
        </section>
    `;
}

// stop keeps a click inside a control from opening the breakdown.
export function stop(e) {
    e.stopPropagation();
}

// Blank states why a widget is empty.
export function Blank({ kind, children }) {
    return html`<p class=${`uwgblank ${kind || ""}`}>${children}</p>`;
}

// troubleOf returns why a tile is empty when it is not about the data itself.
export function troubleOf(state) {
    if (state.kind === "loading") return html`<${Blank}>Loading…<//>`;
    if (state.kind === "unavailable") return html`<${Blank} kind="warn">${state.error}<//>`;
    if (state.kind === "failed") return html`<${Blank} kind="crit">${state.error}<//>`;
    return null;
}

// ShareBar renders a segmented share bar.
export function ShareBar({ parts, total }) {
    const whole = total || parts.reduce((sum, p) => sum + (p.value || 0), 0);
    if (whole <= 0) return html`<div class="usharebar empty"></div>`;

    let at = 0;
    const stops = parts.map((p) => {
        const from = (at / whole) * 100;
        at += p.value || 0;
        const to = (at / whole) * 100;
        return `var(${p.color}) ${from.toFixed(2)}% ${to.toFixed(2)}%`;
    });
    return html`<div class="usharebar" style=${`background:linear-gradient(90deg, ${stops.join(",")})`}></div>`;
}

// Legend renders the captions of a share bar.
export function Legend({ parts }) {
    return html`
        <ul class="uleg">
            ${parts.map((p) => html`
                <li key=${p.key}>
                    <i style=${`background:var(${p.color})`}></i>
                    <span class="ulegname">${p.label}</span>
                    <span class="ulegval">${p.note}</span>
                </li>
            `)}
        </ul>
    `;
}

// Row renders a list row with a right-aligned value and a share bar under it.
export function Row({ name, sub, value, share, tone, onOpen, title }) {
    const body = html`
        <span class="usrline">
            <span class="usrname" title=${title || name}>${name}</span>
            ${sub && html`<span class="usrsub">${sub}</span>`}
            <span class="usrval">${value}</span>
        </span>
        ${share != null && html`
            <span class="usrbar"><i class=${tone || ""} style=${`width:${Math.max(0, Math.min(100, share))}%`}></i></span>
        `}
    `;
    if (!onOpen) return html`<div class="usrow">${body}</div>`;
    const click = (e) => {
        e.stopPropagation();
        onOpen();
    };
    return html`<button class="usrow open" type="button" onClick=${click}>${body}</button>`;
}

// pctText renders a share as a percentage string.
export function pctText(part, whole) {
    if (!whole) return "0%";
    const value = (part / whole) * 100;
    if (value > 0 && value < 0.1) return "<0.1%";
    return `${value >= 10 ? Math.round(value) : Math.round(value * 10) / 10}%`;
}
