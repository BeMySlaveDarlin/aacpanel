import { html } from "../html.js";

// Both shells show the leaf the listener serves, including one kept in memory
// while a broken renewal is being repaired on disk.
export function Certificate({ certificate }) {
    if (!certificate) return null;
    const days = Math.max(0, Math.ceil(certificate.daysLeft));
    const expired = certificate.daysLeft <= 0;
    const kind = expired ? "crit" : certificate.daysLeft <= 14 ? "warn" : "";
    return html`
        <section class="card lancert">
            <div class="cardhead"><span class="tname">LAN certificate</span></div>
            <p>${certificate.name}</p>
            <p class=${kind}>${expired ? "Expired" : `${days} days left`} · ${new Date(certificate.expiresAt * 1000).toLocaleDateString()}</p>
            ${certificate.daysLeft <= 14 && html`<p class="hint">Renewal needs attention.</p>`}
        </section>
    `;
}
