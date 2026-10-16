// Copying from the feed, from the file viewer and from the lists.

import { bytes } from "../../format.js";

// fromClick handles a click on the copy button and reports whether there was one.
export function fromClick(event, toast) {
    const btn = event.target.closest && event.target.closest(".mdcopy, .msgcopy");
    if (!btn) return false;

    if (btn.dataset.md) {
        copyText(btn.dataset.md, toast);
        return true;
    }

    const code = btn.closest(".mdcode");
    const text = code && code.querySelector("code");
    if (!text) return true;

    copyText(text.textContent, toast);
    return true;
}

// How long the word over copied code stands.
export const INLINE_TIP_MS = 1100;

// fromInline copies code in a line on a tap and reports whether it did. The
// word "copied" stands over the line the finger touched for a moment: the
// finger is there, and a toast would be far below it. A path is not copied —
// a tap on it opens the file — and a selection being made is left alone.
export function fromInline(event) {
    const code = event.target.closest && event.target.closest("code.copyable");
    if (!code || code.closest(".mdcode")) return false;
    const sel = document.getSelection();
    if (sel && !sel.isCollapsed && code.contains(sel.anchorNode)) return false;
    const box = [...code.getClientRects()]
        .find((r) => event.clientY >= r.top - 2 && event.clientY <= r.bottom + 2)
        || code.getBoundingClientRect();
    const say = (word, bad) => {
        code.classList.toggle("copied", !bad);
        const tip = document.createElement("div");
        tip.className = `copiedtip${bad ? " bad" : ""}`;
        tip.textContent = word;
        tip.style.left = `${Math.round(box.left + box.width / 2)}px`;
        tip.style.top = `${Math.round(box.top - 5)}px`;
        document.body.append(tip);
        setTimeout(() => {
            tip.remove();
            code.classList.remove("copied");
        }, INLINE_TIP_MS);
    };
    Promise.resolve()
        .then(() => navigator.clipboard.writeText(code.textContent))
        .then(() => say("copied", false), () => say("not copied", true));
    return true;
}

// filePick returns what is copied from the file viewer and what is said about it.
export function filePick(state) {
    const text = (state && state.text) || "";
    if (!text) return null;
    if (state.next > 0) {
        return {
            text,
            title: "Copied the shown chunk",
            sub: `${bytes(state.next)} of ${bytes(state.size)}`,
        };
    }
    return { text, title: "Copied", sub: state.name || lead(text) };
}

// fileCopy puts the shown contents into the clipboard.
export function fileCopy(state, toast) {
    const pick = filePick(state);
    if (!pick) return;
    copyText(pick.text, toast, pick.title, pick.sub);
}

// copyText puts any text into the clipboard and says so.
export async function copyText(text, toast, title = "Copied", sub = lead(text)) {
    try {
        await navigator.clipboard.writeText(text);
        toast(title, sub);
    } catch (err) {
        toast("Could not copy", "the clipboard is unavailable", true);
    }
}

// hostCopies lets what the program in a terminal copies reach the clipboard
// of this browser. tmux keeps a selection made with the mouse, and a copy
// from its own menu, in a buffer on the host and hands it to the terminal as
// OSC 52; a terminal that drops it leaves the text on the host. tmux hands it
// to every device that shows the pane: only the one in focus — the one the
// person works at — takes it, and a query of the clipboard is not answered.
export function hostCopies(term, toast) {
    return term.parser.registerOscHandler(52, (data) => {
        const text = osc52Text(data);
        if (text !== null && document.hasFocus()) copyText(text, toast);
        return true;
    });
}

// osc52Text is the text an OSC 52 hands over — "<where>;<base64>" — or null
// for an empty one and one that is not base64, a query (?) among them.
export function osc52Text(data) {
    const at = data.indexOf(";");
    const b64 = at < 0 ? "" : data.slice(at + 1);
    if (!b64) return null;
    try {
        return new TextDecoder().decode(Uint8Array.from(atob(b64), (c) => c.charCodeAt(0)));
    } catch {
        return null;
    }
}

// copyName copies the name of a container, as the host calls it.
export function copyName(name, toast) {
    return copyText(name, toast, "Name copied", name);
}

function lead(text) {
    const first = String(text).split("\n", 1)[0].trim();
    return first.length > 48 ? first.slice(0, 47) + "…" : first;
}
