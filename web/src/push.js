import { useCallback, useEffect, useState } from "preact/hooks";

import { toKey } from "./pushkey.js";

const KEY_URL = "/api/push/key";
const SUBSCRIPTION_URL = "/api/push/subscription";

export function supported() {
    return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

// usePush reports the subscription state: unsupported, denied, off or on.
export function usePush() {
    const [state, setState] = useState("off");
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    useEffect(() => {
        if (!supported()) {
            setState("unsupported");
            return;
        }
        if (Notification.permission === "denied") {
            setState("denied");
            return;
        }
        navigator.serviceWorker.ready
            .then((reg) => reg.pushManager.getSubscription())
            .then((sub) => {
                setState(sub ? "on" : "off");
                if (sub) return tellServer(sub);
            })
            .catch(() => setState("off"));
    }, []);

    const enable = useCallback(async () => {
        setBusy(true);
        setError("");
        try {
            const permission = await Notification.requestPermission();
            if (permission !== "granted") {
                setState(permission === "denied" ? "denied" : "off");
                return;
            }

            const keyResponse = await fetch(KEY_URL, { credentials: "same-origin" });
            if (keyResponse.status === 503) {
                throw new Error("the server is not ready to hand out keys yet, try again in a minute");
            }
            if (!keyResponse.ok) throw new Error(await message(keyResponse));

            const { key } = await keyResponse.json();
            const registration = await navigator.serviceWorker.ready;
            const subscription = await registration.pushManager.subscribe({
                userVisibleOnly: true,
                applicationServerKey: toKey(key),
            });

            const saved = await save(subscription);
            if (!saved.ok) throw new Error(await message(saved));

            setState("on");
        } catch (err) {
            setError(err.message || "could not subscribe");
        } finally {
            setBusy(false);
        }
    }, []);

    const forget = useCallback(async () => {
        const registration = await navigator.serviceWorker.ready;
        const subscription = await registration.pushManager.getSubscription();
        if (subscription) await subscription.unsubscribe();
        setState("off");
    }, []);

    return { state, error, busy, enable, forget };
}

// tellServer makes sure the server holds the subscription the browser holds.
// The browser rotates it when the worker is replaced, and the worker's own
// handler for that can be missed — the worker was stopped, the page was
// closed — so a subscription the server does not know, or knows under another
// endpoint, is posted again on every start, quietly: the screen already says
// "on", and that is true of the browser side. A server that cannot answer,
// or a session that cannot carry a subscription, leaves things as they are.
// Returns whether the subscription was posted.
export async function tellServer(subscription) {
    try {
        const known = await fetch(SUBSCRIPTION_URL, { credentials: "same-origin" });
        if (known.ok) {
            const { endpoint } = await known.json();
            if (endpoint === subscription.endpoint) return false;
        } else if (known.status !== 404) {
            return false;
        }
        const saved = await save(subscription);
        return saved.ok;
    } catch (err) {
        return false;
    }
}

function save(subscription) {
    return fetch(SUBSCRIPTION_URL, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(subscription),
    });
}

async function message(response) {
    const text = (await response.text()).trim();
    try {
        const parsed = JSON.parse(text);
        if (parsed && parsed.error) return parsed.error;
    } catch (err) {
    }
    return text || `the server answered ${response.status}`;
}
