// No conversation picked.

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";

export function ChatEmpty() {
    return html`
        <div class="chatnone">
            <span class="chatnoneicon">${Icon.sessions()}</span>
            <p class="chatnonesay">No session picked</p>
            <p class="chatnonesub">The conversation opens here</p>
        </div>
    `;
}
