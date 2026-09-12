// The desktop tooltip: one node for the whole shell, placed by script.
//
// A pseudo-element under the button cannot do this job: it inherits the
// opacity of a dimmed button, is clipped by the scrolling column it sits in,
// and has no way to learn where the screen ends. One node at the shell root
// is dimmed by nothing, sits above every column, and is moved back inside
// the viewport after measuring.

// GAP is the distance between the element and its tip, MARGIN the least
// distance the tip keeps from any edge of the viewport.
const GAP = 6;
const MARGIN = 8;

// attachTips shows the data-tip of whatever is hovered or keyboard-focused
// inside the shell in the given node, and returns the function that stops it.
export function attachTips(shell, tip) {
    let shown = null;

    const hide = () => {
        shown = null;
        tip.classList.remove("on");
    };

    const show = (el) => {
        const text = el.getAttribute("data-tip");
        if (!text) {
            hide();
            return;
        }
        shown = el;
        tip.textContent = text;
        tip.classList.add("on");
        // The node has to be visible with its final text before it is measured:
        // the size of a hidden or empty box says nothing about the tip.
        const { x, y } = placeTip(
            el.getBoundingClientRect(),
            tip.getBoundingClientRect(),
            el.getAttribute("data-tipside") === "left",
            window.innerWidth,
            window.innerHeight,
        );
        tip.style.transform = `translate(${x}px, ${y}px)`;
    };

    const over = (e) => {
        const el = e.target instanceof Element ? e.target.closest("[data-tip]") : null;
        if (!el) {
            if (shown) hide();
            return;
        }
        // Focus that came from a click has just hidden the tip on purpose;
        // only focus that arrived by keyboard is a request to read it.
        if (e.type === "focusin" && !el.matches(":focus-visible")) return;
        if (el !== shown) show(el);
    };

    const out = (e) => {
        if (!shown) return;
        if (e.relatedTarget instanceof Node && shown.contains(e.relatedTarget)) return;
        hide();
    };

    shell.addEventListener("pointerover", over);
    shell.addEventListener("pointerout", out);
    shell.addEventListener("focusin", over);
    shell.addEventListener("focusout", out);
    // A click changes what the button is about, and a scroll in any column
    // moves the element from under the tip; scroll does not bubble, so capture.
    shell.addEventListener("pointerdown", hide, true);
    shell.addEventListener("scroll", hide, true);

    return () => {
        shell.removeEventListener("pointerover", over);
        shell.removeEventListener("pointerout", out);
        shell.removeEventListener("focusin", over);
        shell.removeEventListener("focusout", out);
        shell.removeEventListener("pointerdown", hide, true);
        shell.removeEventListener("scroll", hide, true);
        hide();
    };
}

// placeTip returns where a tip of the given size goes beside the element:
// under it and centred, or beside it on the left when asked. When that side
// has no room the tip takes the other one, and either way it stays at least
// MARGIN away from every edge of the viewport.
export function placeTip(at, size, left, vw, vh) {
    let x;
    let y;
    if (left) {
        x = at.left - GAP - size.width;
        y = at.top + at.height / 2 - size.height / 2;
        if (x < MARGIN) x = at.right + GAP;
    } else {
        x = at.left + at.width / 2 - size.width / 2;
        y = at.bottom + GAP;
        if (y + size.height > vh - MARGIN) y = at.top - GAP - size.height;
    }
    x = Math.round(Math.min(Math.max(x, MARGIN), vw - MARGIN - size.width));
    y = Math.round(Math.min(Math.max(y, MARGIN), vh - MARGIN - size.height));
    return { x, y };
}
