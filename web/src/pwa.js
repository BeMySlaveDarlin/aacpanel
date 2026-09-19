// Service worker registration and update handling.

let reg = null;
let announced = null;
let reloading = false;
let applying = null;
let asked = false;
let installPrompt = null;
let stuck = null;

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
// The page leaves itself a note about the reload it just sent for. A reload
// that changed nothing brings the page back to the same banner over the same
// worker, and the round after it ends where this one did — so the note is what
// tells the page it is going in circles. sessionStorage, not localStorage: the
// note belongs to this tab and goes away with it.
const markKey = "aacpanel:self-reload";
// How long the note speaks for the round the page is in: longer than a load,
// a banner, a tap and the grace put together, short enough that a tap made
// much later counts as a fresh attempt rather than the same circle.
const loopWindow = 60000;

export async function register(onUpdate) {
    if (!("serviceWorker" in navigator)) return null;

    const hadController = Boolean(navigator.serviceWorker.controller);

    navigator.serviceWorker.addEventListener("controllerchange", () => {
        // The first worker takes the page over without a reload: nothing has
        // changed for the page yet. A takeover the page asked for is reloaded
        // regardless of how the page was loaded.
        if (!hadController && !asked) return;
        reload("takeover");
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
    // Nothing is waiting: the page came up on the version it will run, so the
    // reload that brought it here did its job and is no circle to break.
    else forget();

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
// sent for a reload. A second call while the first is under way joins it. An
// attempt that ended without a reload is not kept: the banner is live again
// and the tap is the person's to repeat.
export function apply() {
    if (!applying) applying = takeOver().finally(() => { if (!reloading) applying = null; });
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
            reload("stale");
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
                reload("grace");
                return;
            }
            // "installed": the worker finished installing and is told next round.
            // "redundant": a newer worker replaced it and is picked next round.
            if (state !== "activating" && state !== "activated") continue;
        }
        await after(changeGrace);
        reload("takeover");
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

// reload sends the page for a reload and leaves the reason behind. A page
// that came back from its own reload over the same reason gains nothing by
// turning the round again, so it stays where it is and says so instead.
function reload(why) {
    if (reloading) return;
    const mark = read();
    if (mark && mark.why === why && Date.now() - mark.at < loopWindow) {
        if (stuck) stuck();
        return;
    }
    write({ why, at: Date.now() });
    reloading = true;
    location.reload();
}

// The storage can be absent altogether — a private window, site data switched
// off — and then reading it throws instead of answering. The guard is simply
// not armed in that case, and the page reloads as it would have anyway.
function read() {
    try {
        const raw = sessionStorage.getItem(markKey);
        const mark = raw ? JSON.parse(raw) : null;
        if (mark && typeof mark.why === "string" && typeof mark.at === "number") return mark;
    } catch {
        // no storage to read, or something else wrote over the note
    }
    return null;
}

function write(mark) {
    try {
        sessionStorage.setItem(markKey, JSON.stringify(mark));
    } catch {
        // nothing to remember the round by
    }
}

function forget() {
    try {
        sessionStorage.removeItem(markKey);
    } catch {
        // nothing was remembered
    }
}

// watchStuck hands the page the news that an update is not installing: the
// page has already come back from a reload over this one and will not turn
// the same round again.
export function watchStuck(onStuck) {
    stuck = onStuck;
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

// The version the page is running under, asked of the worker that controls it.
// Empty when no worker controls the page at all.
export async function runningVersion() {
    const worker = navigator.serviceWorker && navigator.serviceWorker.controller;
    if (!worker) return "";
    const answer = await new Promise((resolve) => {
        const channel = new MessageChannel();
        const timer = setTimeout(() => resolve(null), 2000);
        channel.port1.onmessage = (event) => {
            clearTimeout(timer);
            resolve(event.data);
        };
        try {
            worker.postMessage({ type: "VERSION" }, [channel.port2]);
        } catch {
            clearTimeout(timer);
            resolve(null);
        }
    });
    return answer && typeof answer.version === "string" ? answer.version : "";
}

// The version the server is serving. It is asked for at the root rather than
// under /dist/, because the worker answers for /dist/ out of its own cache:
// a device stuck on an old build would be told its own version back. Past
// every other cache too — the point of the question is the comparison.
export async function servedVersion() {
    try {
        const answer = await fetch("/version", { cache: "no-store", credentials: "same-origin" });
        if (!answer.ok) return "";
        const body = await answer.json();
        return body && typeof body.version === "string" ? body.version : "";
    } catch {
        return "";
    }
}

// scrub takes the worker off this device: the registrations go, and with them
// the caches the panel keeps. It is the way out of a worker that will not step
// aside — on a phone there is nothing else to unregister one with, and closing
// the application does not always do it.
export async function scrub() {
    const gone = { workers: 0, caches: [] };
    if ("serviceWorker" in navigator) {
        const found = await navigator.serviceWorker.getRegistrations().catch(() => []);
        for (const registration of found) {
            if (await registration.unregister().catch(() => false)) gone.workers += 1;
        }
    }
    if (typeof caches !== "undefined") {
        const names = await caches.keys().catch(() => []);
        for (const name of names) {
            // Only what this panel put there: a browser keeps the caches of
            // every site in one place, and the rest of them are not ours.
            if (!name.startsWith("aacpanel-")) continue;
            if (await caches.delete(name).catch(() => false)) gone.caches.push(name);
        }
    }
    return gone;
}

// reinstall scrubs the worker and brings the page back from the server. The
// note about the reload goes with it: this reload is the person's doing, and
// the guard against going in circles has nothing to guard here.
export async function reinstall() {
    const gone = await scrub();
    forget();
    reloading = true;
    location.reload();
    return gone;
}

export function clearData() {
    const worker = navigator.serviceWorker && navigator.serviceWorker.controller;
    if (worker) worker.postMessage({ type: "CLEAR_DATA" });
}
