// The page of one contour: how it is authorized, its groups and its projects.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { summary } from "./launch.js";
import { authState, filterGroups } from "./pick.js";
import { GroupCard } from "./group.js";

// hooksWarning returns what to say when the contour settings drifted.
export function hooksWarning(profile) {
    if (profile.hooks === "diverged") {
        return "the settings drifted from the personal profile — questions from its sessions may not reach the panel";
    }
    if (profile.hooks === "unknown") {
        return "the profile settings were not read";
    }
    return "";
}

export function ProfilePage({ profile, open, onToggle, onForm, gone, loose, onLoose }) {
    const [query, setQuery] = useState("");
    const groups = profile.groups || [];
    const total = groups.reduce((n, group) => n + (group.projects || []).length, 0);
    const auth = authState(profile);
    const launch = summary(profile.launch);
    const warn = hooksWarning(profile);
    const shown = filterGroups(groups, query);

    return html`
        <div class="pfhead">
            <span class="dot ${auth.dot}"></span>
            <span class="pfheadname">${profile.name}</span>
            <span class="pfheadsub">${launch ? `${auth.text} · ${launch}` : auth.text}</span>
            <button
                class="pfact"
                type="button"
                aria-label=${`add a group to contour ${profile.name}`}
                onClick=${() => onForm({ kind: "group", mode: "add", profile })}
            >${Icon.plus()}</button>
            <button
                class="pfact"
                type="button"
                aria-label=${`edit contour ${profile.name}`}
                onClick=${() => onForm({ kind: "profile", mode: "edit", profile })}
            >${Icon.pencil()}</button>
        </div>

        ${warn && html`<p class="hint warn pfwarn">${warn}</p>`}

        ${total > 1 && html`
            <input
                class="search pfsearch"
                type="search"
                spellcheck="false"
                placeholder="project or path"
                value=${query}
                onInput=${(e) => setQuery(e.target.value)}
            />
        `}

        ${groups.length === 0
            ? html`<p class="empty">There are no groups yet. A group is a shelf to put projects on.</p>`
            : html`<div class="pfsub">project groups</div>`}

        ${groups.length > 0 && shown.length === 0 && html`
            <p class="hint">nothing found — neither in the names nor in the paths</p>
        `}

        ${shown.map((group) => html`
            <${GroupCard}
                key=${group.id}
                profile=${profile}
                group=${group}
                open=${open}
                onToggle=${onToggle}
                onForm=${onForm}
                gone=${gone}
                searching=${query.trim() !== ""}
            />
        `)}

        ${loose > 0 && html`
            <button class="pfloose" type="button" onClick=${onLoose}>
                <span class="pfloosetext">found on disk but not on the map</span>
                <span class="count">${loose}</span>
                <span class="chev">${Icon.chevron()}</span>
            </button>
        `}
    `;
}

