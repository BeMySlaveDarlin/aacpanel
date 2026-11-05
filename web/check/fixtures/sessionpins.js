// Pin scenarios share the session groups page and its browser. Both screens
// take the same snapshots, as two devices do after the collector reads a pin.
import { render } from "preact";
import { html } from "/src/html.js";
import { SessionColumn, orderOf } from "/src/desktop/sessions.js";
import { Sessions } from "/src/screens/sessions.js";
import { CATCH_UP_LIMIT, CATCH_UP_MS, useCatchUp } from "/src/catchup.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";
import { MenuHost } from "/src/desktop/menu.js";
import { counted, quiet } from "/check/fixtures/quiet.js";

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
    const names = (selector) => [...mount.querySelectorAll(selector)].map(text);
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
    const read = () => ({
        phone: names(".phone .pjsess"),
        desk: names(".dklive .dkname"),
        keys: order.slice(),
        ordered: orderOf(snapshot.sessions, profileMap, wait).map((s) => s.session),
    });
    const phoneButton = (name) => [...mount.querySelectorAll(".phone .pjcard")]
        .find((c) => text(c.querySelector(".pjsess")) === name).querySelector(".pjact[aria-pressed]");
    const deskRow = (name) => [...mount.querySelectorAll(".dklive")].find((r) => text(r.querySelector(".dkname")) === name);
    const look = (name) => {
        const button = phoneButton(name);
        const mark = deskRow(name).querySelector(".dkact[aria-pressed]");
        const accent = document.createElement("span");
        accent.style.color = "var(--accent)";
        mark.append(accent);
        const accentColor = getComputedStyle(accent).color;
        accent.remove();
        return {
            pressed: button.getAttribute("aria-pressed") === "true",
            pending: button.getAttribute("aria-busy") === "true",
            off: button.disabled,
            dim: Number(getComputedStyle(button).opacity) < 1,
            mark: Boolean(mark),
            markOn: mark && mark.getAttribute("aria-pressed") === "true",
            markPending: mark && mark.getAttribute("aria-busy") === "true",
            markDim: mark && Number(getComputedStyle(mark).opacity) < 1,
            markOpacity: Number(getComputedStyle(mark).opacity),
            markColor: getComputedStyle(mark).color,
            markFill: getComputedStyle(mark.querySelector("svg")).fill,
            accentColor,
        };
    };
    const phonePin = async (name) => {
        const button = phoneButton(name);
        if (!button || button.disabled) throw new Error(`no pin button for ${name}`);
        const before = sent.length;
        button.click();
        await until(() => sent.length > before && wait.of("pin", sent.at(-1).target));
        await until(() => phoneButton(name).getAttribute("aria-busy") === "true");
        await until(() => order.join("\n") === names(".dklive .dkname").join("\n"));
    };
    const deskPin = async (name) => {
        const button = deskRow(name).querySelector(".dkact[aria-pressed]");
        if (!button || button.getAttribute("aria-disabled") === "true") throw new Error(`no pin button for ${name}`);
        const before = sent.length;
        button.click();
        await until(() => sent.length > before && wait.of("pin", sent.at(-1).target));
        await until(() => button.getAttribute("aria-busy") === "true");
        button.click();
        if (sent.length !== before + 1) throw new Error("a pending pin was sent twice");
        await until(() => order.join("\n") === names(".dklive .dkname").join("\n"));
    };
    const menuItem = async (name, label) => {
        const row = deskRow(name);
        row.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 100, clientY: 100 }));
        await until(() => mount.querySelector(".pkrow"));
        const item = [...mount.querySelectorAll(".pkrow")].find((r) => text(r.querySelector(".pkname")) === label);
        if (!item) throw new Error(`no menu item ${label}`);
        return item;
    };
    const menuPin = async (name, label) => {
        const item = await menuItem(name, label);
        if (!item || item.getAttribute("aria-disabled") === "true") throw new Error(`no menu item ${label}`);
        const before = sent.length;
        item.click();
        await until(() => sent.length > before && wait.of("pin", sent.at(-1).target));
        await until(() => phoneButton(name).getAttribute("aria-busy") === "true");
        await until(() => order.join("\n") === names(".dklive .dkname").join("\n"));
    };
    const pendingMenu = async (name, label) => {
        const item = await menuItem(name, label);
        const out = { label: text(item.querySelector(".pkname")), off: item.getAttribute("aria-disabled") === "true",
            note: text(item.querySelector(".pkdesc")) };
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
        const out = { before: read() };
        await phonePin("thread");
        out.pinning = read();
        out.pinLook = look("review");
        if (!phoneOnly) out.pinMenu = await pendingMenu("child", "Unpin the group of review");
        await answer(true);
        out.answered = read();
        out.answerPending = look("review").pending;
        // A fresh snapshot can still precede the collector's session pass.
        await paint();
        out.lagging = look("review").pending;
        await confirm();
        out.pinned = read();
        out.confirmedLook = look("review");
        out.pressed = [...mount.querySelectorAll('.phone .pjact[aria-pressed="true"]')]
            .map((b) => b.getAttribute("aria-label"));
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
        out.unpinLook = look("review");
        if (!phoneOnly) out.unpinMenu = await pendingMenu("thread", "Pin the group of review");
        await answer(true);
        await confirm();
        out.unpinned = read();
        out.unpinnedLook = look("review");
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
            const box = toast.getBoundingClientRect();
            out.refused.push({ during, after: read(), pending: Boolean(mount.querySelector('[aria-busy="true"]')),
                reason: text(toast.querySelector(".tsub")), visible: box.width > 0 && box.height > 0
                    && box.top >= 0 && box.bottom <= innerHeight && box.left >= 0 && box.right <= innerWidth });
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
        out.expiredLook = look("audit");

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
                rows.push(...[...block.querySelectorAll(".pjsess")].map(text));
            }
            return { title: head.firstChild.textContent.trim(), rows };
        });
        const states = () => Object.fromEntries([...mount.querySelectorAll(".phone .pjcard")]
            .map((card) => [text(card.querySelector(".pjsess")), text(card.querySelector(".pjtext"))]));
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
