// Picks the nearest answering panel address for API requests.

export const ENDPOINTS_KEY = "aacpanel.endpoints";
export const BASE_KEY = "aacpanel.apibase";
export const PROBE_MS = 5000;
export const PROMPT_MS = 30000;

export const LABELS = { local: "localhost", lan: "LAN", public: "domain", ts: "tailscale" };

// closer lists the addresses nearer than the current one, in map order.
export function closer(here, endpoints) {
    const list = Array.isArray(endpoints) ? endpoints : [];
    const at = list.findIndex((e) => e && e.kind === here);
    return at < 0 ? list.slice() : list.slice(0, at);
}

// choose returns the first candidate that answered as this very panel.
export function choose(candidates, results, panel) {
    for (const e of candidates) {
        const r = results.get(e.url);
        if (r && panel && r.panel === panel && r.kind === e.kind) return e;
    }
    return null;
}

// usable reports whether the page may reach this address at all.
export function usable(e, protocol) {
    if (!e || typeof e.url !== "string") return false;
    if (protocol !== "https:" || e.url.startsWith("https://")) return true;
    return /^http:\/\/(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/i.test(e.url);
}

// probe asks an address whether it is this panel; null means it is not.
export async function probe(url, { fetch: f = fetch, timeout = PROBE_MS } = {}) {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), timeout);
    try {
        const r = await f(url + "/probe", {
            mode: "cors", credentials: "omit", cache: "no-store", signal: ctl.signal,
        });
        if (!r.ok) return null;
        const body = await r.json();
        return body && typeof body.panel === "string" ? { panel: body.panel, kind: body.kind } : null;
    } catch (err) {
        return null;
    } finally {
        clearTimeout(timer);
    }
}

async function promptPending(permissions) {
    try {
        const p = await permissions.query({ name: "local-network-access" });
        return p.state === "prompt";
    } catch (err) {
        return false;
    }
}

function storedMap(storage) {
    try {
        const map = JSON.parse(read(storage, ENDPOINTS_KEY) || "null");
        return map && Array.isArray(map.endpoints) ? map : null;
    } catch (err) {
        return null;
    }
}

async function loadMap(f, storage) {
    let map = null;
    try {
        const r = await f("/api/endpoints", { credentials: "same-origin", headers: { Accept: "application/json" } });
        if (r.ok) map = await r.json();
    } catch (err) {
        map = null;
    }
    if (map && Array.isArray(map.endpoints)) {
        write(storage, ENDPOINTS_KEY, JSON.stringify(map));
        return map;
    }
    return storedMap(storage);
}

// failed reports whether an address that just refused a request should be dropped.
export async function failed(url, { fetch: f = fetch, storage = localStorage } = {}) {
    if (!url) return false;
    const answer = await probe(url, { fetch: f });
    if (!answer) return true;
    const map = storedMap(storage);
    if (!map) return false;
    const known = map.endpoints.find((e) => e && e.url === url);
    return answer.panel !== map.panel || (Boolean(known) && answer.kind !== known.kind);
}

// pick chooses the base for API requests and the bearer token for it.
export async function pick({
    fetch: f = fetch,
    storage = localStorage,
    permissions = typeof navigator !== "undefined" ? navigator.permissions : null,
    token = async () => "",
    protocol = typeof location !== "undefined" ? location.protocol : "https:",
    avoid = "",
} = {}) {
    const previous = read(storage, BASE_KEY);
    const map = await loadMap(f, storage);
    if (!map) return { here: "", via: "", base: "", token: "", why: "no-map", previous, endpoints: [] };
    const here = map.here || "";
    const endpoints = map.endpoints.filter((e) => e && typeof e.kind === "string" && typeof e.url === "string");
    const done = (via, base, bearer, why) => {
        write(storage, BASE_KEY, base);
        return { here, via, base, token: bearer, why, previous, endpoints };
    };
    const candidates = endpoints.filter((e) => e.url !== avoid && (e.kind === here || usable(e, protocol)));
    if (!candidates.some((e) => e.kind !== here)) return done("", "", "", "alone");

    const timeout = permissions && await promptPending(permissions) ? PROMPT_MS : PROBE_MS;
    const probes = new Map(candidates.map((e) => [e.url, probe(e.url, { fetch: f, timeout })]));
    let bearer = null;
    let skipped = false;
    for (const e of candidates) {
        const r = await probes.get(e.url);
        if (!r || r.panel !== map.panel || r.kind !== e.kind) continue;
        if (e.kind === here) return done("", "", "", skipped ? "no-token" : "here");
        if (bearer === null) {
            try {
                bearer = (await token()) || "";
            } catch (err) {
                bearer = "";
            }
        }
        if (!bearer && e.kind !== "local") {
            skipped = true;
            continue;
        }
        return done(e.kind, e.url, bearer, e.kind);
    }
    return done("", "", "", skipped ? "no-token" : "silent");
}

// measure pings every address of the map from where the panel is open.
export async function measure({ fetch: f = fetch, storage = localStorage, now = () => performance.now() } = {}) {
    const map = await loadMap(f, storage);
    if (!map) return { here: "", rows: [] };
    const rows = await Promise.all(map.endpoints.map(async (e) => {
        const t0 = now();
        const r = await probe(e.url, { fetch: f });
        const ok = Boolean(r) && r.panel === map.panel && r.kind === e.kind;
        return { kind: e.kind, url: e.url, ok, ms: ok ? Math.max(1, Math.round(now() - t0)) : null };
    }));
    return { here: map.here || "", rows };
}

function read(storage, key) {
    try {
        return storage.getItem(key) || "";
    } catch (err) {
        return "";
    }
}

function write(storage, key, value) {
    try {
        if (value) storage.setItem(key, value);
        else storage.removeItem(key);
    } catch (err) {
    }
}
