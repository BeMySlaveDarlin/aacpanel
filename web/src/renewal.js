import { within } from "./deadline.js";
import { toKey } from "./pushkey.js";

const KEY_URL = "/api/push/key";
const SUBSCRIPTION_URL = "/api/push/subscription";

// renew subscribes the browser again and tells the server. It runs inside the
// service worker, where there is no page and no gate to lean on: the worker
// fetches the key and posts the subscription itself, with the cookie of its
// origin. A failure of the server is let through, not swallowed — the worker's
// console is the only place it can be seen. A renewal that ran out of time is
// dropped without a sound — the page checks the subscription against the
// server every time the panel opens, and that is the repeat.
export async function renew(registration) {
    await within((signal) => subscribeAgain(registration, signal));
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
