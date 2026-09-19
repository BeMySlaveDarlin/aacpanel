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
    if (type === "VERSION") {
        const port = event.ports && event.ports[0];
        if (port) port.postMessage({ version: VERSION });
    }
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
    event.waitUntil(self.registration.showNotification(payload.title || "aacpanel", {
        body: payload.body || "",
        tag: payload.tag || "aacpanel",
        renotify: true,
        requireInteraction: critical,
        icon: "/static/icons/icon-192.png",
        badge: "/static/icons/icon-192.png",
        data: { severity: payload.severity, ts: payload.ts, url: screenOf(payload) },
    }));
});

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
    const url = (event.notification.data && event.notification.data.url) || "";

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

function fetchWithin(request, ms = NET_MS) {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), ms);
    return fetch(request, { signal: ctl.signal }).finally(() => clearTimeout(timer));
}

async function navigation(request) {
    const cache = await caches.open(SHELL_CACHE);
    try {
        const response = await fetchWithin(request);
        if (response.ok && !response.redirected && new URL(request.url).pathname === SHELL_URL) {
            await cache.put(SHELL_URL, response.clone());
        }
        return response;
    } catch (err) {
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
        return response;
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
        return response;
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
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), NEAR_MS);
    try {
        const there = new URL(here.pathname + here.search, BASE);
        const r = await fetch(there, { mode: "cors", credentials: "omit", signal: ctl.signal });
        if (!r.ok) throw new Error(`response ${r.status}`);
        return new Response(r.body, { status: r.status, statusText: r.statusText, headers: r.headers });
    } catch (err) {
        BASE = "";
        await saveRoute();
        return null;
    } finally {
        clearTimeout(timer);
    }
}

async function asset(request) {
    const cache = await caches.open(SHELL_CACHE);
    const hit = await cache.match(request);
    const fresh = fetch(request)
        .then((response) => {
            if (response.ok) cache.put(request, response.clone());
            return response;
        })
        .catch(() => null);

    const response = hit || (await fresh);
    return response || new Response("", { status: 504, statusText: "no connection" });
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
