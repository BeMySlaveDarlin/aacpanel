// What is done to terminals of places. Every press goes through the gate and
// is asked of the host first: an executor that does not know the action says
// why instead of taking the press and doing nothing.
import { useAction } from "../../actions/gate.js";
import { hostLabel } from "../../actions/registry.js";
import { knows, whyNot } from "../../exec.js";
import { useToast } from "../../ui/toasts.js";
import { samePlace, tabName } from "../../data/terms.js";

// newestIn is the terminal of a place started last.
function newestIn(terms, place) {
    let best = null;
    for (const t of terms || []) {
        if (samePlace(t.place, place) && (!best || (t.created || 0) > (best.created || 0))) best = t;
    }
    return best;
}

// useTermActs returns the four things done to a terminal. reload asks the
// list again and returns it: a started terminal is opened from the list, so
// its tab stands among the others at once.
export function useTermActs(exec, reload) {
    const run = useAction();
    const toast = useToast();
    const can = {
        start: knows(exec, "term.start"),
        close: knows(exec, "term.close"),
        rename: knows(exec, "term.rename"),
        console: knows(exec, "term.console"),
    };
    const why = {
        start: whyNot(exec, "term.start"),
        close: whyNot(exec, "term.close"),
        rename: whyNot(exec, "term.rename"),
        console: whyNot(exec, "term.console"),
    };

    // start opens a shell in the place and answers the terminal to open, or
    // null when there is none.
    const start = async (place) => {
        if (!can.start) {
            toast("A terminal cannot be started", why.start, true);
            return null;
        }
        const result = await run("term.start", place, { place });
        if (!result.ok) return null;
        const id = result.data && result.data.id;
        const terms = await reload();
        if (id) return { id, place };
        const made = newestIn(terms, place);
        return made ? { id: made.id, place } : null;
    };

    const close = async (t) => {
        const result = await run("term.close", t.id, { id: t.id });
        if (result.ok) await reload();
        return result.ok;
    };

    const rename = async (t, name) => {
        const result = await run("term.rename", t.id, { id: t.id, name });
        if (result.ok) await reload();
        return result.ok;
    };

    // A window is the only one of the four whose consequence is not on the
    // screen: it opens on the desktop of the machine, so the screen says so.
    const window = async (t) => {
        const result = await run("term.console", t.id, { id: t.id });
        if (result.ok) toast(`A window opened on ${hostLabel()}`, tabName(t));
        return result.ok;
    };

    return { can, why, start, close, rename, window };
}
