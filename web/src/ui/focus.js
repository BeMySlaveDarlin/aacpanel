// Who gets the keyboard when the screen changes.
import { WIDE } from "./wide.js";

// typing reports whether the person is standing in an input field right now.
export function typing() {
    const el = document.activeElement;
    if (!el) return false;
    if (el.isContentEditable) return true;
    return el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT";
}

// terminalOnScreen reports whether a live session terminal is on screen.
export function terminalOnScreen() {
    return Boolean(document.querySelector(".termwrap:not(.off)"));
}

// mayFocus reports whether the focus may be taken right now.
export function mayFocus() {
    if (!window.matchMedia(WIDE).matches) return false;
    return !typing();
}

function loose() {
    const el = document.activeElement;
    return !el || el === document.body || el === document.documentElement;
}

// regain returns the focus to a field a repaint took it from.
export function regain(el) {
    if (!el || !mayFocus() || !loose()) return false;
    el.focus();
    return true;
}
