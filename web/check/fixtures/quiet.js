// What a fixture waits on after it acts: the screen has come to rest. A pause
// guessed in milliseconds is either longer than the screen needs, and every
// run of the check pays for it, or shorter than a loaded machine gives the
// screen, and the fixture reads a screen still on its way. The screen says
// itself when it is done: the fake host has answered everything it was
// asked, nothing the page set off is due within a second, nothing moves, and
// the document stands still.

let asked = 0;

// counted wraps the fake host of a fixture: a request asked and not yet
// answered is something the screen still waits on.
export function counted(fake) {
    return (...args) => {
        asked += 1;
        return Promise.resolve()
            .then(() => fake(...args))
            .finally(() => { asked -= 1; });
    };
}

// An answer is read after it comes, and the read is a task of its own: on a
// loaded machine the screen still waits on the body of an answer frames after
// the answer itself came.
for (const read of ["json", "text", "arrayBuffer", "blob", "formData"]) {
    const plain = Response.prototype[read];
    Response.prototype[read] = function (...args) {
        asked += 1;
        return plain.apply(this, args).finally(() => { asked -= 1; });
    };
}

// A timer of the page due within soonMs is a step the screen is about to
// take — a debounced save, a search that waits for the typing to stop, a
// press held long enough — and the fixture waits for it as it would for an
// answer: a check that nothing happened is worth something only once the
// step that would have done it has run. A longer one — a toast's lifetime, a
// deadline, a poll — is the screen's own time, and a fixture that means to
// read past it says so with a pause of its own.
const soonMs = 1000;
const soon = new Set();
const setTimer = window.setTimeout.bind(window);
const clearTimer = window.clearTimeout.bind(window);
window.setTimeout = (fn, ms, ...args) => {
    if (typeof fn !== "function" || Number(ms) > soonMs) return setTimer(fn, ms, ...args);
    const id = setTimer((...given) => {
        soon.delete(id);
        fn(...given);
    }, ms, ...args);
    soon.add(id);
    return id;
};
window.clearTimeout = (id) => {
    soon.delete(id);
    clearTimer(id);
};

const frame = () => new Promise((resolve) => requestAnimationFrame(() => resolve()));

// arrive takes every transition and animation of the page that ends by itself
// to its end, and says whether there was one. What a fixture reads is where
// the screen comes to rest — the end of a transition is the style the rules
// give, and its end event fires as it would — so the run does not sit out the
// motion on the way. One that loops — a spinner, a pulse — never ends and is
// left alone.
function arrive() {
    let moved = false;
    for (const a of document.getAnimations()) {
        if (a.playState !== "running" || !a.effect || !Number.isFinite(a.effect.getComputedTiming().endTime)) continue;
        a.finish();
        moved = true;
    }
    return moved;
}

// quiet resolves with true once the fake host has answered everything, no
// timer of the page is due soon, nothing moves, and two frames in a row change
// nothing in the document: a render an answer sets off lands within a frame,
// an effect after it within the next. It gives up after ms with false, and
// the fixture reads what is there — a screen that never settles is a finding,
// not a hang.
export async function quiet(ms = 3000) {
    let changed = true;
    const seen = new MutationObserver(() => { changed = true; });
    seen.observe(document, { subtree: true, childList: true, attributes: true, characterData: true });
    const end = performance.now() + ms;
    try {
        while (performance.now() < end) {
            changed = arrive();
            await frame();
            await frame();
            if (!changed && asked === 0 && soon.size === 0) return true;
        }
        return false;
    } finally {
        seen.disconnect();
    }
}
