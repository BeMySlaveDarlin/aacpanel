import { useCallback, useEffect, useState } from "preact/hooks";

function toKey(base64) {
    const pad = base64.replace(/-/g, "+").replace(/_/g, "/");
    const raw = atob(pad + "=".repeat((4 - (pad.length % 4)) % 4));
    return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

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
            .then((sub) => setState(sub ? "on" : "off"))
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

            const keyResponse = await fetch("/api/push/key", { credentials: "same-origin" });
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

            const saved = await fetch("/api/push/subscription", {
                method: "POST",
                credentials: "same-origin",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(subscription),
            });
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

async function message(response) {
    const text = (await response.text()).trim();
    try {
        const parsed = JSON.parse(text);
        if (parsed && parsed.error) return parsed.error;
    } catch (err) {
    }
    return text || `the server answered ${response.status}`;
}
