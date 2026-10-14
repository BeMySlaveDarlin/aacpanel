// The terminal button of a conversation: the terminal of the session's
// project, typed into last, or a new one there when the project has none.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { latestIn, list } from "../../data/terms.js";
import { useTermActs } from "./acts.js";

// useTermJump asks the host for its terminals at the press rather than
// keeping them fresh behind every conversation: a list read a minute ago
// would start a second shell beside one opened since. press opens the
// terminal of the place; busy is true while it is being found or started.
export function useTermJump(place, exec, onTerm) {
    const acts = useTermActs(exec, list);
    const toast = useToast();
    const [busy, setBusy] = useState(false);

    const press = async () => {
        if (busy) return;
        setBusy(true);
        let terms;
        try {
            terms = await list();
        } catch (err) {
            setBusy(false);
            toast("The terminals did not come", String(err.message || err), true);
            return;
        }
        const last = latestIn(terms, place);
        const target = last ? { id: last.id, place: last.place } : await acts.start(place);
        setBusy(false);
        if (target) onTerm(target);
    };

    return { busy, press };
}

// TermJump is that terminal as a button of the conversation.
export function TermJump({ place, exec, onTerm }) {
    const jump = useTermJump(place, exec, onTerm);
    return html`
        <button class="wchip tjump" type="button" disabled=${jump.busy} onClick=${jump.press}
                aria-label="the terminal of the project: the one typed into last, or a new one">
            ${Icon.prompt()}
        </button>
    `;
}
