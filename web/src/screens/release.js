import { html } from "../html.js";

// Both machine pages say which release the panel was built from and, once the
// service has seen a newer one on GitHub, that it is out, with its page and the
// command that takes this install there. A build that names no release says so
// and is told of none.
export function PanelRelease({ panel }) {
    if (!panel) return null;
    const newer = panel.newer;
    return html`
        <section class="card panelrel" aria-label="Panel version">
            <h3>Panel</h3>
            <div class="panelrow">
                <div><strong>aacpanel</strong><span class="panelversion">${panel.version || "version unknown"}</span>
                    <p class="hint">${releaseHint(panel)}</p></div>
            </div>
            ${newer && html`
                <p class="panelnew">
                    <a href=${newer.page} target="_blank" rel="noopener noreferrer">${newer.tag} is out</a>
                    ${" — "}<code>${newer.update}</code>
                </p>
            `}
        </section>
    `;
}

function releaseHint(panel) {
    if (!panel.version) return "Built without a release: told of none";
    if (!panel.check) return "Not asking GitHub for newer releases: AACP_UPDATE_CHECK=0";
    return "Updated by ./install.sh update in its clone";
}
