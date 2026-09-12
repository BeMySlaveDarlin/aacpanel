import { toKey } from "./pushkey.js";

const KEY_URL = "/api/push/key";
const SUBSCRIPTION_URL = "/api/push/subscription";

// renew subscribes the browser again and tells the server. It runs inside the
// service worker, where there is no page and no gate to lean on: the worker
// fetches the key and posts the subscription itself, with the cookie of its
// origin. A failure is let through, not swallowed — the worker's console is
// the only place it can be seen.
export async function renew(registration) {
    const keyResponse = await fetch(KEY_URL, { credentials: "same-origin" });
    if (!keyResponse.ok) throw new Error(`the key request was answered ${keyResponse.status}`);
    const { key } = await keyResponse.json();

    const subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: toKey(key),
    });
    const saved = await fetch(SUBSCRIPTION_URL, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(subscription),
    });
    if (!saved.ok) throw new Error(`the subscription was answered ${saved.status}`);
}
