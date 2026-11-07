// The existing contour fixture also exercises its registry and screenshots.
import { h, render } from "preact";
import { html } from "/src/html.js";
import { Sessions } from "/src/screens/sessions.js";
import { Nav } from "/src/ui/nav.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";
import { counted, quiet } from "/check/fixtures/quiet.js";

const json = (body) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
const text = (el) => el ? el.textContent.replace(/\s+/g, " ").trim() : "";
const until = async (predicate) => {
    const end = performance.now() + 4000;
    while (!predicate() && performance.now() < end) await new Promise(requestAnimationFrame);
    if (!predicate()) throw new Error("the contour fixture did not reach the expected state");
    await quiet();
};
const click = async (selector) => {
    const target = document.querySelector(selector);
    if (!target) throw new Error(`missing control: ${selector}`);
    target.click();
    await quiet();
};
const back = async (predicate) => { history.back(); await until(predicate); };
const sheet = (label) => document.querySelector(`.sheet.on[aria-label="${label}"]`);
const stamp = (days = 0) => { const now = new Date(); return new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate() - days)).toISOString(); };
const person = { name: "work-ser1063", known: true, live: true, status: "busy", conversations: ["11111111-old", "22222222-live"] };
const task = (number, title, more = {}) => ({ projectId: 7, project: "shop", contourId: 4, contour: "Acme", number, title,
    status: "open", priority: "MEDIUM", tags: [], externalRefs: [], externalUrl: "", people: [{ name: "quiet-worker", known: true, live: false, status: "", conversations: ["aaaa1111-old"] }],
    lastEventAt: stamp(10), updatedAt: stamp(10), lastActor: "aaaa1111", lastActorName: "quiet-worker", inWork: false, ...more });
const base = [
    task(1, "SER-1063: keep inventory current", { inWork: true, lastEventAt: stamp(), priority: "HIGH", externalRefs: ["SER-1063"], people: [person], lastActor: "22222222", lastActorName: "work-ser1063" }),
    task(2, "Review the quiet importer"),
    task(3, "Deferred API work", { projectId: 8, project: "api", status: "deferred" }),
    task(4, "SER-1063: Completed inventory check", { externalRefs: ["SER-1063"], status: "done", closedAt: stamp(), lastEventAt: stamp() }),
    task(5, "LMS-42: Completed API check", { externalRefs: ["LMS-42"], projectId: 8, project: "api", status: "done", closedAt: stamp(1), lastEventAt: stamp(1) }),
    task(6, "Cancelled experiment", { status: "cancelled", closedAt: stamp(3), lastEventAt: stamp(3) }),
];
let mode = "list";
const boardTasks = () => mode === "empty" ? [] : mode !== "long" ? base : [...base,
    ...Array.from({ length: 240 }, (_, i) => task(100 + i, `Queued task ${i}`)),
    ...Array.from({ length: 300 }, (_, i) => task(400 + i, `Archived task ${i}`, { status: "done", closedAt: stamp(35 + i), lastEventAt: stamp(35 + i) }))];
const eventsOf = (row, period = false) => [{ id: row.number * 10, kind: period && row.status === "done" ? "closed" : "changed",
    at: period ? stamp(1) : row.lastEventAt, precision: "date", actor: row.lastActor, actorName: row.lastActorName, origin: "cctm", payload: {} }];
const noTask = [{ external: "SER-1064", people: [{ ...person, name: "work-ser1064", conversations: ["33333333-live"] }] }];
const profiles = [{ id: 4, name: "Acme", profile: "Acme", configDir: "/srv/account", launch: {}, groups: [
    { id: 1, name: "Services", projects: [{ id: 7, name: "shop", session: "shop", path: "/srv/shop", launch: {} }, { id: 8, name: "api", session: "api", path: "/srv/api", launch: {} }] },
    { id: 2, name: "Personal", projects: [{ id: 6, name: "home", path: "/home/u", launch: {} }] },
] }, { id: 3, name: "personal", profile: "personal", configDir: "/srv/personal", launch: {}, groups: [] }];
const now = Math.floor(Date.now() / 1000);
const windowOf = (pct, hours) => ({ pct, resetsAt: now + hours * 3600 + 1800 });
const limits = { contours: [{ profile: "Acme", contour: 4, ageSec: 30, fiveHour: windowOf(99.5, 1), sevenDay: windowOf(88, 132),
    codex: { ageSec: 30, sevenDay: windowOf(44, 132) } }] };
const live = [{ session: "work-ser1063", sessionId: "22222222-live", cwd: "/srv/shop", project: { id: 7, name: "shop" }, contour: 4, status: "busy", pct: 12, transport: "stream" },
    { session: "home", sessionId: "home-id", cwd: "/home/u", project: { id: 6, name: "home" }, contour: 4, home: true, status: "idle", pct: 1, transport: "stream" }];
const snapshot = { at: now, sessions: live, profileMap: profiles, host: { home: "/home/u" }, limits };
const exec = { available: true, kinds: ["session.open", "session.send"] };
const wait = { opening: () => [], of: () => null };
const requests = [];
const sent = [];
let periodCalls = [];
const realFetch = window.fetch.bind(window);
window.fetch = counted(async (url, init = {}) => {
    const u = new URL(String(url), location.origin);
    requests.push(u.pathname);
    if (u.pathname === "/api/tasks/board") return json({ tasks: u.searchParams.get("contour") === "4" ? boardTasks() : [], noTask: mode === "empty" ? [] : noTask, notes: [] });
    const detail = u.pathname.match(/^\/api\/projects\/(\d+)\/tasks\/(\d+)$/);
    if (detail) {
        const row = boardTasks().find((t) => t.projectId === Number(detail[1]) && t.number === Number(detail[2]));
        return json({ task: { ...row, markdown: "## Description\n\nKeep **stock** current.\n\n## Verify\n\n- Read the logs." }, events: eventsOf(row), handoffs: [] });
    }
    if (u.pathname === "/api/tasks/period") {
        periodCalls.push({ from: u.searchParams.get("from"), until: u.searchParams.get("until"), offset: u.searchParams.get("offset") });
        const yesterday = new Date(u.searchParams.get("until")) <= new Date(stamp());
        // Two pages put work of one issue in different replies.
        const offset = Number(u.searchParams.get("offset"));
        const row = yesterday ? base[4] : offset ? base[3] : base[0];
        const groups = [{ external: yesterday ? "LMS-42" : "SER-1063", tasks: [{ task: row, events: eventsOf(row, true) }] }];
        return json({ from: u.searchParams.get("from"), until: u.searchParams.get("until"), groups, totalTasks: yesterday ? 1 : 501, limit: 500, offset });
    }
    if (u.pathname === "/api/profiles") return json({ profiles, accounts: [], disk: { dirs: [] }, models: {} });
    if (u.pathname === "/api/profiles/schema") return realFetch(url, init);
    if (u.pathname === "/api/profiles/journal") return json({ entries: [] });
    if (u.pathname === "/api/actions") { sent.push(JSON.parse(init.body)); return json({ ok: true, detail: "opened" }); }
    if (u.pathname === "/api/sessions/archive") return json({ rows: [], total: 0, limit: 20, offset: 0 });
    if (u.pathname.startsWith("/api/term")) return json({ available: false });
    return json({});
});
window.EventSource = class { constructor() { this.readyState = 1; } addEventListener() {} close() {} };

export async function contourScene(root, hash) {
    localStorage.removeItem("aacpanel.sessions.profile");
    const mount = (stale = false) => render(h(ToastHost, null, h(GateHost, null, html`
        <main class="body"><${Sessions} snapshot=${snapshot} error=${null} ageSec=${stale ? null : 1} exec=${exec} wait=${wait} faults=${[]} onLayer=${() => {}} /></main>
        <${Nav} current="sessions" onSelect=${() => {}} home=${null} />
    `)), root);
    mode = ["long", "empty"].includes(hash) ? hash : "list";
    mount();
    await until(() => document.querySelectorAll(".limit-ring").length === 3);
    if (hash) {
        if (hash !== "limits") {
            await click('.pfpage .pfaction[aria-label="Tasks"]');
            await until(() => document.querySelector(".tqscreen") && !text(document.querySelector(".tqscreen")).includes("Loading tasks"));
            if (hash === "task") await click(".tqrow");
            if (hash === "report") await click(".tqtile.report");
            await quiet();
        }
        return { screenshot: hash };
    }
    const out = {};
    out.rings = [...document.querySelectorAll(".limit-ring")].map((r) => ({ vendor: text(r.querySelector(".agentword")), window: text(r.querySelector(".limit-dial")), label: r.getAttribute("aria-label"), tone: r.className }));
    out.actions = [...document.querySelector(".pflimitrow").querySelectorAll(".pfaction")].map((b) => b.getAttribute("aria-label"));
    await click('.pfpage .pfaction[aria-label="New session"]');
    await until(() => sheet("New session"));
    out.projects = [...sheet("New session").querySelectorAll(".newproject")].map(text);
    await click('.sheet.on[aria-label="New session"] .newproject');
    await until(() => sheet("action confirmation"));
    out.beforeConfirm = sent.length;
    await click('.sheet.on[aria-label="action confirmation"] .btn.primary');
    await until(() => sent.length === 1 && !sheet("action confirmation"));
    out.sent = sent;
    await click('.pfpage .pfaction[aria-label="Profile"]');
    await until(() => document.querySelector(".phead h2")?.textContent === "Acme" && document.querySelector(".pzwhere .pzcard"));
    out.profile = requests.includes("/api/profiles");
    await click(".pback");
    await until(() => document.querySelector(".pflimitrow"));
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector(".tqrow"));
    out.headings = [...document.querySelectorAll(".tqscreen .pjsection")].map(text);
    out.unfiled = text(document.querySelector(".tqnotasks"));
    out.quietBefore = document.querySelector('[aria-label="Quiet"] .tqrow') === null;
    await click('[aria-label="Quiet"] .tqpline');
    out.quietAfter = text(document.querySelector('[aria-label="Quiet"] .tqrow'));
    await click(".tqtiles .tqtile");
    out.closedToday = text(document.querySelector(".tqtiles + .tqfoldgroup"));
    await click(".tqtiles .tqtile");
    await click(".tqgroup .tqrow");
    await until(() => sheet("Task")?.querySelector(".tqbody b"));
    out.task = { body: text(sheet("Task").querySelector(".tqbody")), people: text(sheet("Task").querySelector(".tqpeople")), events: text(sheet("Task").querySelector(".tqevents")) };
    await back(() => !sheet("Task"));
    out.taskBack = Boolean(document.querySelector(".tqscreen"));
    await click(".tqtile.report");
    await until(() => sheet("Report")?.querySelector(".tqrline"));
    await click('.sheet.on[aria-label="Report"] .tqrline');
    out.report = text(sheet("Report").querySelector(".tqactivity"));
    out.reportRows = sheet("Report").querySelectorAll(".tqactivity").length;
    out.reportPages = periodCalls.length;
    const yesterday = [...sheet("Report").querySelectorAll(".tqperiods button")].find((b) => text(b) === "Yesterday");
    yesterday.click();
    await until(() => text(sheet("Report")).includes("LMS-42"));
    out.yesterday = text(sheet("Report"));
    const week = [...sheet("Report").querySelectorAll(".tqperiods button")].find((b) => text(b) === "Week");
    week.click();
    await until(() => text(sheet("Report")).includes("SER-1063") && text(sheet("Report").querySelector('.tqperiods [aria-pressed="true"]')) === "Week");
    out.week = text(sheet("Report"));
    out.periodCalls = periodCalls;
    await back(() => !sheet("Report"));
    await back(() => !document.querySelector(".tqscreen"));
    out.screenBack = Boolean(document.querySelector(".pflimitrow"));
    mode = "long";
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector('[aria-label="Quiet"] .tqpline'));
    out.longCollapsed = document.querySelectorAll(".tqrow").length;
    await click('[aria-label="Quiet"] .tqpline');
    out.longFirst = document.querySelector('[aria-label="Quiet"]').querySelectorAll(".tqrow").length;
    await click('[aria-label="Quiet"] .tqmore');
    out.longMore = document.querySelector('[aria-label="Quiet"]').querySelectorAll(".tqrow").length;
    const search = document.querySelector('.tqsearch input');
    search.value = "Queued task 239";
    search.dispatchEvent(new Event("input", { bubbles: true }));
    await quiet();
    out.searched = text(document.querySelector('[aria-label="Quiet"]')).includes("Queued task 239") && !text(document.querySelector('[aria-label="Quiet"]')).includes("Queued task 0");
    search.value = "";
    search.dispatchEvent(new Event("input", { bubbles: true }));
    await quiet();
    await back(() => !document.querySelector(".tqscreen"));
    mode = "empty";
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector(".tqempty"));
    out.empty = text(document.querySelector(".tqempty"));
    await back(() => !document.querySelector(".tqscreen"));
    out.emptyBack = Boolean(document.querySelector(".pflimitrow"));
    mount(true);
    await quiet();
    out.staleRings = document.querySelectorAll(".limit-ring.old").length;
    out.staleNote = text(document.querySelector(".limits-note"));
    return out;
}
