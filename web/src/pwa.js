// Service worker registration and update handling.

let reg = null;
let announced = null;
let reloading = false;
let applying = null;
let asked = false;
let installPrompt = null;

// A worker told to take over usually does so within a second. The browser
// holds it back for as long as the old worker has an event in flight — an
// answer still streaming to the page, a push renewal against a server that
// is restarting — and a reload from the page cannot hurry that along: under
// the old worker the page comes back with the same banner. So the page waits
// for the takeover and reloads only once the controller has changed; the
// reload after the grace is the last resort, for the case where what keeps
// the old worker busy is a request of this very page.
const takeoverGrace = 15000;
// controllerchange follows the activation; when it does not, the page reloads itself.
const changeGrace = 1000;

export async function register(onUpdate) {
    if (!("serviceWorker" in navigator)) return null;

    const hadController = Boolean(navigator.serviceWorker.controller);

    navigator.serviceWorker.addEventListener("controllerchange", () => {
        // The first worker takes the page over without a reload: nothing has
        // changed for the page yet. A takeover the page asked for is reloaded
        // regardless of how the page was loaded.
        if (!hadController && !asked) return;
        reload();
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
        announced = worker;
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

// apply asks the new worker to take over and settles once the page has been
// sent for a reload. A second call while the first is under way joins it.
export function apply() {
    if (!applying) applying = takeOver();
    return applying;
}

async function takeOver() {
    asked = true;
    const deadline = Date.now() + takeoverGrace;
    const told = new Set();
    for (;;) {
        const worker = candidate();
        if (!worker) {
            // Nobody to wait for: the banner is stale, and a reload brings up what is installed.
            reload();
            return;
        }
        if (worker.state === "installed" && !told.has(worker)) {
            told.add(worker);
            worker.postMessage({ type: "SKIP_WAITING" });
        }
        if (worker.state === "installing" || worker.state === "installed") {
            const left = deadline - Date.now();
            const state = left > 0 ? await nextState(worker, left) : null;
            if (state === null) {
                reload();
                return;
            }
            // "installed": the worker finished installing and is told next round.
            // "redundant": a newer worker replaced it and is picked next round.
            if (state !== "activating" && state !== "activated") continue;
        }
        await after(changeGrace);
        reload();
        return;
    }
}

// candidate is the worker the tap is about: the one waiting at the
// registration, else the one still installing, else the one the banner was
// shown for — unless it has gone redundant in the meantime.
function candidate() {
    if (reg) return live(reg.waiting) || live(reg.installing) || live(announced);
    return live(announced);
}

function live(worker) {
    return worker && worker.state !== "redundant" ? worker : null;
}

// nextState resolves with the worker's next state, or with null when none came within ms.
function nextState(worker, ms) {
    return new Promise((resolve) => {
        const done = (state) => {
            clearTimeout(timer);
            worker.removeEventListener("statechange", onChange);
            resolve(state);
        };
        const onChange = () => done(worker.state);
        const timer = setTimeout(() => done(null), ms);
        worker.addEventListener("statechange", onChange);
    });
}

function after(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

function reload() {
    if (reloading) return;
    reloading = true;
    location.reload();
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

// watchOpen hands the page the screen a tap on a notification asks for, when
// the worker could not take the window there itself.
export function watchOpen(onOpen) {
    if (!("serviceWorker" in navigator)) return;
    navigator.serviceWorker.addEventListener("message", (event) => {
        const data = event.data || {};
        if (data.type === "OPEN" && typeof data.url === "string") onOpen(data.url);
    });
}

export function clearData() {
    const worker = navigator.serviceWorker && navigator.serviceWorker.controller;
    if (worker) worker.postMessage({ type: "CLEAR_DATA" });
}
