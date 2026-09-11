// Routes every /api/* request through the base address chosen by the router.

export const TOKEN_MS = 60 * 60 * 1000;
export const TOKEN_WAIT_MS = 5000;

const nativeFetch = typeof window !== "undefined" && window.fetch ? window.fetch.bind(window) : null;
const NativeEventSource = typeof window !== "undefined" ? window.EventSource : undefined;

let base = "";
let via = "";
let token = "";
let timer = null;
let onFail = null;

// direct issues a fetch against the home origin, bypassing the interception.
export function direct(input, init) {
    return nativeFetch(input, init);
}

// current reports the base requests go through right now.
export function current() {
    return { base, via };
}

function apiPath(url) {
    return typeof url === "string" && url.startsWith("/api/") ? url : "";
}

function streamURL(path) {
    if (!token) return base + path;
    return base + path + (path.includes("?") ? "&" : "?") + "token=" + encodeURIComponent(token);
}

async function routed(path, init) {
    const headers = new Headers((init && init.headers) || undefined);
    if (token) headers.set("Authorization", "Bearer " + token);
    const dead = base;
    try {
        return await nativeFetch(base + path, { ...init, headers, mode: "cors", credentials: "omit" });
    } catch (err) {
        if (!(err && err.name === "AbortError") && dead === base && onFail) onFail(dead, err);
        throw err;
    }
}

// install puts the interception in place; call it once before the first request.
export function install() {
    if (!nativeFetch) return;
    window.fetch = (input, init) => {
        const path = apiPath(input);
        return base && path ? routed(path, init) : nativeFetch(input, init);
    };
    if (NativeEventSource) {
        window.EventSource = class extends NativeEventSource {
            constructor(url, opts) {
                const path = apiPath(url);
                super(base && path ? streamURL(path) : url, opts);
            }
        };
    }
}

// fetchToken asks the home origin for a bearer token; empty means none was issued.
export async function fetchToken() {
    const ctl = new AbortController();
    const wait = setTimeout(() => ctl.abort(), TOKEN_WAIT_MS);
    try {
        const r = await nativeFetch("/api/session/token", {
            credentials: "same-origin", cache: "no-store", signal: ctl.signal,
            headers: { Accept: "application/json" },
        });
        if (!r.ok) return "";
        const body = await r.json();
        return body && typeof body.token === "string" ? body.token : "";
    } catch (err) {
        return "";
    } finally {
        clearTimeout(wait);
    }
}

function tellWorker() {
    try {
        const worker = navigator.serviceWorker && navigator.serviceWorker.controller;
        if (worker) worker.postMessage({ type: "BASE", base });
    } catch (err) {
    }
}

// use applies the router's choice: the base, its kind and the bearer token.
export function use({ base: next = "", via: kind = "", token: fresh = "" } = {}) {
    base = next || "";
    via = base ? kind : "";
    token = base ? fresh : "";
    tellWorker();
    if (timer) {
        clearInterval(timer);
        timer = null;
    }
    if (!base) return;
    timer = setInterval(async () => {
        const renewed = await fetchToken();
        if (renewed) token = renewed;
    }, TOKEN_MS);
}

// watchFailures registers the callback for network failures on the base.
export function watchFailures(cb) {
    onFail = cb;
}
