// Pin scenarios share the session groups page and its browser. Both screens
// take the same snapshots, as two devices do after the collector reads a pin.
// The pin of a card is its button at a desk and a sector of its pie on the
// phone.
import { render } from "preact";
import { html } from "/src/html.js";
import { SessionColumn, orderOf } from "/src/desktop/sessions.js";
import { Sessions } from "/src/screens/sessions.js";
import { CATCH_UP_LIMIT, CATCH_UP_MS, useCatchUp } from "/src/catchup.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";
import { MenuHost } from "/src/desktop/menu.js";
import { counted, quiet } from "/check/fixtures/quiet.js";
import { closePie, item, openPie, sector, settled, underPies } from "/check/fixtures/pie.js";

export async function sessionPins(phoneOnly = false) {
    const mount = document.createElement("div");
    document.body.append(mount);
    const project = (id, name) => ({ id, name, session: name, path: `/srv/${name}` });
    const alpha = project(81, "alpha"), beta = project(82, "beta"), gamma = project(83, "gamma");
    const profileMap = [
        { id: 8, profile: "First", groups: [{ name: "work", projects: [alpha, beta] }] },
        { id: 9, profile: "Second", groups: [{ name: "work", projects: [gamma] }] },
    ];
    const row = (p, name, extra = {}) => ({
        session: name, sessionId: `id-${name}`, project: p, cwd: p.path, profile: p === gamma ? "Second" : "First",
        contour: p === gamma ? 9 : 8, transport: "stream", status: "idle", pct: 10,
        startedAt: "2026-01-01T00:00:00Z", ...extra,
    });
    const base = [
        row(alpha, "alpha", { status: "busy" }), row(beta, "beta"),
        row(beta, "review"), row(beta, "child", { parent: { session: "review" } }),
        row(beta, "thread", { agent: "codex", title: "thread", parent: { session: "child" } }),
        row(beta, "audit"), row(gamma, "gamma"),
    ];
    let sessions = base.map((s) => ({ ...s }));
    let order = [];
    let sequence = 0;
    let snapshot;
    let wait;
    let reply;
    let poll;
    let refreshed = 0;
    const sent = [];
    const exec = { available: true, kinds: ["session.pin"] };
    const fetch = window.fetch;
    const interval = window.setInterval;
    const now = Date.now;
    window.setInterval = (fn, ms, ...args) => {
        if (ms === CATCH_UP_MS) poll = fn;
        return interval(fn, ms, ...args);
    };
    const json = (body) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
    window.fetch = counted(async (url, init) => {
        if (String(url) === "/api/actions") {
            const req = JSON.parse(init.body);
            sent.push(req);
            if (req.kind !== "session.pin") throw new Error(`unexpected action ${req.kind}`);
            sequence++;
            return new Promise((resolve) => { reply = resolve; });
        }
        if (String(url).startsWith("/api/sessions/archive")) return json({ rows: [], total: 0 });
        return fetch(url, init);
    });
    const until = async (check) => {
        const end = performance.now() + 3000;
        while (!check()) {
            if (performance.now() >= end) throw new Error("the pin did not reach the screen");
            await new Promise(requestAnimationFrame);
        }
    };
    const text = (el) => el.textContent.trim();
    const refresh = () => { refreshed++; };
    function Lists({ snapshot }) {
        wait = useCatchUp(snapshot, refresh);
        return html`<${ToastHost}><${GateHost}><${MenuHost}>
            <div class="deskshell"><div class="dkbody">
                <${SessionColumn} snapshot=${snapshot} limits=${null} wait=${wait}
                    current="" onPick=${() => {}} picks=${[]} setPicks=${() => {}} exec=${exec}
                    onOrder=${(list) => { order = list.map((s) => s.name); }} />
            </div></div>
            <div class="phone">
                <${Sessions} snapshot=${{ ...snapshot, profileMap: [profileMap[0]],
                    sessions: snapshot.sessions.filter((s) => s.contour === 8) }} ageSec=${0} exec=${exec} wait=${wait} />
            </div>
        <//><//><//>`;
    }
    const paint = async () => {
        // Every snapshot comes within one second, as woken ones do: only the
        // milliseconds tell a later one from the one at the press.
        snapshot = { at: 1, atMs: (snapshot ? snapshot.atMs : 1000) + 1, sessions, profileMap };
        render(html`<${Lists} snapshot=${snapshot} />`, mount);
        await quiet();
    };
    const cards = (selector) => [...mount.querySelectorAll(selector)].map((c) => c.dataset.name);
    const read = () => ({
        phone: cards(".phone .sc:not(.scpast)"),
        desk: cards(".dkbody .sc:not(.scpast)"),
        keys: order.slice(),
        ordered: orderOf(snapshot.sessions, profileMap, wait).map((s) => s.session),
    });
    // phonePinOf opens the pie of a phone card and gives its pin.
    const phonePinOf = async (name) => {
        await settled();
        await openPie(mount.querySelector(`.phone .sc[data-name="${name}"]`));
        return sector("pin");
    };
    const deskRow = (name) => mount.querySelector(`.dkbody .sc[data-name="${name}"]`);
    const look = async (name) => {
        const pin = item(await phonePinOf(name));
        await closePie();
        const mark = deskRow(name).querySelector(".scact[aria-pressed]");
        return {
            pressed: pin.on,
            pending: /^(Pinning|Unpinning) the group$/.test(pin.why),
            off: pin.off,
            mark: Boolean(mark),
            markOn: mark && mark.getAttribute("aria-pressed") === "true",
            markPending: mark && mark.getAttribute("aria-busy") === "true",
        };
    };
    const phonePin = async (name) => {
        const pin = await phonePinOf(name);
        if (!pin || pin.getAttribute("aria-disabled") === "true") throw new Error(`no pin in the pie of ${name}`);
        const before = sent.length;
        pin.click();
        await until(() => sent.length > before && wait.of("pin", sent.at(-1).target));
        await settled();
        await until(() => order.join("\n") === cards(".dkbody .sc:not(.scpast)").join("\n"));
    };
    const deskPin = async (name) => {
        const button = deskRow(name).querySelector(".scact[aria-pressed]");
        if (!button || button.getAttribute("aria-disabled") === "true") throw new Error(`no pin button for ${name}`);
        const before = sent.length;
        button.click();
        await until(() => sent.length > before && wait.of("pin", sent.at(-1).target));
        // The group moves to the pinned place at once: its card is drawn
        // there anew.
        const pending = () => deskRow(name).querySelector(".scact[aria-pressed]");
        await until(() => pending().getAttribute("aria-busy") === "true");
        pending().click();
        if (sent.length !== before + 1) throw new Error("a pending pin was sent twice");
        await until(() => order.join("\n") === cards(".dkbody .sc:not(.scpast)").join("\n"));
    };
    const menuItem = async (name, label) => {
        const row = deskRow(name);
        row.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 100, clientY: 100 }));
        await until(() => mount.querySelector(".pkrow"));
        const entry = [...mount.querySelectorAll(".pkrow")].find((r) => text(r.querySelector(".pkname")) === label);
        if (!entry) throw new Error(`no menu item ${label}`);
        return entry;
    };
    const menuPin = async (name, label) => {
        const entry = await menuItem(name, label);
        if (!entry || entry.getAttribute("aria-disabled") === "true") throw new Error(`no menu item ${label}`);
        const before = sent.length;
        entry.click();
        await until(() => sent.length > before && wait.of("pin", sent.at(-1).target));
        await until(() => order.join("\n") === cards(".dkbody .sc:not(.scpast)").join("\n"));
    };
    const pendingMenu = async (name, label) => {
        const entry = await menuItem(name, label);
        const out = { label: text(entry.querySelector(".pkname")), off: entry.getAttribute("aria-disabled") === "true",
            note: text(entry.querySelector(".pkdesc")) };
        document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
        await until(() => !mount.querySelector(".pkrow"));
        return out;
    };
    const answer = async (ok) => {
        reply(ok ? json({ ok: true, logged: true }) : new Response(JSON.stringify({ error: "the pin could not be saved" }), { status: 403 }));
        reply = null;
        await quiet();
    };
    const confirm = async () => {
        const req = sent.at(-1);
        sessions = sessions.map((s) => s.session === req.target
            ? { ...s, pinnedAt: req.params.on ? `2026-01-01T00:00:00.00000000${sequence}Z` : undefined } : s);
        await paint();
        await until(() => !wait.of("pin", req.target));
    };
    try {
        await paint();
        underPies();
        const out = { before: read() };
        await phonePin("thread");
        out.pinning = read();
        out.pinLook = await look("review");
        if (!phoneOnly) out.pinMenu = await pendingMenu("child", "Unpin the group of review");
        await answer(true);
        out.answered = read();
        out.answerPending = (await look("review")).pending;
        // A fresh snapshot can still precede the collector's session pass.
        await paint();
        out.lagging = (await look("review")).pending;
        await confirm();
        out.pinned = read();
        out.confirmedLook = await look("review");
        out.pressed = [];
        for (const name of cards(".phone .sc:not(.scpast)")) {
            const pin = item(await phonePinOf(name));
            await closePie();
            if (pin.on) out.pressed.push(name);
        }
        if (phoneOnly) await phonePin("audit");
        else await menuPin("audit", "Pin the group");
        out.secondPinning = read();
        await answer(true);
        await confirm();
        // An earlier pin in another contour never moves that contour.
        sessions = sessions.map((s) => s.session === "gamma"
            ? { ...s, pinnedAt: "2025-01-01T00:00:00.000000000Z" } : s);
        await paint();
        out.multiple = read();
        // A new process and conversation under the same name carry the saved
        // pin, and a change of status cannot move either pinned group.
        sessions = sessions.map((s) => s.session === "review"
            ? { ...s, sessionId: "reopened-review", status: "waiting" } : s);
        await paint();
        out.reopened = read();
        if (phoneOnly) await phonePin("child");
        else await deskPin("child");
        out.unpinning = read();
        out.unpinLook = await look("review");
        if (!phoneOnly) out.unpinMenu = await pendingMenu("thread", "Pin the group of review");
        await answer(true);
        await confirm();
        out.unpinned = read();
        out.unpinnedLook = await look("review");
        await phonePin("audit");
        await answer(true);
        await confirm();
        sessions = sessions.map((s) => ({ ...s, status: base.find((b) => b.session === s.session).status }));
        await paint();
        out.restored = read();
        out.sent = sent.map((r) => `${r.kind}:${r.target}:${JSON.stringify(r.params)}`);
        out.confirmation = Boolean(document.querySelector(".sheet.on"));
        out.refused = [];
        for (const press of [() => phonePin("thread"), () => phoneOnly ? phonePin("audit") : menuPin("audit", "Pin the group")]) {
            await press();
            const during = read();
            await answer(false);
            await until(() => !wait.busy && mount.querySelector(".toast.on.bad"));
            const toast = mount.querySelector(".toast.on.bad");
            out.refused.push({ during, after: read(), pending: Boolean(mount.querySelector('[aria-busy="true"]')),
                reason: text(toast.querySelector(".tsub")) });
        }
        sessions = sessions.map((s) => s.session === "audit" ? { ...s, pinnedAt: "2026-01-01T00:00:00Z" } : s);
        await paint();
        out.refusedUnpin = { before: read() };
        if (phoneOnly) await phonePin("audit");
        else await menuPin("audit", "Unpin the group");
        out.refusedUnpin.during = read();
        await answer(false);
        out.refusedUnpin.after = read();
        sessions = sessions.map((s) => ({ ...s, pinnedAt: undefined }));
        await paint();
        await phonePin("audit");
        await answer(true);
        const asked = refreshed;
        Date.now = () => now() + CATCH_UP_LIMIT + CATCH_UP_MS;
        poll();
        await until(() => !wait.busy);
        await quiet();
        out.expired = read();
        out.polled = refreshed > asked;
        out.expiredLook = await look("audit");

        // Working and idle share one section in map order, even when the
        // snapshot arrives in a different order and every activity changes.
        const resting = project(84, "quiet"), asking = project(85, "asking");
        profileMap[0].groups[0].projects = [resting, beta, asking, alpha];
        sessions = [
            row(alpha, "alpha", { status: "busy" }),
            row(beta, "audit", { status: "busy", pinnedAt: "2026-01-01T00:00:02Z" }),
            row(beta, "review", { pinnedAt: "2026-01-01T00:00:01Z" }),
            row(asking, "asking", { status: "waiting" }),
            row(beta, "beta"), row(resting, "quiet"),
            row(resting, "z-helper", { lastRequestAt: "2026-01-01T00:02:00Z" }),
            row(resting, "a-helper", { lastRequestAt: "2026-01-01T00:01:00Z" }),
            row(alpha, "outside-z", { project: null, cwd: "/srv/outside-z" }),
            row(alpha, "outside-a", { project: null, cwd: "/srv/outside-a" }),
        ];
        const sections = () => [...mount.querySelectorAll(".phone .pjsection")].map((head) => {
            const rows = [];
            for (let block = head.nextElementSibling; block && block.classList.contains("pjblock"); block = block.nextElementSibling) {
                rows.push(...[...block.querySelectorAll(".sc:not(.scpast)")].map((card) => card.dataset.name));
            }
            return { title: head.firstChild.textContent.trim(), rows };
        });
        const states = () => Object.fromEntries([...mount.querySelectorAll(".phone .sc:not(.scpast)")]
            .map((card) => [card.dataset.name, text(card.querySelector(".scsay"))]));
        await paint();
        out.sections = { before: sections(), statesBefore: states(), deskBefore: read().desk };
        sessions = sessions.map((s) => ({ ...s,
            status: s.session === "review" || s.session === "asking" ? "waiting" : s.session === "quiet" ? "busy" : "idle",
            lastRequestAt: s.session === "a-helper" ? "2026-01-01T00:03:00Z" : s.lastRequestAt,
        })).reverse();
        await paint();
        await until(() => states().quiet === "working" && states().alpha === "idle");
        out.sections.changed = sections();
        out.sections.statesChanged = states();
        out.sections.deskChanged = read().desk;
        sessions = sessions.map((s) => s.session === "asking" ? { ...s, status: "idle" } : s);
        await paint();
        await until(() => states().asking === "idle");
        out.sections.answered = sections();
        return out;
    } finally {
        if (reply) reply(new Response("", { status: 503 }));
        Date.now = now;
        render(null, mount);
        mount.remove();
        window.fetch = fetch;
        window.setInterval = interval;
    }
}
