import { useState } from "preact/hooks";
import { html } from "../html.js";
import { useAction } from "../actions/gate.js";
import { knows, whyNot } from "../exec.js";

export function Programs({ programs, exec, onDone }) {
    const run = useAction();
    const [busy, setBusy] = useState("");
    const update = async (kind) => {
        setBusy(kind);
        try {
            const result = await run(kind, "host");
            if (result.ok) onDone?.();
        } finally {
            setBusy("");
        }
    };
    return html`
        <section class="card programs" aria-label="Agent versions">
            <h3>Agents</h3>
            <div class="program-row">
                <div><strong>Codex</strong><span class="program-version">${programs?.codex?.version || "unknown"}</span>
                    <p class="hint">Updated by the panel · every six hours</p></div>
                <button class="btn" type="button" disabled=${!!busy || !knows(exec, "codex.update")}
                    title=${whyNot(exec, "codex.update")} onClick=${() => update("codex.update")}>
                    ${busy === "codex.update" ? "Updating…" : "Update now"}
                </button>
            </div>
            ${programs?.codex?.verdict && html`
                <details class="program-verdict"><summary>Codex ${programs.codex.checkedVersion || ""} · ${programs.codex.verdict}</summary>
                    <pre>${programs.codex.detail}</pre></details>
            `}
            ${programs?.codex?.updateError && html`<p class="hint">${programs.codex.updateError}</p>`}
            ${Object.entries(programs?.codex?.restartPending || {}).map(([contour, reason]) => html`
                <p class="hint" key=${contour}>${contour}: ${reason}</p>
            `)}
            <div class="program-row">
                <div><strong>Claude</strong><span class="program-version">${programs?.claude?.version || "unknown"}</span>
                    <p class="hint">Updates itself</p></div>
                <button class="btn" type="button" disabled=${!!busy || !knows(exec, "claude.update")}
                    title=${whyNot(exec, "claude.update")} onClick=${() => update("claude.update")}>
                    ${busy === "claude.update" ? "Updating…" : "Update now"}
                </button>
            </div>
            ${programs?.claude?.verdict && html`
                <details class="program-verdict"><summary>Claude ${programs.claude.checkedVersion || ""} · ${programs.claude.verdict}</summary>
                    <pre>${programs.claude.detail}</pre></details>
            `}
            ${programs?.claude?.updateError && html`<p class="hint">${programs.claude.updateError}</p>`}
        </section>
    `;
}
