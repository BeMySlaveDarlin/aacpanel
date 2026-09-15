// The project map in the right-hand panel of the Sessions section.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { plural } from "../../format.js";
import { useProfileMap, orderOf } from "../../screens/profiles/state.js";
import { EditLayer } from "../../screens/profiles/forms.js";
import { GroupRow } from "../profiles.js";

export function Projects({ picks, exec, onOpened }) {
    const {
        profiles, catalog, disk, error, gone,
        open, toggle, form, setForm,
        apply, remove, reorder,
    } = useProfileMap();
    const [query, setQuery] = useState("");

    const shown = (profiles || []).filter((p) => picks.length === 0 || picks.includes(p.name));

    if (error) return html`<p class="dkempty dkfail">${error}</p>`;
    if (profiles === null) return html`<p class="dkempty">loading…</p>`;

    const editing = form && html`<${EditLayer}
        form=${form}
        profiles=${profiles}
        catalog=${catalog}
        disk=${disk}
        order=${orderOf(profiles, form, reorder)}
        onClose=${() => setForm(null)}
        onDone=${apply}
        onRemove=${remove}
    />`;

    const match = (group) => {
        const text = query.trim().toLowerCase();
        if (!text) return true;
        if (group.name.toLowerCase().includes(text)) return true;
        return (group.projects || []).some((p) => `${p.name} ${p.path}`.toLowerCase().includes(text));
    };

    return html`
        <div class="dkscroll">
            ${shown.reduce((n, p) => n + (p.groups || []).length, 0) > 4 && html`
                <input
                    class="dkfind"
                    type="search"
                    placeholder="group or project"
                    value=${query}
                    onInput=${(e) => setQuery(e.target.value)}
                />
            `}
            ${shown.map((profile) => {
                const groups = (profile.groups || []).filter(match);
                return html`
                    <section key=${profile.id}>
                        <div class="dkcontour">
                            <span class="dkcontourname">${profile.name}</span>
                            <span class="dkcontournum">
                                ${(profile.groups || []).length} ${plural((profile.groups || []).length, "group", "groups")}
                            </span>
                            <span class="dkacts">
                                <i
                                    class="dkact"
                                    onClick=${() => setForm({ kind: "group", mode: "add", profile })}
                                ><${Icon.plus} /></i>
                                <i
                                    class="dkact"
                                    onClick=${() => setForm({ kind: "profile", mode: "edit", profile })}
                                ><${Icon.pencil} /></i>
                            </span>
                        </div>
                        ${groups.map((group) => html`
                            <${GroupRow}
                                key=${group.id}
                                profile=${profile}
                                group=${group}
                                expanded=${open.has(group.id)}
                                onToggle=${() => toggle(group.id)}
                                onForm=${setForm}
                                onRemove=${remove}
                                gone=${gone}
                                exec=${exec}
                                onOpened=${onOpened}
                            />
                        `)}
                        ${groups.length === 0 && html`<p class="dkempty dkunder">nothing matches the query</p>`}
                    </section>
                `;
            })}
            ${shown.length === 0 && html`
                <p class="dkempty">
                    ${picks.length ? "the picked contours have no projects" : "There are no contours yet. Groups and projects live inside a contour — add one below."}
                </p>
            `}
            <button class="dkadd" type="button" onClick=${() => setForm({ kind: "profile", mode: "add" })}>
                <${Icon.plus} /> new contour
            </button>
        </div>
        ${editing}
    `;
}
