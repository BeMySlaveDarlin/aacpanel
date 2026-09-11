// The profile map: contour → group → project.
import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { Pages } from "./sessions/pages.js";
import { ProfilePage } from "./profiles/page.js";
import { EditLayer } from "./profiles/forms.js";
import { LooseLayer } from "./profiles/loose.js";
import { looseOf } from "./profiles/pick.js";
import { orderOf, useProfileMap } from "./profiles/state.js";

// Profiles renders the profile map on the phone.
export function Profiles() {
    const map = useProfileMap();
    const {
        profiles, catalog, disk, error, gone,
        names, current, pick, profile,
        open, toggle, form, setForm, loose, setLoose,
        apply, remove, reorder,
    } = map;

    if (form) {
        return html`<${EditLayer}
            form=${form}
            profiles=${profiles}
            catalog=${catalog}
            disk=${disk}
            order=${orderOf(profiles, form, reorder)}
            onClose=${() => setForm(null)}
            onDone=${apply}
            onRemove=${remove}
        />`;
    }

    if (loose && profile) {
        return html`<${LooseLayer}
            profile=${profile}
            disk=${disk}
            profiles=${profiles}
            onBack=${() => setLoose(false)}
            onDone=${apply}
        />`;
    }

    return html`
        ${error && html`<p class="hint crit">${error}</p>`}
        ${profiles === null && !error && html`<p class="empty">Loading…</p>`}

        ${profiles && profiles.length === 0 && html`
            <p class="empty">There are no profiles yet. A profile is a contour with its own token and its own projects.</p>
        `}

        ${profiles && profiles.length > 0 && html`
            <${Pages}
                names=${names}
                current=${current}
                onPick=${pick}
                page=${(name) => {
                    const own = profiles.find((p) => p.name === name);
                    if (!own) return null;
                    return html`
                        <${ProfilePage}
                            key=${own.id}
                            profile=${own}
                            open=${open}
                            onToggle=${toggle}
                            onForm=${setForm}
                            gone=${gone}
                            loose=${looseOf(disk, profiles, name).length}
                            onLoose=${() => setLoose(true)}
                        />
                    `;
                }}
            />
        `}

        ${profiles && html`
            <button
                class="pffab"
                type="button"
                aria-label="add a contour"
                onClick=${() => setForm({ kind: "profile", mode: "add" })}
            >${Icon.plus()}</button>
        `}
    `;
}
