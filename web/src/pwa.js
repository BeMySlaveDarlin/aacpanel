// Service worker registration and update handling.

let waiting = null;
let reg = null;
let reloading = false;
let installPrompt = null;

const applyGrace = 2000;

export async function register(onUpdate) {
    if (!("serviceWorker" in navigator)) return null;

    const hadController = Boolean(navigator.serviceWorker.controller);

    navigator.serviceWorker.addEventListener("controllerchange", () => {
        if (!hadController || reloading) return;
        reloading = true;
        location.reload();
    });

    let registration;
    try {
        registration = await navigator.serviceWorker.register("/sw.js");
    } catch (err) {
        console.error("aacpanel: the service worker was not registered", err);
        return null;
    }
    reg = registration;

    const announce = (worker) => {
        if (!worker) return;
        waiting = worker;
        onUpdate();
    };

    if (registration.waiting && navigator.serviceWorker.controller) announce(registration.waiting);

    registration.addEventListener("updatefound", () => {
        const installing = registration.installing;
        if (!installing) return;
        installing.addEventListener("statechange", () => {
            if (installing.state === "installed" && navigator.serviceWorker.controller) announce(installing);
        });
    });

    document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "visible") registration.update().catch(() => {});
    });

    return registration;
}

export function apply() {
    const worker = (reg && reg.waiting) || waiting;
    if (worker) worker.postMessage({ type: "SKIP_WAITING" });
    setTimeout(() => {
        if (reloading) return;
        reloading = true;
        location.reload();
    }, applyGrace);
}

export function watchInstall(onChange) {
    window.addEventListener("beforeinstallprompt", (event) => {
        event.preventDefault();
        installPrompt = event;
        onChange(true);
    });
    window.addEventListener("appinstalled", () => {
        installPrompt = null;
        onChange(false);
    });
}

export async function install() {
    if (!installPrompt) return false;
    const prompt = installPrompt;
    installPrompt = null;
    prompt.prompt();
    const { outcome } = await prompt.userChoice;
    return outcome === "accepted";
}

export function tellEndpoints(origins) {
    const worker = navigator.serviceWorker && navigator.serviceWorker.controller;
    if (worker) worker.postMessage({ type: "ENDPOINTS", origins });
}

export function clearData() {
    const worker = navigator.serviceWorker && navigator.serviceWorker.controller;
    if (worker) worker.postMessage({ type: "CLEAR_DATA" });
}
