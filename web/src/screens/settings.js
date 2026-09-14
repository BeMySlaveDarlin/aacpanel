// The panel settings screen: what it is running with and what changing that costs.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Icon } from "../ui/icons.js";
import { Trouble } from "../ui/trouble.js";
import { useToast } from "../ui/toasts.js";
import { useWide } from "../ui/wide.js";
import { askMicrophone, dictation, setDictation, speech } from "./chat/dictate.js";
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
        <${Device} />

        <div class="grouphead">The host</div>
        <p class="hint sethead">
            These only show. They are changed on the host — every line says what its change costs.
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

// Device holds what belongs to this browser rather than to the host. The
// microphone is the device's, so a phone that dictates and a desk that does not
// is an ordinary arrangement, and the switch is kept here and nowhere else.
function Device() {
    const toast = useToast();
    const heard = Boolean(speech());
    const [on, setOn] = useState(() => dictation());
    const [asking, setAsking] = useState(false);

    const flip = async () => {
        if (on) {
            setOn(setDictation(false));
            return;
        }
        setAsking(true);
        const got = await askMicrophone();
        setAsking(false);
        if (!got.ok) {
            toast("Dictation stays off", got.why, true);
            return;
        }
        setOn(setDictation(true));
    };

    return html`
        <div class="grouphead">This device</div>
        <p class="hint sethead">Kept in this browser. Nothing here reaches the host or the other devices.</p>

        <section class="card setrow">
            <div class="setline">
                <span class="setkey">dictation</span>
                <span class=${`setcost ${on ? "now" : "locked"}`}>${on ? "on" : "off"}</span>
            </div>
            <p class="setval">Hold the send button in a conversation and talk; let go and the words land in the field, to read over before they go.</p>
            <p class="hint warn">
                The recognition is the browser's own: it sends the recording to Google and needs the
                network. Nothing else in the panel leaves the machine.
            </p>
            ${heard
                ? html`
                    <button class="btn" type="button" disabled=${asking} onClick=${flip}>
                        ${asking ? "asking for the microphone…" : on ? "Turn dictation off" : "Turn dictation on"}
                    </button>
                `
                : html`<p class="hint">This browser has no speech recognition, so there is nothing to turn on.</p>`}
        </section>
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
