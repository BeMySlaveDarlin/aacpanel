import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { guide, saveGuide } from "../../data/briefs.js";

// This text belongs to the panel's database, unlike the host configuration
// listed below it. Each call for brief rules reads the saved text afresh.
export function BriefGuide() {
    const [saved, setSaved] = useState(null);
    const [text, setText] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [retry, setRetry] = useState(0);
    const [done, setDone] = useState(false);

    useEffect(() => {
        let alive = true;
        setError("");
        guide().then((body) => {
            if (!alive) return;
            setSaved(body.text);
            setText(body.text);
        }).catch((err) => alive && setError(String(err.message || err)));
        return () => { alive = false; };
    }, [retry]);

    const save = async (event) => {
        event.preventDefault();
        setBusy(true);
        setError("");
        setDone(false);
        try {
            const body = await saveGuide(text);
            setSaved(body.text);
            setDone(true);
        } catch (err) {
            setError(String(err.message || err));
        } finally {
            setBusy(false);
        }
    };

    return html`
        <div class="grouphead">Briefs</div>
        <form class="card setrow setguide" onSubmit=${save}>
            <div class="setline">
                <label class="setkey" for="brief-guide">Guide for briefs</label>
                <span class="setcost now">applies to the next request</span>
            </div>
            <p class="hint" id="brief-guide-help">
                Your rules and examples follow the panel's writing rules for every session.
                Save an empty field to use only the panel's rules.
            </p>
            ${saved === null && !error && html`<p class="hint">Loading…</p>`}
            ${saved !== null && html`
                <textarea id="brief-guide" class="setguidefield" rows="9" value=${text}
                    aria-describedby="brief-guide-help" disabled=${busy}
                    onInput=${(event) => { setText(event.currentTarget.value); setDone(false); }} />
                <div class="btnrow">
                    <button class="btn primary" type="submit" disabled=${busy || text === saved}>
                        ${busy ? "Saving…" : "Save guide"}
                    </button>
                    <span class="hint" role="status">${done ? "Saved" : text !== saved ? "Not saved" : ""}</span>
                </div>
            `}
            ${error && html`<p class="hint warn" role="alert">${error}</p>`}
            ${error && saved === null && html`<button class="btn" type="button" onClick=${() => setRetry(retry + 1)}>Try again</button>`}
        </form>
    `;
}
