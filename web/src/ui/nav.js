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

// What talks to the machine stands left of the home button — its sessions and
// its terminals — and what it runs and how it is mapped stands right of it.
const LEFT = new Set(["sessions", "terminals"]);

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

    return html`
        <nav class="tabs">
            ${items.filter((section) => LEFT.has(section.id)).map(button)}
            <div class="homeslot">${home}</div>
            ${items.filter((section) => !LEFT.has(section.id)).map(button)}
        </nav>
    `;
}
