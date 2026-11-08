// The bottom menu: the main navigation of the phone layout.
import { html } from "../html.js";
import { Icon } from "./icons.js";

export const TABS = [
    { id: "sessions", label: "Sessions", icon: Icon.sessions },
    { id: "terminals", label: "Terminals", icon: Icon.prompt },
    { id: "host", label: "Host", icon: Icon.cpu },
];

// Sessions and terminals stand left of the home button; the host and the
// settings menu stand right of it.
const LEFT = ["sessions", "terminals"];
const RIGHT = ["host"];

// Nav draws the menu. A listener without the terminal has no terminals item:
// a button there would open a screen with nothing behind it.
export function Nav({ current, onSelect, onMenu, menu = false, flag = false, home, terminals = true }) {
    const items = TABS.filter((section) => terminals || section.id !== "terminals");
    const button = (section) => html`
        <button
            key=${section.id}
            type="button"
            aria-current=${current === section.id ? "true" : "false"}
            onClick=${() => onSelect(section.id)}
        >
            ${section.icon()}
            <span>${section.label}</span>
        </button>
    `;

    const side = (ids) => ids.map((id) => items.find((section) => section.id === id)).filter(Boolean).map(button);

    return html`
        <nav class="tabs">
            ${side(LEFT)}
            <div class="homeslot">${home}</div>
            ${side(RIGHT)}
            <button type="button" aria-label=${flag ? "Settings, the app can be installed" : "Settings"}
                aria-haspopup="dialog" aria-expanded=${menu ? "true" : "false"}
                data-flag=${flag ? "install" : undefined} onClick=${onMenu}>
                ${Icon.settings()}
                <span>Settings</span>
            </button>
        </nav>
    `;
}
