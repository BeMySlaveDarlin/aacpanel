// Service worker registration and update handling.

import { hush, unhush } from "./api.js";

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
// attempt that ended without a reload is not kept: the banner is live again,
// the page asks the api again, and the tap is the person's to repeat.
export function apply() {
    if (!applying) {
        applying = takeOver().finally(() => {
            if (reloading) return;
            applying = null;
            unhush();
        });
    }
    return applying;
}

// takeOver holds the page's requests of the api from the tap on: a page that
// keeps one out through the old worker at any moment keeps the new one
// waiting, and only a page that has gone quiet is handed over at once.
async function takeOver() {
    asked = true;
    hush();
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
            letGo();
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

// letGo asks the worker that controls the page to cut every request it still
// has out: an answer it streams to any page holds the new worker back, and
// only the old worker can end it. It is asked as the new worker is told to
// take over, not at the tap — what the page fetches while the new one installs
// would hold it back again. The takeover follows from the old worker going
// quiet, not from its answer, so none is waited for: a worker from before the
// question does not know it, and the page waits the grace out as it always did.
function letGo() {
    const worker = navigator.serviceWorker.controller;
    if (worker) worker.postMessage({ type: "RELEASE" });
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
// turning the round again, so it stays where it is and says so instead, with
// what the old worker still has out when it can tell.
function reload(why) {
    if (reloading) return;
    const mark = read();
    if (mark && mark.why === why && Date.now() - mark.at < loopWindow) {
        if (stuck) holding().then(stuck);
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
// the same round again. The news comes with what holding found.
export function watchStuck(onStuck) {
    stuck = onStuck;
}

// holding names as many requests as a line of the note has room for, and
// gives the worker a second to answer: one that does not know the question
// never will.
const heldShown = 3;
const heldWait = 1000;

// holding asks the worker that controls the page which requests it still has
// out, the longest-running first — on a phone the one way to see what keeps
// an update from installing. null when no worker answered: one from before
// the question does not know it, and that is not the same as having nothing.
async function holding() {
    const answer = await ask("INFLIGHT", heldWait);
    if (!answer || !Array.isArray(answer.live)) return null;
    return answer.live.sort((a, b) => b.secs - a.secs).slice(0, heldShown);
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
    const answer = await ask("VERSION", 2000);
    return answer && typeof answer.version === "string" ? answer.version : "";
}

// ask puts a question to the worker that controls the page over a channel of
// its own, apart from the rest of what the worker says, and resolves with the
// answer — or with null when no worker controls the page or none answered
// within ms.
function ask(type, ms) {
    const worker = navigator.serviceWorker && navigator.serviceWorker.controller;
    if (!worker) return Promise.resolve(null);
    return new Promise((resolve) => {
        const channel = new MessageChannel();
        const done = (answer) => {
            clearTimeout(timer);
            channel.port1.close();
            resolve(answer);
        };
        const timer = setTimeout(() => done(null), ms);
        channel.port1.onmessage = (event) => done(event.data);
        try {
            worker.postMessage({ type }, [channel.port2]);
        } catch {
            done(null);
        }
    });
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
