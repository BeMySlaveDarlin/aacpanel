import { quiet, quietOf } from "./quiet.js";
import { renew } from "./renewal.js";

const VERSION = __VERSION__;
const ASSETS = __ASSETS__;

const SHELL_CACHE = `aacpanel-shell-${VERSION}`;
const DATA_CACHE = "aacpanel-data";

const SHELL_URL = "/app";

// A file on its way to a device goes around the worker: a data request is cut
// off after five seconds and its answer is kept in DATA_CACHE, and a file is
// neither short nor wanted twice — it would be saved to the phone through a
// copy of itself stored on the same phone.
const DOWNLOAD_URL = "/api/chat/file/download";

let ENDPOINTS = [];

let BASE = "";
let toldBase = false;

const ROUTE_CACHE = "aacpanel-route";
const ROUTE_KEY = "/__route";

self.addEventListener("install", (event) => {
    event.waitUntil(caches.open(SHELL_CACHE).then((cache) => cache.addAll(ASSETS)));
});

self.addEventListener("activate", (event) => {
    event.waitUntil((async () => {
        const names = await caches.keys();
        await Promise.all(
            names
                .filter((name) => name.startsWith("aacpanel-shell-") && name !== SHELL_CACHE)
                .map((name) => caches.delete(name)),
        );
        await self.clients.claim();
    })());
});

self.addEventListener("message", (event) => {
    const type = event.data && event.data.type;
    if (type === "SKIP_WAITING") self.skipWaiting();
    // Which version is actually running. A phone has no developer tools, so
    // this is the only way to tell a worker that stepped aside from one that
    // says it did.
    if (type === "VERSION") reply(event, { version: VERSION });
    // The page is handing itself to a newer worker, and the browser holds that
    // one back for as long as this one still streams an answer. Every request
    // still out is cut, the answer already on its way to a page with it, and
    // the page is told what was cut.
    if (type === "RELEASE") reply(event, { released: release() });
    // What this worker has out right now. An update that does not install has
    // to be explained on a phone, and nothing else there can see the worker.
    if (type === "INFLIGHT") reply(event, { live: listed() });
    if (type === "CLEAR_DATA") event.waitUntil(caches.delete(DATA_CACHE));
    if (type === "ENDPOINTS" && Array.isArray(event.data.origins)) {
        ENDPOINTS = event.data.origins.filter((o) => typeof o === "string");
    }
    if (type === "BASE" && typeof event.data.base === "string") {
        BASE = event.data.base;
        toldBase = true;
        event.waitUntil(saveRoute());
    }
});

function reply(event, data) {
    const port = event.ports && event.ports[0];
    if (port) port.postMessage(data);
}

async function saveRoute() {
    try {
        const cache = await caches.open(ROUTE_CACHE);
        await cache.put(ROUTE_KEY, new Response(JSON.stringify({ base: BASE }), {
            headers: { "Content-Type": "application/json" },
        }));
    } catch (err) {
    }
}

let loading = null;
function loadRoute() {
    if (!loading) {
        loading = (async () => {
            try {
                const cache = await caches.open(ROUTE_CACHE);
                const hit = await cache.match(ROUTE_KEY);
                if (!hit) return;
                const saved = await hit.json();
                if (!toldBase && saved && typeof saved.base === "string") BASE = saved.base;
            } catch (err) {
            }
        })();
    }
    return loading;
}

self.addEventListener("push", (event) => {
    if (!event.data) return;

    let payload;
    try {
        payload = event.data.json();
    } catch (err) {
        payload = { title: "aacpanel", body: event.data.text() };
    }

    const critical = payload.severity === "critical";
    const button = quietOf(payload);
    event.waitUntil(self.registration.showNotification(payload.title || "aacpanel", {
        body: payload.body || "",
        tag: payload.tag || "aacpanel",
        renotify: true,
        requireInteraction: critical,
        icon: ICON,
        badge: ICON,
        actions: button ? [{ action: "quiet", title: button.label }] : [],
        data: { severity: payload.severity, ts: payload.ts, url: screenOf(payload), quiet: button },
    }));
});

const ICON = "/static/icons/icon-192.png";

// screenOf is the panel screen a tap on the notification opens — a path of
// the panel and nothing else.
function screenOf(payload) {
    const url = payload.url;
    if (typeof url !== "string" || !url.startsWith("/") || url.startsWith("//")) return "";
    return url;
}

// The browser rotates the push subscription when the worker is replaced, and a
// new worker travels with every release. Without this handler the server keeps
// sending to the old endpoint, the push service answers that it is gone, and the
// server removes the device — while the browser still holds a live subscription
// and the screen says "on".
self.addEventListener("pushsubscriptionchange", (event) => {
    event.waitUntil(renew(self.registration));
});

self.addEventListener("notificationclick", (event) => {
    event.notification.close();
    const data = event.notification.data || {};
    if (event.action === "quiet" && data.quiet) {
        event.waitUntil(quiet(data.quiet, ICON));
        return;
    }
    const url = data.url || "";

    event.waitUntil((async () => {
        const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
        for (const client of clients) {
            if (!client.url.includes(self.location.origin)) continue;
            await client.focus();
            if (url) await show(client, url);
            return;
        }
        await self.clients.openWindow(url || SHELL_URL);
    })());
});

// show takes an open window of the panel to the screen: by driving the window
// where the browser lets the worker do that, by telling the page otherwise —
// a window the worker does not control refuses the navigation.
async function show(client, url) {
    if (typeof client.navigate === "function") {
        try {
            await client.navigate(url);
            return;
        } catch (err) {
        }
    }
    client.postMessage({ type: "OPEN", url });
}

self.addEventListener("fetch", (event) => {
    const request = event.request;
    if (request.method !== "GET") return;

    const url = new URL(request.url);

    if (url.pathname === DOWNLOAD_URL) return;

    if (request.headers.get("Accept") === "text/event-stream") return;

    if (url.origin !== self.location.origin) {
        if (url.pathname.startsWith("/api/") && ENDPOINTS.includes(url.origin)) event.respondWith(data(request));
        return;
    }

    if (request.mode === "navigate") {
        event.respondWith(navigation(request));
        return;
    }
    if (url.pathname.startsWith("/api/")) {
        event.respondWith(data(request));
        return;
    }
    if (url.pathname.startsWith("/dist/")) {
        event.respondWith(code(request));
        return;
    }
    if (isShellAsset(url.pathname)) {
        event.respondWith(asset(request));
    }
});

function isShellAsset(pathname) {
    return pathname.startsWith("/static/") || pathname === "/manifest.webmanifest";
}

const NET_MS = 5000;

// The requests the worker has out on the page's behalf, each from the moment
// it is sent until the page has had its body to the end, or it failed. The
// browser keeps a new worker waiting for as long as the old one streams an
// answer, and a body that stalls never ends: only the worker can cut what it
// fetched, so it keeps every request where a word from the page reaches it.
const live = new Set();
const sent = new WeakMap();

// fetchWithin asks the network on the page's behalf and counts the request
// among the live ones until its answer is handed on or given up. ms bounds the
// wait for the answer to begin; its body takes as long as it takes.
async function fetchWithin(request, ms = NET_MS) {
    const entry = { path: new URL(request.url).pathname, since: Date.now(), ctl: new AbortController() };
    live.add(entry);
    const timer = Number.isFinite(ms) ? setTimeout(() => entry.ctl.abort(), ms) : null;
    try {
        const response = await fetch(request, { signal: entry.ctl.signal });
        sent.set(response, entry);
        return response;
    } catch (err) {
        live.delete(entry);
        throw err;
    } finally {
        clearTimeout(timer);
    }
}

// handed passes an answer on to the page through the worker, which sees its
// body end and can still cut it: aborting the request errors the body the page
// is reading, and only then does the browser stop counting it as work. An
// answer without a body — a redirect the browser follows itself — goes as it is.
//
// The body is cut at the page's end, not through the pipe: a pipe told to
// stop first waits out the bytes it has in hand, and one that cancels its
// source waits for the copy kept for the cache to finish reading. A page that
// walks away from the answer ends the request for good — copy and all.
function handed(response) {
    const entry = sent.get(response);
    if (!entry) return response;
    const done = () => live.delete(entry);
    if (!response.body) {
        done();
        return response;
    }
    let gate = null;
    const pass = new TransformStream({ start: (controller) => { gate = controller; } });
    entry.ctl.signal.addEventListener("abort", () => gate.error(entry.ctl.signal.reason));
    response.body
        .pipeTo(pass.writable, { preventCancel: true })
        .catch(() => entry.ctl.abort())
        .then(done);
    return new Response(pass.readable, {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers,
    });
}

// forgo lets go of an answer nobody is going to read.
function forgo(response) {
    const entry = sent.get(response);
    if (!entry) return;
    entry.ctl.abort();
    live.delete(entry);
}

function listed() {
    const now = Date.now();
    return [...live].map((entry) => ({ path: entry.path, secs: Math.round((now - entry.since) / 1000) }));
}

// release cuts every request still out and says which they were.
function release() {
    const cut = listed();
    for (const entry of live) entry.ctl.abort();
    return cut;
}

async function navigation(request) {
    const cache = await caches.open(SHELL_CACHE);
    let response = null;
    try {
        response = await fetchWithin(request);
        if (response.ok && !response.redirected && new URL(request.url).pathname === SHELL_URL) {
            await cache.put(SHELL_URL, response.clone());
        }
        return handed(response);
    } catch (err) {
        if (response) forgo(response);
        const hit = await cache.match(SHELL_URL);
        if (hit) return mark(hit);
        return offlinePage();
    }
}

function dataKey(request) {
    const url = new URL(request.url);
    url.searchParams.delete("viewing");
    return self.location.origin + url.pathname + url.search;
}

async function data(request) {
    const cache = await caches.open(DATA_CACHE);
    const key = dataKey(request);
    try {
        const response = await fetchWithin(request);
        if (response.ok) keep(cache, key, response.clone());
        return handed(response);
    } catch (err) {
        const hit = await cache.match(key);
        if (hit) return mark(hit);
        throw err;
    }
}

// keep stores a copy of the answer for the time without a connection. The
// fetch event ends with the answer, not with the copy: the browser holds a
// new worker back for as long as the old one has an event in flight, and a
// body that stalls halfway would keep the event open — the update the page
// asked for would then wait for a connection nobody is going to close.
function keep(cache, key, response) {
    stamp(response).then((copy) => cache.put(key, copy)).catch(() => {});
}

const NEAR_MS = 2000;

async function code(request) {
    const cache = await caches.open(SHELL_CACHE);
    const near = await fromNear(request);
    if (near) return near;
    try {
        const response = await fetchWithin(request);
        if (response.ok) cache.put(request, response.clone());
        return handed(response);
    } catch (err) {
        const hit = await cache.match(request);
        if (hit) return mark(hit);
        return new Response("", { status: 504, statusText: "no connection" });
    }
}

async function fromNear(request) {
    await loadRoute();
    if (!BASE) return null;
    const here = new URL(request.url);
    try {
        const there = new URL(here.pathname + here.search, BASE);
        const r = await fetchWithin(new Request(there, { mode: "cors", credentials: "omit" }), NEAR_MS);
        if (!r.ok) {
            forgo(r);
            throw new Error(`response ${r.status}`);
        }
        // The answer is built anew on its way to the page, and that is what
        // code from another origin needs to be served as code of this one.
        return handed(r);
    } catch (err) {
        BASE = "";
        await saveRoute();
        return null;
    }
}

// asset answers a file of the shell from the cache and refreshes the copy
// behind it. The refresh reaches no page and holds nothing up; a file fetched
// for the page counts among the live requests and waits as long as it takes.
async function asset(request) {
    const cache = await caches.open(SHELL_CACHE);
    const hit = await cache.match(request);
    if (hit) {
        fetch(request).then((response) => (response.ok ? cache.put(request, response) : null)).catch(() => {});
        return hit;
    }
    try {
        const response = await fetchWithin(request, Infinity);
        if (response.ok) cache.put(request, response.clone());
        return handed(response);
    } catch (err) {
        return new Response("", { status: 504, statusText: "no connection" });
    }
}

async function stamp(response) {
    const headers = new Headers(response.headers);
    headers.set("X-Cached-At", new Date().toUTCString());
    return new Response(await response.blob(), {
        status: response.status,
        statusText: response.statusText,
        headers,
    });
}

function mark(response) {
    const headers = new Headers(response.headers);
    headers.set("X-From-Cache", "1");
    return new Response(response.body, {
        status: response.status,
        statusText: response.statusText,
        headers,
    });
}

function offlinePage() {
    const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>aacpanel — no connection</title>
<style>
  body { margin:0; min-height:100vh; display:grid; place-items:center; text-align:center;
         background:#06070f; color:#eef1ff; font:15px/1.5 system-ui, sans-serif; padding:24px; }
  h1 { font-size:18px; margin:0 0 8px; }
  p { margin:0; color:#8189b3; }
</style></head>
<body><div><h1>No connection</h1><p>The app shell has not been saved yet — open aacpanel at least once while online.</p></div></body></html>`;
    return new Response(html, {
        status: 503,
        headers: { "Content-Type": "text/html; charset=utf-8", "X-From-Cache": "1" },
    });
}
