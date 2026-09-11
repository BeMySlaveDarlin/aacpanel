// Loads the container tree and the host snapshot for the shell.
import { clearData } from "./pwa.js";
import { viewing } from "./viewing.js";

export async function loadTree() {
    const response = await fetch("/api/tree", { headers: { Accept: "application/json" } });

    if (response.status === 401) {
        clearData();
        const err = new Error("the session has ended");
        err.unauthorized = true;
        throw err;
    }
    if (!response.ok) throw failure(response, await response.text());

    const fromCache = response.headers.get("X-From-Cache") === "1";
    const cachedAt = response.headers.get("X-Cached-At");

    return {
        tree: await response.json(),
        stale: fromCache,
        at: cachedAt ? new Date(cachedAt) : new Date(),
    };
}

function failure(response, body) {
    const text = (body || "").trim();

    let parsed = null;
    try {
        parsed = JSON.parse(text);
    } catch (err) {
        parsed = null;
    }

    const err = new Error((parsed && parsed.error) || text || `the server answered ${response.status}`);
    err.status = response.status;
    err.unauthorized = response.status === 401;
    if (parsed) {
        err.reason = parsed.reason || "";
        err.afterSec = parsed.after_sec || 0;
    }
    return err;
}

function hostURL() {
    const seen = viewing();
    return seen ? `/api/host?viewing=${encodeURIComponent(seen)}` : "/api/host";
}

export async function loadHost() {
    const response = await fetch(hostURL(), { headers: { Accept: "application/json" } });

    if (response.status === 401) {
        clearData();
        const err = new Error("the session has ended");
        err.unauthorized = true;
        throw err;
    }
    if (!response.ok) throw failure(response, await response.text());

    const snapshot = await response.json();
    return {
        snapshot,
        stale: response.headers.get("X-From-Cache") === "1",
        ageSec: snapshot.at ? Math.max(0, Date.now() / 1000 - snapshot.at) : null,
    };
}
