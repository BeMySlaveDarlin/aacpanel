// The existing contour fixture also exercises its registry and screenshots.
import { h, render } from "preact";
import { html } from "/src/html.js";
import { Sessions } from "/src/screens/sessions.js";
import { ProfileLimits } from "/src/screens/sessions/limits.js";
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
const pickTaskTab = (id) => click(`.tqscreen [role="tab"][id="tq-4-${id}"]`);
const findTask = async (value) => {
    const input = document.querySelector(".tqsearch input");
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await quiet();
};
const taskBoard = () => {
    const selected = [...document.querySelectorAll('.tqscreen [role="tab"][aria-selected="true"]')];
    const panels = [...document.querySelectorAll('.tqscreen [role="tabpanel"]')];
    return {
        tabs: [...document.querySelectorAll('.tqscreen [role="tablist"] [role="tab"]')].map(text),
        selected: selected.map(text),
        headings: [...document.querySelectorAll(".tqscreen .pjsection")].map(text),
        rows: [...document.querySelectorAll(".tqscreen .tqrow")].map(text),
        folds: [...document.querySelectorAll(".tqscreen .tqpname")].map(text),
        unfiled: text(document.querySelector(".tqnotasks")),
        hint: text(document.querySelector(".tqscreen .tqhintline")),
        periods: document.querySelectorAll(".tqscreen .tqtile").length,
        panels: panels.length,
        linked: selected.length === 1 && panels.length === 1 && selected[0].getAttribute("aria-controls") === panels[0].id
            && panels[0].getAttribute("aria-labelledby") === selected[0].id,
    };
};
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
    if (u.pathname === "/api/tasks/board") return json({ tasks: u.searchParams.get("contour") === "4" ? boardTasks() : [], noTask: ["empty", "filed"].includes(mode) ? [] : noTask, notes: [] });
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
        if (hash.startsWith("limits")) {
            if (hash === "limits-stale" || hash === "limits-stale-tucked") { mount(true); await quiet(); }
            if (hash === "limits-tucked" || hash === "limits-stale-tucked") await tuckToolbar();
            if (hash === "limits-claude" || hash === "limits-codex") await vendorScene(root, hash.slice(7), false);
        } else {
            await click('.pfpage .pfaction[aria-label="Tasks"]');
            await until(() => document.querySelector(".tqscreen") && !text(document.querySelector(".tqscreen")).includes("Loading tasks"));
            if (hash === "task") await click(".tqrow");
            if (hash === "report") { await pickTaskTab("closed"); await click(".tqtile.report"); }
            await quiet();
        }
        return { screenshot: hash };
    }
    const out = {};
    out.rings = [...document.querySelectorAll(".limit-ring")].map((r) => ({ vendor: r.dataset.agent, window: text(r.querySelector(".limit-window")), percent: text(r.querySelector(".limit-percent")), label: r.getAttribute("aria-label"), tone: r.className }));
    out.actions = [...document.querySelector(".pflimitrow").querySelectorAll(".pfaction")].map((b) => b.getAttribute("aria-label"));
    out.full = readToolbar();
    out.details = await limitDetailsScene(root, mount);
    out.folding = await scrollScene(root);
    mount();
    await until(() => document.querySelectorAll(".limit-ring").length === 3);
    await click('.pfpage .pfaction[aria-label="New session"]');
    await until(() => sheet("New session"));
    out.projects = [...sheet("New session").querySelectorAll(".newproject")].map(text);
    await click('.sheet.on[aria-label="New session"] .newproject');
    await until(() => sheet("action confirmation"));
    out.beforeConfirm = sent.length;
    await click('.sheet.on[aria-label="action confirmation"] .btn.primary');
    await until(() => sent.length === 1 && !sheet("action confirmation"));
    out.sent = [...sent];
    await click('.pfpage .pfaction[aria-label="Profile"]');
    await until(() => document.querySelector(".phead h2")?.textContent === "Acme" && document.querySelector(".pzwhere .pzcard"));
    out.profile = requests.includes("/api/profiles");
    await click(".pback");
    await until(() => document.querySelector(".pflimitrow"));
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector(".tqrow"));
    out.tabs = { initial: taskBoard() };
    await pickTaskTab("unfiled");
    out.tabs.unfiled = taskBoard();
    out.unfiled = text(document.querySelector(".tqnotasks"));
    await pickTaskTab("quiet");
    out.tabs.quiet = taskBoard();
    out.quietBefore = document.querySelector('[aria-label="Quiet"] .tqrow') === null;
    await click('[aria-label="Quiet"] .tqpline');
    out.quietAfter = text(document.querySelector('[aria-label="Quiet"] .tqrow'));
    await findTask("SER-1063");
    out.tabs.search = taskBoard();
    await pickTaskTab("closed");
    out.tabs.searchClosed = taskBoard();
    await pickTaskTab("unfiled");
    out.tabs.emptyUnfiled = taskBoard();
    await findTask("work-ser1064");
    out.tabs.searchUnfiled = taskBoard();
    await findTask("nothing-matches");
    out.tabs.empty = taskBoard();
    await findTask("");
    await pickTaskTab("deferred");
    out.tabs.deferred = taskBoard();
    await click('[aria-label="Deferred"] .tqpline');
    out.tabs.openDeferred = taskBoard();
    await pickTaskTab("closed");
    out.tabs.closed = taskBoard();
    await click(".tqtiles .tqtile");
    out.closedToday = text(document.querySelector(".tqtiles + .tqfoldgroup"));
    await click(".tqtiles .tqtile");
    await click(".tqtile.report");
    await until(() => sheet("Report")?.querySelector(".tqrline"));
    await click('.sheet.on[aria-label="Report"] .tqrline');
    out.report = text(sheet("Report").querySelector(".tqactivity"));
    out.reportRows = sheet("Report").querySelectorAll(".tqactivity").length;
    out.reportPages = periodCalls.length;
    await click('.sheet.on[aria-label="Report"] .tqactivity .tqrow');
    await until(() => sheet("Task")?.querySelector(".tqbody b"));
    out.task = { body: text(sheet("Task").querySelector(".tqbody")), people: text(sheet("Task").querySelector(".tqpeople")), events: text(sheet("Task").querySelector(".tqevents")) };
    await back(() => !sheet("Task"));
    out.taskBack = Boolean(document.querySelector(".tqscreen")) && Boolean(sheet("Report"));
    out.tabs.taskBack = taskBoard();
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
    out.tabs.reportBack = taskBoard();
    await back(() => !document.querySelector(".tqscreen"));
    out.screenBack = Boolean(document.querySelector(".pflimitrow"));
    mode = "long";
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector(".tqrow"));
    out.tabs.reopened = taskBoard();
    out.longCollapsed = document.querySelectorAll(".tqrow").length;
    await pickTaskTab("quiet");
    await click('[aria-label="Quiet"] .tqpline');
    out.longFirst = document.querySelector('[aria-label="Quiet"]').querySelectorAll(".tqrow").length;
    await click('[aria-label="Quiet"] .tqmore');
    out.longMore = document.querySelector('[aria-label="Quiet"]').querySelectorAll(".tqrow").length;
    await findTask("Queued task 239");
    out.searched = text(document.querySelector('[aria-label="Quiet"]')).includes("Queued task 239") && !text(document.querySelector('[aria-label="Quiet"]')).includes("Queued task 0");
    await findTask("");
    await back(() => !document.querySelector(".tqscreen"));
    mode = "filed";
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector(".tqrow"));
    out.tabs.withoutUnfiled = taskBoard();
    await back(() => !document.querySelector(".tqscreen"));
    mode = "empty";
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector(".tqempty"));
    out.empty = text(document.querySelector(".tqempty"));
    await back(() => !document.querySelector(".tqscreen"));
    out.emptyBack = Boolean(document.querySelector(".pflimitrow"));
    mount(true);
    await quiet();
    await moveToolbar(0);
    out.staleRings = document.querySelectorAll(".limit-ring.old").length;
    out.staleNote = text(document.querySelector(".limits-note"));
    out.staleFull = readToolbar();
    await tuckToolbar();
    out.staleFold = readToolbar();
    await foldedActions(out, mount);
    out.singleVendor = [];
    for (const vendor of ["claude", "codex"]) out.singleVendor.push(await vendorScene(root, vendor));
    return out;
}


const pageOfRow = () => document.querySelector(".pfpage") || document.querySelector(".body");
const rowBox = () => pageOfRow()?.querySelector(".pflimits");

async function limitDetailsScene(root, mount) {
    const reading = (name) => `.limit-reading[aria-label^="${name}"]`;
    const tap = async (selector) => {
        const target = document.querySelector(selector);
        if (!target) throw new Error(`missing tap target: ${selector}`);
        const pointer = { bubbles: true, pointerId: 1, pointerType: "touch", isPrimary: true };
        target.dispatchEvent(new PointerEvent("pointerdown", pointer));
        target.dispatchEvent(new PointerEvent("pointerup", pointer));
        target.click();
        await quiet();
    };
    const closed = () => !document.querySelector(".limit-popover") && !document.querySelector('.limit-reading[aria-expanded="true"]');
    const details = () => {
        const pop = document.querySelector(".limit-popover");
        const time = pop?.querySelector("time");
        return {
            count: document.querySelectorAll(".limit-popover").length,
            words: text(pop), label: pop?.getAttribute("aria-label") || "", role: pop?.getAttribute("role") || "",
            resetAt: time?.dateTime || "", resetLocal: text(time),
            compact: Boolean(document.querySelector('.limit-mini[aria-expanded="true"]')),
            readings: [...rowBox().querySelectorAll(".limit-reading")].map((button) => ({
                label: button.getAttribute("aria-label"), tag: button.tagName, type: button.type,
                popup: button.getAttribute("aria-haspopup"), expanded: button.getAttribute("aria-expanded"), focusable: button.tabIndex >= 0,
            })),
        };
    };
    const at = new Date(limits.contours[0].fiveHour.resetsAt * 1000);
    const out = { expectedResetAt: at.toISOString(), expectedResetLocal: at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false }) };
    await tap(reading("Claude, 5 hours"));
    out.full = details();
    await tap(reading("Claude, 5 hours"));
    out.toggledClosed = closed();
    await tap(reading("Claude, 5 hours"));
    await tap(reading("Claude, 7 days"));
    out.week = details();
    await tap(reading("Codex, 7 days"));
    out.other = details();
    await tap(".limit-popover strong");
    out.insideKept = details().count === 1;
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await quiet();
    out.escapeClosed = closed();
    await tap(reading("Claude, 5 hours"));
    await tap(".pflimitrow");
    out.outsideClosed = closed();
    await tap(reading("Claude, 5 hours"));
    await tuckToolbar();
    out.foldClosed = closed();
    await tap(reading("Claude, 5 hours"));
    out.folded = details();
    await moveToolbar(0);
    out.unfoldClosed = closed();

    const own = { contours: [{ profile: "details", contour: 72, ageSec: 5400, fiveHour: windowOf(45.5, 1), sevenDay: { pct: 12.25, resetsAt: 0 },
        codex: { ageSec: 10800, reached: true, sevenDay: windowOf(100, 3) } }] };
    render(h(ToastHost, null, h(GateHost, null, html`<main class="body">
        <${ProfileLimits} key="details" profile="details" contour=${72} limits=${own} stale=${false}
            entry=${profiles[0]} snapshot=${snapshot} exec=${exec} onTasks=${() => {}} onSettings=${() => {}} />
    </main>`)), root);
    await quiet();
    await moveToolbar(0);
    await tap(reading("Claude, 5 hours"));
    out.stale = details();
    await tap(reading("Codex, 7 days"));
    out.codex = details();
    await tap(reading("Claude, 7 days"));
    out.noReset = details();
    mount();
    await quiet();
    await moveToolbar(0);
    return out;
}

function readToolbar() {
    const box = rowBox();
    return {
        tucked: box.classList.contains("tucked"),
        stuck: box.classList.contains("stuck"),
        words: text(box.querySelector(".pflimitrow")),
        vendors: [...box.querySelectorAll(".agentword")].map(text),
        frames: [...box.querySelectorAll("fieldset.limit-group")].map((frame) => ({ vendor: text(frame.querySelector("legend")), windows: frame.querySelectorAll(".limit-reading").length })),
        readings: [...box.querySelectorAll(".limit-reading")].map((reading) => ({
            vendor: reading.dataset.agent, percent: text(reading.querySelector(".limit-percent")),
            label: reading.getAttribute("aria-label"), title: reading.title, tone: reading.className,
            old: reading.classList.contains("old"), clock: Boolean(reading.querySelector(".limit-old")),
            gauge: Boolean(reading.querySelector(".limit-gauge")), share: reading.style.getPropertyValue("--share"), bar: Boolean(reading.querySelector(".limit-minitrack")),
        })),
        actions: [...box.querySelectorAll(".pfaction")].map((button) => ({
            label: button.getAttribute("aria-label"), title: button.title, words: text(button),
            available: !button.disabled && button.getClientRects().length > 0 && getComputedStyle(button).visibility !== "hidden",
            icon: Boolean(button.querySelector("svg")), framedByVendor: Boolean(button.closest("fieldset")),
        })),
    };
}

async function moveToolbar(y) {
    pageOfRow().scrollTop = y;
    // The fold can change scroll anchoring on the next frame. Read only once
    // the scroll and that correction have both had their turn.
    for (let i = 0; i < 6; i += 1) await new Promise(requestAnimationFrame);
    await quiet();
    return readToolbar();
}

async function tuckToolbar() {
    const page = pageOfRow();
    const content = page.querySelector(".pfcol") || page;
    // A scene drawn into the same main keeps the room an earlier scene added,
    // and preact leaves it before the new row: it goes back to the end, under
    // the row it makes room to scroll past.
    let room = content.querySelector("[data-limit-scroll-room]");
    if (!room) {
        room = document.createElement("div");
        room.dataset.limitScrollRoom = "";
        room.setAttribute("aria-hidden", "true");
        room.style.cssText = "height:100vh;flex:none";
    }
    content.append(room);
    await moveToolbar(0);
    const edge = rowBox().getBoundingClientRect().bottom - page.getBoundingClientRect().top;
    await moveToolbar(edge + 40);
    await until(() => rowBox().classList.contains("tucked"));
}

// This census exercises the contour pager separately from registry actions.
// Its windows and conversations also cover a reset with a fractional share.
async function scrollScene(root) {
    const part = (pct, hours) => ({ pct, resetsAt: now + hours * 3600 + 1800 });
    const profileMap = [{ id: 4, profile: "Acme", name: "Acme", groups: [] }, { id: 3, profile: "personal", name: "personal", groups: [] }];
    const limits = { contours: [
        { profile: "Acme", contour: 4, ageSec: 30, fiveHour: part(99.5, 1), sevenDay: part(88, 132),
          codex: { at: now - 30, ageSec: 30, codexHome: "/home/u/.codex", windows: [], sevenDay: part(44, 132) } },
        { profile: "personal", contour: 3, ageSec: 30, fiveHour: part(12, 2), sevenDay: part(24, 100) },
    ] };
    const run = (session, contour) => ({ session, sessionId: `${session}-id`, status: "idle", transport: "stream",
        pct: 12, tokens: 100000, limit: 1000000, cwd: `/srv/${session}`, model: "claude-opus-5-5", contour });
    const sessions = Array.from({ length: 14 }, (_, i) => run(`shop${i}`, 4)).concat([run("notes", 3)]);
    render(h(ToastHost, null, h(GateHost, null, html`
        <main class="body"><${Sessions} key="scroll" snapshot=${{ at: 1, sessions, profileMap, limits }} error=${null} ageSec=${1}
            exec=${{ available: true, kinds: ["session.send"] }} wait=${wait} faults=${[]} onLayer=${() => {}} /></main>
        <${Nav} current="sessions" onSelect=${() => {}} home=${null} />
    `)), root);
    await until(() => document.querySelectorAll(".pfpage .pflimits .limit-ring").length >= 3);
    await document.fonts.ready;
    await moveToolbar(0);
    const edge = Math.round(rowBox().getBoundingClientRect().bottom - pageOfRow().getBoundingClientRect().top);
    const stops = [];
    for (const y of [0, edge - 1, edge + 40, 1, 0]) stops.push(await moveToolbar(y));
    return stops;
}

async function vendorScene(root, vendor, fold = true) {
    const name = `${vendor} only`;
    const own = vendor === "claude"
        ? { ageSec: 30, fiveHour: windowOf(59, 1), sevenDay: windowOf(60, 100) }
        : { codex: { ageSec: 4000, sevenDay: windowOf(85, 100) } };
    const limits = { contours: [{ profile: name, contour: 71, ...own }] };
    render(h(ToastHost, null, h(GateHost, null, html`<main class="body">
        <${ProfileLimits} key=${vendor} profile=${name} contour=${71} limits=${limits} stale=${false}
            entry=${profiles[0]} snapshot=${snapshot} exec=${exec} onTasks=${() => {}} onSettings=${() => {}} />
    </main>`)), root);
    await quiet();
    await moveToolbar(0);
    const full = readToolbar();
    if (!fold) return { vendor, full };
    await tuckToolbar();
    return { vendor, full, folded: readToolbar() };
}


async function foldedActions(out, mount) {
    mode = "list";
    mount();
    await quiet();
    await tuckToolbar();
    out.tuckedNew = readToolbar();
    const before = sent.length;
    await click('.pfpage .pfaction[aria-label="New session"]');
    await until(() => sheet("New session"));
    await click('.sheet.on[aria-label="New session"] .newproject');
    await until(() => sheet("action confirmation"));
    out.foldedBeforeConfirm = sent.length - before;
    await click('.sheet.on[aria-label="action confirmation"] .btn.primary');
    await until(() => sent.length === before + 1 && !sheet("action confirmation"));
    out.foldedSent = sent.slice(before);
    await tuckToolbar();
    out.tuckedProfile = readToolbar();
    await click('.pfpage .pfaction[aria-label="Profile"]');
    await until(() => document.querySelector(".phead h2")?.textContent === "Acme" && document.querySelector(".pzwhere .pzcard"));
    out.foldedProfile = true;
    await click(".pback");
    await until(() => document.querySelector(".pflimitrow"));
    await tuckToolbar();
    out.tuckedTasks = readToolbar();
    await click('.pfpage .pfaction[aria-label="Tasks"]');
    await until(() => document.querySelector(".tqrow"));
    out.foldedTasks = Boolean(document.querySelector(".tqscreen"));
    await back(() => !document.querySelector(".tqscreen"));
}
