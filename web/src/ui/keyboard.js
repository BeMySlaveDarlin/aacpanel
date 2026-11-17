// Whether the phone's own keyboard is open, as a mark on the page.
//
// Focus alone does not say it: a tapped button keeps the focus without any
// keyboard, and closing the keyboard with the back gesture leaves the field
// focused. The page is laid out with interactive-widget=resizes-content, so
// an open keyboard is a window shorter than the tallest one seen at the same
// width while a field is being typed into.
import { typing } from "./focus.js";

// KEYBOARD is the least height a keyboard takes; a smaller change is the
// browser's own bar coming and going.
const KEYBOARD = 150;

let width = 0;
let tallest = 0;

function update() {
    if (window.innerWidth !== width) {
        width = window.innerWidth;
        tallest = 0;
    }
    tallest = Math.max(tallest, window.innerHeight);
    const open = typing() && window.innerHeight < tallest - KEYBOARD;
    const root = document.documentElement;
    if (open) root.dataset.keyboard = "open";
    else delete root.dataset.keyboard;
}

// watchKeyboard keeps the mark current; it is safe to call more than once.
export function watchKeyboard() {
    if (watchKeyboard.on) return;
    watchKeyboard.on = true;
    window.addEventListener("resize", update);
    if (window.visualViewport) window.visualViewport.addEventListener("resize", update);
    document.addEventListener("focusin", update);
    document.addEventListener("focusout", () => setTimeout(update, 0));
    update();
}
