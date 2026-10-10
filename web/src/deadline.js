// The deadline of the requests the service worker sends on its own, inside an
// event of its own: a renewal of the push subscription, a press of the button
// that quiets a push.

// DEADLINE_MS is how long such a request may take. The browser keeps the old
// worker alive for as long as the event waits, and a newer worker waiting
// behind it: a server that is down or restarting — a release is exactly when
// it does — would hold the release back. The panel offers a new version, the
// worker behind it never leaves, and the offer comes back. The deadline stays
// under the grace the page gives a takeover, so an update tapped while such an
// event waits still comes up on that tap.
export const DEADLINE_MS = 12000;

// LATE is what within settles with when the deadline came first.
export const LATE = Symbol("late");

// within runs work with a signal that is aborted at the deadline, and settles
// by then whatever the work does: a request that does not hear the abort, or
// a step that hangs with no signal to hear, is left behind. A failure of the
// work is let through, unless it is the abort itself.
export async function within(work, ms = DEADLINE_MS) {
    const control = new AbortController();
    let timer;
    const deadline = new Promise((done) => {
        timer = setTimeout(() => {
            control.abort();
            done(LATE);
        }, ms);
    });
    const running = work(control.signal);
    // The loser of the race is left behind, and a rejection nobody awaits is
    // reported by the worker as unhandled.
    running.catch(() => {});
    try {
        return await Promise.race([running, deadline]);
    } catch (failure) {
        if (control.signal.aborted) return LATE;
        throw failure;
    } finally {
        clearTimeout(timer);
    }
}
