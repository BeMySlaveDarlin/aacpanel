// The devices screen: what has access to the panel, and how to take it away.
import { useCallback, useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead } from "../ui/back.js";
import { ago } from "../format.js";
import { useAction } from "../actions/gate.js";
import { useToast } from "../ui/toasts.js";
import { Icon } from "../ui/icons.js";

export function Devices({ onBack }) {
    const [devices, setDevices] = useState(null);
    const [revoked, setRevoked] = useState([]);
    const [current, setCurrent] = useState(null);
    const [error, setError] = useState("");
    const [code, setCode] = useState(null);
    const [renaming, setRenaming] = useState(null);
    const [draft, setDraft] = useState("");

    const run = useAction();
    const toast = useToast();

    const load = useCallback(async () => {
        try {
            const response = await fetch("/api/devices", { credentials: "same-origin" });
            if (!response.ok) throw new Error(await message(response));
            const body = await response.json();
            setDevices(body.devices || []);
            setRevoked(body.revoked || []);
            setCurrent(body.current);
            setError("");
        } catch (err) {
            setError(err.message);
        }
    }, []);

    useEffect(() => {
        load();
    }, [load]);

    const revoke = async (device) => {
        const own = device.id === current;
        const result = await run(own ? "device.revokeSelf" : "device.revoke", device.name, { id: device.id });
        if (!result.ok) return;
        if (own) location.href = "/login?reason=revoked";
        else load();
    };

    const issue = async () => {
        const result = await run("enroll.issue", "new device", {});
        if (result.ok && result.data) setCode(result.data);
    };

    const rename = async (device, name) => {
        setRenaming(null);
        if (!name || name === device.name) return;
        const result = await run("device.rename", name, { id: device.id });
        if (result.ok) load();
    };

    return html`
        <${BackHead} onBack=${onBack} label="to the settings">
            <h2>Devices</h2>
        <//>

        ${error && html`<p class="empty">${error}</p>`}
        ${devices === null && !error && html`<p class="empty">Loading…</p>`}
        ${devices && devices.length === 0 && html`<p class="empty">No devices have been added.</p>`}

        ${(devices || []).map((device) => html`
            <section class="card device" key=${device.id}>
                ${renaming === device.id
                    ? html`
                        <div class="rename">
                            <input
                                class="search"
                                autofocus
                                value=${draft}
                                onInput=${(e) => setDraft(e.target.value)}
                                onKeyDown=${(e) => {
                                    if (e.key === "Enter") rename(device, draft.trim());
                                    if (e.key === "Escape") setRenaming(null);
                                }}
                            />
                            <button class="btn" type="button" onClick=${() => setRenaming(null)}>Cancel</button>
                            <button class="btn primary" type="button" onClick=${() => rename(device, draft.trim())}>Save</button>
                        </div>
                    `
                    : html`
                        <button class="device-name" type="button" onClick=${() => { setDraft(device.name); setRenaming(device.id); }}>
                            ${device.name}
                            ${device.id === current && html`<span class="tag">this device</span>`}
                            <small>rename</small>
                        </button>
                    `}

                <div class="kv"><span class="k">added</span><span class="v">${ago(device.created_at)}</span></div>
                <div class="kv">
                    <span class="k">last sign-in</span>
                    <span class="v">${device.last_seen ? ago(device.last_seen) : "never"}</span>
                </div>

                <button class="btn danger" type="button" onClick=${() => revoke(device)}>Revoke access</button>
            </section>
        `)}

        ${revoked.length > 0 && html`
            <div class="projhead"><h2>Revoked</h2></div>
            ${revoked.map((device) => html`
                <section class="card device gone" key=${device.id}>
                    <div class="device-name plain">${device.name}</div>
                    <div class="kv"><span class="k">added</span><span class="v">${ago(device.created_at)}</span></div>
                    <div class="kv">
                        <span class="k">last sign-in</span>
                        <span class="v">${device.last_seen ? ago(device.last_seen) : "never"}</span>
                    </div>
                    <div class="kv">
                        <span class="k">access closed</span>
                        <span class="v">${ago(device.revoked_at)}</span>
                    </div>
                </section>
            `)}
        `}

        <section class="card">
            <h2>New device</h2>
            <p class="hint">The code lasts five minutes and burns out after the first registration.</p>
            ${code
                ? html`
                    <p class="enroll-code">${code.code}</p>
                    <p class="hint">enter it on the new device before ${when(code.expires_at)}</p>
                `
                : html`<button class="btn primary" type="button" onClick=${issue}>Issue a code</button>`}
        </section>
    `;
}

async function message(response) {
    const text = (await response.text()).trim();
    try {
        const parsed = JSON.parse(text);
        if (parsed && parsed.error) return parsed.error;
    } catch (err) {
    }
    if (response.status === 401) return "you need to sign in again";
    if (response.status === 503) return "the device database is unavailable";
    return text || `the server answered ${response.status}`;
}

function when(iso) {
    if (!iso) return "—";
    return new Date(iso).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}
