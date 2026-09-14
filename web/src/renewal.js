import { toKey } from "./pushkey.js";

const KEY_URL = "/api/push/key";
const SUBSCRIPTION_URL = "/api/push/subscription";

// DEADLINE_MS is how long the renewal may take. The browser keeps the old
// worker alive for as long as the event waits, so a server that is down or
// restarting would hold the release back: the panel offers a new version, the
// worker behind it never leaves, and the offer comes back. A renewal that ran
// out of time is dropped without a sound — the page checks the subscription
// against the server every time the panel opens, and that is the repeat.
const DEADLINE_MS = 12000;

// renew subscribes the browser again and tells the server. It runs inside the
// service worker, where there is no page and no gate to lean on: the worker
// fetches the key and posts the subscription itself, with the cookie of its
// origin. A failure of the server is let through, not swallowed — the worker's
// console is the only place it can be seen.
export async function renew(registration) {
    const control = new AbortController();
    let timer;
    const deadline = new Promise((done) => {
        timer = setTimeout(() => {
            control.abort();
            done(true);
        }, DEADLINE_MS);
    });
    const work = subscribeAgain(registration, control.signal).then(() => false);
    // The loser of the race is left behind, and a rejection nobody awaits is
    // reported by the worker as unhandled.
    work.catch(() => {});
    try {
        if (await Promise.race([work, deadline])) return;
    } catch (failure) {
        if (control.signal.aborted) return;
        throw failure;
    } finally {
        clearTimeout(timer);
    }
}

async function subscribeAgain(registration, signal) {
    const keyResponse = await fetch(KEY_URL, { credentials: "same-origin", signal });
    if (!keyResponse.ok) throw new Error(`the key request was answered ${keyResponse.status}`);
    const { key } = await keyResponse.json();

    const subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: toKey(key),
    });
    const saved = await fetch(SUBSCRIPTION_URL, {
        method: "POST",
        credentials: "same-origin",
        signal,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(subscription),
    });
    if (!saved.ok) throw new Error(`the subscription was answered ${saved.status}`);
}
