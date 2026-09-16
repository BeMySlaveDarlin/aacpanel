// A published page, shown from the panel's own copy.
//
// The link on a card opens the account the page was published into, and the
// person reading the panel is signed into one account at a time: a page a
// contour published is a page they cannot open. The copy has no account at
// all, so it opens for whoever can see the panel.

import { html } from "../html.js";
import { pageURL } from "../data/artifacts.js";

// How large a copy is before the screen says so. Under this a page loads while
// the reader is still looking at the title.
const HEAVY = 400 * 1024;

function weigh(bytes) {
    if (!bytes) return "";
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function ArtifactPage({ card }) {
    if (!card || !card.id) {
        return html`
            <p class="hint">There is no copy of this page: it was published before the panel kept them,
            or the copy has been swept.</p>
        `;
    }

    return html`
        <div class="artpage">
            <div class="artsub">
                ${card.file}
                ${card.versions > 1 && html` · ${card.versions} versions`}
                ${card.bytes > 0 && html` · ${weigh(card.bytes)}`}
            </div>

            ${(card.bytes || 0) > HEAVY && html`
                <p class="hint">A page this size takes a moment to draw on a phone.</p>
            `}

            <!-- The page is somebody else's code. The frame is sandboxed here and
                 the answer carries the same sandbox in a header, so the rule holds
                 even if the address is opened on its own. Scripts run; the cookie
                 of the panel is not theirs to read. -->
            <iframe
                class="artframe"
                title=${card.title || card.file}
                src=${pageURL(card.id)}
                sandbox="allow-scripts allow-popups allow-forms"
                referrerpolicy="no-referrer"
            ></iframe>

            ${card.url && html`
                <p class="artout">
                    <a href=${card.url} target="_blank" rel="noopener noreferrer">Open where it was published</a>
                    <span class="artoutwhy">that address asks for the account it went out under</span>
                </p>
            `}
        </div>
    `;
}
