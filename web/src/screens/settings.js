// The panel settings screen: what it is running with and what changing that costs.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Icon } from "../ui/icons.js";
import { Trouble } from "../ui/trouble.js";
import { useWide } from "../ui/wide.js";
import { byGroup, clash, costOf, SOURCE, valueText, valueTone } from "./settings/model.js";

const SETTINGS_URL = "/api/settings";

function useSettings() {
    const [state, setState] = useState({ kind: "loading" });

    useEffect(() => {
        let alive = true;
        fetch(SETTINGS_URL, { credentials: "same-origin" })
            .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
            .then((body) => alive && setState({ kind: "ready", items: body.settings || [] }))
            .catch((err) => alive && setState({ kind: "failed", error: String(err.message || err) }));
        return () => {
            alive = false;
        };
    }, []);

    return state;
}

export function Settings({ onClose }) {
    const wide = useWide();

    useBackClose(wide, onClose);

    useEffect(() => {
        if (!wide) return undefined;
        const onKey = (event) => {
            if (event.key === "Escape") onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [wide, onClose]);

    const state = useSettings();

    if (wide) {
        return html`
            <div class="dkscrim" onClick=${onClose}>
                <div
                    class="dkmodal dkmodalwide"
                    role="dialog"
                    aria-modal="true"
                    aria-label="settings"
                    onClick=${(event) => event.stopPropagation()}
                >
                    <div class="dkmodalhead">
                        <span class="dkmodaltitle">Settings</span>
                        <span class="dkmodalsub">what this panel is running with</span>
                        <button class="dkclose" type="button" aria-label="close" onClick=${onClose}>
                            ${Icon.close()}
                        </button>
                    </div>
                    <div class="dkmodalbody"><${List} state=${state} /></div>
                </div>
            </div>
        `;
    }

    return html`
        <${BackHead} onBack=${onClose} label="back">
            <h2>Settings</h2>
        <//>
        <${List} state=${state} />
    `;
}

function List({ state }) {
    return html`
        <p class="hint sethead">
            This page only shows. Values are changed on the host — every line says what its change costs.
        </p>

        ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}

        ${state.kind === "failed" && html`
            <${Trouble} what="settings" error=${state.error}
                hint="the panel itself keeps running: this list is only a view of what it was started with." />
        `}

        ${state.kind === "ready" && state.items.length === 0 && html`
            <p class="empty">No settings are known.</p>
        `}

        ${state.kind === "ready" && byGroup(state.items).map(({ group, items }) => html`
            <div class="grouphead" key=${group.title}>${group.title}</div>
            <p class="hint">${group.hint}</p>
            ${items.map((item) => html`<${Row} key=${item.key} item=${item} />`)}
        `)}
    `;
}

function Row({ item }) {
    const cost = costOf(item.cost);
    const file = clash(item);

    return html`
        <section class="card setrow">
            <div class="setline">
                <span class="setkey">${item.key}</span>
                <span class=${`setcost ${cost.tone}`}>${cost.text}</span>
            </div>

            <div class=${`setval ${valueTone(item)}`}>${valueText(item)}</div>

            <div class="setmeta">
                <span>${SOURCE[item.source] || item.source}</span>
                ${item.note && html`<span class="setnote">${item.note}</span>`}
            </div>

            ${file && html`
                <div class="setclash">
                    <div class="setclashrow">
                        <span class="setclashk">the machine description says</span>
                        <span class="setclashv">${file}</span>
                    </div>
                    <p class="hint">
                        The environment was read when the unit started and wins over the file.
                        This value applies only after the restart named above.
                    </p>
                </div>
            `}
        </section>
    `;
}
