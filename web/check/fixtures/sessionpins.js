// Pin scenarios share the session groups page and its browser. Both screens
// take the same snapshots, as two devices do after the collector reads a pin.
import { render } from "preact";
import { html } from "/src/html.js";
import { SessionColumn, orderOf } from "/src/desktop/sessions.js";
import { blocksOf, ProjectBlock } from "/src/screens/sessions/blocks.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";
import { MenuHost } from "/src/desktop/menu.js";
import { counted, quiet } from "/check/fixtures/quiet.js";

export async function sessionPins() {
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
    const sent = [];
    const exec = { available: true, kinds: ["session.pin"] };
    const fetch = window.fetch;
    const json = (body) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
    window.fetch = counted(async (url, init) => {
        if (String(url) === "/api/actions") {
            const req = JSON.parse(init.body);
            sent.push(req);
            if (req.kind !== "session.pin") throw new Error(`unexpected action ${req.kind}`);
            sequence++;
            sessions = sessions.map((s) => s.session === req.target
                ? { ...s, pinnedAt: req.params.on ? `2026-01-01T00:00:00.00000000${sequence}Z` : undefined } : s);
            return json({ ok: true, logged: true });
        }
        if (String(url).startsWith("/api/sessions/archive")) return json({ rows: [], total: 0 });
        return fetch(url, init);
    });
    const until = async (check) => {
        const end = performance.now() + 3000;
        while (!check()) {
            if (performance.now() >= end) throw new Error("the pin did not reach the screen");
            await new Promise((resolve) => setTimeout(resolve, 20));
        }
    };
    const text = (el) => el.textContent.trim();
    const names = (selector) => [...mount.querySelectorAll(selector)].map(text);
    const paint = async () => {
        render(html`<${ToastHost}><${GateHost}><${MenuHost}>
            <div class="deskshell"><div class="dkbody">
                <${SessionColumn} snapshot=${{ at: sequence + 1, sessions, profileMap }} limits=${null}
                    current="" onPick=${() => {}} picks=${[]} setPicks=${() => {}} exec=${exec}
                    onOrder=${(list) => { order = list.map((s) => s.name); }} />
            </div></div>
            <div class="phone">
                ${blocksOf({ profile: profileMap[0], sessions: sessions.filter((s) => s.contour === 8) })
                    .map((block) => html`<${ProjectBlock} key=${block.key} block=${block} live=${sessions}
                                                       exec=${exec} onOpen=${() => {}} />`)}
            </div>
        <//><//><//>`, mount);
        await quiet();
    };
    const read = () => ({
        phone: names(".phone .pjsess"),
        desk: names(".dklive .dkname"),
        keys: order.slice(),
        ordered: orderOf(sessions, profileMap).map((s) => s.session),
    });
    const phonePin = async (name) => {
        const card = [...mount.querySelectorAll(".phone .pjcard")].find((c) => text(c.querySelector(".pjsess")) === name);
        const button = card.querySelector(".pjact[aria-pressed]");
        if (!button || button.disabled) throw new Error(`no pin button for ${name}`);
        const before = sent.length;
        button.click();
        await until(() => sent.length > before);
        await quiet();
        await paint();
    };
    const menuPin = async (name, label) => {
        const row = [...mount.querySelectorAll(".dklive")].find((r) => text(r.querySelector(".dkname")) === name);
        row.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 100, clientY: 100 }));
        await until(() => document.querySelector(".pkrow"));
        const item = [...document.querySelectorAll(".pkrow")].find((r) => text(r.querySelector(".pkname")) === label);
        if (!item || item.getAttribute("aria-disabled") === "true") throw new Error(`no menu item ${label}`);
        const before = sent.length;
        item.click();
        await until(() => sent.length > before);
        await quiet();
        await paint();
    };
    try {
        await paint();
        const out = { before: read() };
        await phonePin("thread");
        out.pinned = read();
        out.pressed = [...mount.querySelectorAll('.phone .pjact[aria-pressed="true"]')]
            .map((b) => b.getAttribute("aria-label"));
        await menuPin("audit", "Pin the group");
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
        await menuPin("child", "Unpin the group of review");
        out.unpinned = read();
        await phonePin("audit");
        sessions = sessions.map((s) => ({ ...s, status: base.find((b) => b.session === s.session).status }));
        await paint();
        out.restored = read();
        out.sent = sent.map((r) => `${r.kind}:${r.target}:${JSON.stringify(r.params)}`);
        out.confirmation = Boolean(document.querySelector(".sheet.on"));
        return out;
    } finally {
        render(null, mount);
        mount.remove();
        window.fetch = fetch;
    }
}
