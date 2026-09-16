// The bottom menu: the main navigation of the phone layout.
import { html } from "../html.js";
import { Icon } from "./icons.js";

export const TABS = [
    { id: "sessions", label: "Sessions", icon: Icon.sessions },
    { id: "containers", label: "Containers", icon: Icon.containers },
];

export const PAGES = [
    { id: "briefs", label: "Briefs", icon: Icon.file },
    { id: "profiles", label: "Profiles", icon: Icon.cube },
];

export const SECTIONS = [...TABS, ...PAGES];

export function Nav({ current, onSelect, home }) {
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
            ${TABS.map(button)}
            <div class="homeslot">${home}</div>
            ${PAGES.map(button)}
        </nav>
    `;
}
