import { useCallback, useEffect, useState } from "preact/hooks";
import { html } from "../html.js";
import { useAction } from "../actions/gate.js";
import { BrowserViewer } from "./browser/viewer.js";
import { knows } from "../exec.js";

// Browser keeps profile lifecycle actions behind the shared confirmation gate.
export function Browser({ exec, wide = false, onLayer }) {
    const run = useAction();
    const [status, setStatus] = useState(null);
    const [error, setError] = useState("");
    const [name, setName] = useState("");
    const [open, setOpen] = useState(null);
    const [busy, setBusy] = useState(false);
    const reload = useCallback(async () => {
        try {
            const r = await fetch("/api/browser");
            if (!r.ok) throw new Error("The browser profiles are unavailable");
            const body = await r.json();
            setStatus(body);
            setError(body.enabled ? "" : body.reason);
        } catch (e) {
            setError(e.message);
        }
    }, []);
    useEffect(() => { reload(); }, [reload]);
    useEffect(() => {
        if (onLayer) onLayer(Boolean(open));
        return () => { if (onLayer) onLayer(false); };
    }, [Boolean(open), onLayer]);
    const act = async (kind, profile) => {
        if (busy || !exec.browser || !knows(exec, kind)) return;
        setBusy(true);
        try {
            const result = await run(kind, profile.id || profile.name, { name: profile.name });
            if (result.ok) {
                if (kind === "browser.open") setOpen(profile);
                if (kind === "browser.create") setName("");
                await reload();
            }
        } finally {
            setBusy(false);
        }
    };
    if (open) return html`<${BrowserViewer} profile=${open} limits=${status.limits} wide=${wide}
        onBack=${() => { setOpen(null); reload(); }} />`;
    return html`<section class=${`browser-profiles${wide ? " browser-wide" : ""}`}>
        <header><h2>Browser</h2><p>Your browser on this host. Sign-ins stay with each profile.</p></header>
        ${error && html`<p role="alert">${error}</p>`}
        ${status && status.enabled && html`
            <form class="browser-create" onSubmit=${(e) => { e.preventDefault(); act("browser.create", { name: name.trim() }); }}>
                <input aria-label="New browser profile" placeholder="Profile name" maxLength="40" value=${name}
                    onInput=${(e) => setName(e.currentTarget.value)} />
                <button type="submit" disabled=${busy || !name.trim()}>Create</button>
            </form>
            <div class="browser-cards">${status.profiles.map((p) => html`<article key=${p.id}>
                <div><h3>${p.name}</h3><span>${p.open ? "Running" : "Closed"}</span></div>
                <button disabled=${busy} onClick=${() => act("browser.open", p)}>Open</button>
                ${p.open && html`<button disabled=${busy} onClick=${() => act("browser.close", p)}>Close</button>`}
                <button class="browser-delete" disabled=${busy} onClick=${() => act("browser.delete", p)}>Delete</button>
            </article>`)}</div>
            ${!status.profiles.length && html`<p class="browser-empty">Create a profile to open a page and sign in.</p>`}
            <p class="browser-note">Public websites only. Uploads and downloads are unavailable. Two profiles can run at once.</p>
        `}
    </section>`;
}
