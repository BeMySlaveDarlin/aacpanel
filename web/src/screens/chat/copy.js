// Copying from the feed and from the file viewer.

import { bytes } from "../../format.js";

// fromClick handles a click on the copy button and reports whether there was one.
export function fromClick(event, toast) {
    const btn = event.target.closest && event.target.closest(".mdcopy");
    if (!btn) return false;

    if (btn.dataset.md) {
        copy(btn.dataset.md, toast);
        return true;
    }

    const code = btn.closest(".mdcode");
    const text = code && code.querySelector("code");
    if (!text) return true;

    copy(text.textContent, toast);
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
    copy(pick.text, toast, pick.title, pick.sub);
}

async function copy(text, toast, title = "Copied", sub = lead(text)) {
    try {
        await navigator.clipboard.writeText(text);
        toast(title, sub);
    } catch (err) {
        toast("Could not copy", "the clipboard is unavailable", true);
    }
}

function lead(text) {
    const first = String(text).split("\n", 1)[0].trim();
    return first.length > 48 ? first.slice(0, 47) + "…" : first;
}
