// The bottom menu: the main navigation of the phone layout.
import { html } from "../html.js";
import { Icon } from "./icons.js";

export const TABS = [
    { id: "sessions", label: "Sessions", icon: Icon.sessions },
    { id: "terminals", label: "Terminals", icon: Icon.prompt },
    { id: "containers", label: "Containers", icon: Icon.containers },
];

export const PAGES = [
    { id: "profiles", label: "Profiles", icon: Icon.cube },
];

export const SECTIONS = [...TABS, ...PAGES];

// Sessions and profiles stand left of the home button, containers and
// terminals right of it, each side in this order.
const LEFT = ["sessions", "profiles"];
const RIGHT = ["containers", "terminals"];

// Nav draws the menu. A listener without the terminal has no terminals item:
// a button there would open a screen with nothing behind it.
export function Nav({ current, onSelect, home, terminals = true }) {
    const items = SECTIONS.filter((section) => terminals || section.id !== "terminals");
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
        </nav>
    `;
}
