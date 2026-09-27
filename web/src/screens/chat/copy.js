// Copying from the feed and from the file viewer.

import { bytes } from "../../format.js";

// fromClick handles a click on the copy button and reports whether there was one.
export function fromClick(event, toast) {
    const btn = event.target.closest && event.target.closest(".mdcopy");
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

function lead(text) {
    const first = String(text).split("\n", 1)[0].trim();
    return first.length > 48 ? first.slice(0, 47) + "…" : first;
}
