// An update tapped while the old worker streams answers that never end: an
// answer of the api from the nearest panel and a file of code. The test
// bundles this page with the panel's own modules and serves both versions of
// the worker; the page leaves what it saw in window.result, on the load that
// saw it — the takeover reloads the page, and the note in sessionStorage is
// what carries the tap over to the load after it.
import { render } from "preact";

import { html } from "../../src/html.js";
import { ToastHost } from "../../src/ui/toasts.js";
import { UpdateStuck } from "../../src/main.js";
import * as pwa from "../../src/pwa.js";

const params = new URLSearchParams(location.search);
const NEAR = params.get("near");
// "takeover": both versions are the panel's own worker. "silent": the old one
// is a worker from before the questions about what it has out. "answers": the
// old one answers what it has out but cannot be made to let go.
const old = params.get("old");
const noteKey = "fixture";
const reloadKey = "aacpanel:self-reload";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function until(check, ms) {
    const end = Date.now() + ms;
    for (;;) {
        const value = await check();
        if (value) return value;
        if (Date.now() > end) return null;
        await sleep(25);
    }
}

async function firstLoad() {
    let announced = false;
    const registration = await pwa.register(() => { announced = true; });
    if (!(await until(() => navigator.serviceWorker.controller, 10000))) return { error: "no worker took the page" };
    pwa.tellEndpoints([NEAR]);
    navigator.serviceWorker.controller.postMessage({ type: "BASE", base: NEAR });
    // The worker has heard both once it answers a question asked after them.
    if (old === "takeover") await pwa.runningVersion();

    fetch(NEAR + "/api/stall?session=a").then((r) => r.text()).catch(() => {});
    const script = document.createElement("script");
    script.src = "/dist/stall.js?v=1";
    document.head.append(script);
    const stalled = await until(async () => (await (await fetch("/held")).json()).count >= 2, 10000);
    if (!stalled) return { error: "the answers never reached the servers" };

    await fetch("/bump");
    await registration.update();
    if (!(await until(() => announced, 10000))) return { error: "the new version was never offered" };

    let stuck = false;
    render(html`<${ToastHost}><${UpdateStuck} onStuck=${() => { stuck = true; }} /><//>`, document.getElementById("root"));

    // The tap. Where the old worker holds, the page has been round once
    // already: it came back from a reload over the grace and carries the note.
    if (old !== "takeover") sessionStorage.setItem(reloadKey, JSON.stringify({ why: "grace", at: Date.now() }));
    const tapAt = Date.now();
    sessionStorage.setItem(noteKey, JSON.stringify({ tapAt }));
    pwa.apply();
    if (old === "takeover") return null;

    const toast = await until(() => document.querySelector(".toast.on"), 25000);
    return {
        reloaded: false,
        stuck,
        afterTap: Date.now() - tapAt,
        toast: toast ? toast.querySelector(".tmain").textContent : "",
        sub: toast && toast.querySelector(".tsub") ? toast.querySelector(".tsub").textContent : "",
    };
}

async function afterReload(note) {
    note.reloads = (note.reloads || 0) + 1;
    sessionStorage.setItem(noteKey, JSON.stringify(note));
    const mark = JSON.parse(sessionStorage.getItem(reloadKey) || "null");
    return {
        reloaded: true,
        reloads: note.reloads,
        afterTap: Date.now() - note.tapAt,
        version: await pwa.runningVersion(),
        why: mark ? mark.why : "",
    };
}

const note = JSON.parse(sessionStorage.getItem(noteKey) || "null");
(note ? afterReload(note) : firstLoad()).then(
    (result) => { if (result) window.result = result; },
    (err) => { window.result = { error: String(err && err.stack || err) }; },
);
