// The action journal panel of the right column.
import { html } from "../../html.js";
import { actionName } from "../../actions/registry.js";
import { ago, useJSON } from "./util.js";

const RESULT = {
    ok: { word: "done", cls: "ok" },
    failed: { word: "did not work", cls: "crit" },
    timeout: { word: "no answer — it may still have gone through", cls: "warn" },
    rejected: { word: "rejected", cls: "crit" },
    "": { word: "in flight", cls: "" },
};

function took(ms) {
    if (!ms || ms <= 0) return "";
    return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${ms} ms`;
}

export function Journal() {
    const { data, error } = useJSON("/api/actions?limit=60");
    const rows = (data && data.actions) || [];
    if (error) return html`<p class="dkempty">the journal is unavailable: ${error}</p>`;
    return html`
        <div class="dkscroll">
            ${rows.map((a) => {
                const result = RESULT[a.result || ""] || { word: a.result, cls: "" };
                return html`
                    <div class=${`dkentry${a.result && a.result !== "ok" ? " bad" : ""}`} key=${a.id}>
                        <div class="dkentrytop">
                            <span class=${`dkdot dk${a.result === "ok" ? "ok" : a.result ? "crit" : "busy"}`}></span>
                            <span class="dkname">${actionName(a.kind)}</span>
                            <span class="dkentrytarget">${a.target || ""}</span>
                            <span class="dkwhen">${ago(a.ts)}</span>
                        </div>
                        ${a.detail && html`<div class="dkentrydetail">${a.detail}</div>`}
                        ${a.error && html`<div class="dkentrydetail dkfail">${a.error}</div>`}
                        <div class="dkentrymeta">
                            <span class=${result.cls}>${result.word}</span>
                            ${a.device_name && html`<span>${a.device_name}</span>`}
                            ${took(a.duration_ms) && html`<span>${took(a.duration_ms)}</span>`}
                        </div>
                    </div>
                `;
            })}
            ${rows.length === 0 && html`<p class="dkempty">there are no records</p>`}
        </div>
    `;
}
