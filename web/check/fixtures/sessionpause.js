// Pause and restart controls share the pages that already exercise sessions:
// the pause of a project from the heading over its cards, the pause and the
// continue of one session from its card — its buttons at a desk, its pie on
// the phone — and the restart choices. lab-docs had the panel open a helper
// in the other project; a scope of its own includes that helper. The quick
// choices are pressed on a clock set by the fixture.
import { render } from "preact";
import { html } from "/src/html.js";
import { ProjectBlock } from "/src/screens/sessions/blocks.js";
import { CardPieHost } from "/src/screens/sessions/cardpie.js";
import { SessionColumn } from "/src/desktop/sessions.js";
import { Chat } from "/src/screens/chat.js";
import { Row } from "/src/screens/chat/rows.js";
import { stateOf as listState } from "/src/screens/sessions/blocks.js";
import { stateOf as headState } from "/src/screens/chat/head.js";
import { setLiveSessions } from "/src/actions/registry.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";
import { MenuHost } from "/src/desktop/menu.js";
import { counted, quiet } from "/check/fixtures/quiet.js";
import { openPie, ring, sector, settled, underPies } from "/check/fixtures/pie.js";

const until = async (ready, what) => {
    const end = performance.now() + 4000;
    while (!ready() && performance.now() < end) await new Promise(requestAnimationFrame);
    if (!ready()) throw new Error(`the screen did not reach ${what}`);
};

export async function sessionPause() {
    const wide = matchMedia("(min-width:1100px)").matches;
    const mount = document.createElement("div");
    document.body.append(mount);
    mount.id = "pausepreview";
    const project = { id: 501, name: "lab", session: "lab", path: "/srv/lab" };
    const peer = { id: 502, name: "peer", session: "peer", path: "/srv/peer" };
    const profile = { id: 50, profile: "Work", groups: [{ name: "Services", projects: [project, peer] }, { name: "Other", projects: [] }] };
    const next = new Date(Date.now() + 7200000).toISOString();
    const pause = { id: "0123456789abcdef", name: "pause", next };
    const alarm = { id: "fedcba9876543210", name: "review", next: new Date(Date.now()+3600000).toISOString() };
    const sessions = [
        { session: "lab", sessionId: "lab-id", project, contour: 50, status: "idle", transport: "stream", pct: 35,
            pauseAlarm: pause, nextAlarm: alarm, work: { alarms: 2 }, checklist: { items: [{ text: "Review the implementation", status: "active" }] } },
        { session: "lab-docs", sessionId: "docs-id", project, contour: 50, status: "idle", transport: "stream", pct: 12 },
        { session: "thread", sessionId: "thread-id", project: peer, agent: "codex", contour: 50, status: "idle", pct: 10, transport: "stream" },
        { session: "quiet", sessionId: "quiet-id", project: peer, contour: 50, status: "idle", pct: 5, transport: "stream" },
        { session: "outside", sessionId: "outside-id", project, contour: 50, outside: true, status: "idle", pct: 5 },
        { session: "helper", sessionId: "helper-id", project: peer, parent: { session: "lab-docs" }, contour: 50, status: "idle", pct: 3, transport: "stream", pauseAlarm: pause },
    ];
    const snapshot = { at: Date.now()/1000, sessions, profileMap: [profile] };
    const exec = { available: true, kinds: ["project.pause", "session.open", "session.close", "session.restart", "session.pin", "session.send"] };
    const fetch = window.fetch;
    const sent = [];
    const json = (body) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
    window.fetch = counted(async (url, init) => {
        if (String(url) === "/api/actions") { sent.push(JSON.parse(init.body)); return json({ ok: true, delivered: 3, failed: [], detail: "3 sessions received the message" }); }
        if (String(url).startsWith("/api/chat?")) return json({ items: [], first: 0, last: 0, total: 0, state: null });
        if (String(url).startsWith("/api/sessions/archive")) return json({ rows: [], total: 0 });
        return new Response("", { status: 404 });
    });
    const block = (p, list) => ({ key: p.id, name:p.name, project:p, group:"Services", live:list, ghosts:[] });
    const messages = [
        { role:"mail", source:"panel", from:"pause", until:next, text:"Words without a date.", pos:1 },
        { role:"mail", source:"panel", from:"pause over", text:"Lifted.", pos:2 },
        { role:"mail", source:"alarm", from:"pause", text:"Rang.", pos:3 },
        { role:"mail", source:"panel", from:"pause with no end", text:"Words without an end.", pos:4 },
    ];
    const show = (snapshot, chat = false) => {
        const live = snapshot.sessions;
        setLiveSessions(live);
        render(html`<${ToastHost}><${GateHost}><${MenuHost}>
            ${wide ? html`<div class="deskshell" style="position:static;height:650px"><div class="dkbody">
                <${SessionColumn} snapshot=${snapshot} limits=${null} current="" onPick=${() => {}} picks=${[]} setPicks=${() => {}} exec=${exec} />
            </div></div>` : html`<${CardPieHost} snapshot=${snapshot} exec=${exec} wait=${null}><div class="page">
                <${ProjectBlock} block=${block(project, live.filter((s) => !s.outside && s.project?.id === project.id))} profile=${profile} live=${live} exec=${exec} />
            </div><//>`}
            ${chat && html`<div class="pausechat" style="height:500px;display:flex;flex-direction:column">
                <${Chat} name="lab" id="lab-id" live=${live[0]} snapshot=${snapshot} exec=${exec} onBack=${() => {}} />
            </div>
            <div class="chatfeed pausefeed">${messages.map((item) => html`<${Row} key=${item.pos} item=${item} />`)}</div>`}
        <//><//><//>`, mount);
    };
    show(snapshot, true);
    const text = (el) => el?.textContent.replace(/\s+/g," ").trim() || "";
    // The page's clock at a time of today, and what the field of a moment
    // reads, built apart from the sheet's own arithmetic.
    const clock = Date.now;
    const today = new Date();
    const at = (day, h, m = 0) => new Date(today.getFullYear(), today.getMonth(), today.getDate() + day, h, m);
    const local = (d) => `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,"0")}-${String(d.getDate()).padStart(2,"0")}T${String(d.getHours()).padStart(2,"0")}:${String(d.getMinutes()).padStart(2,"0")}`;
    const field = () => sheet().querySelector(".pauseuntil");
    const choose = async (name) => {
        [...sheet().querySelectorAll(".pausechips .chip")].find((b) => text(b) === name).click();
        await quiet();
        return { value: field().value, pressed: [...sheet().querySelectorAll('.pausechips .chip[aria-pressed="true"]')].map(text) };
    };
    const pauseButton = () => mount.querySelector('[aria-label="pause sessions in project lab"]');
    const sheet = () => mount.querySelector('.sheet.on[aria-label="pause sessions"]');
    const gate = () => mount.querySelector('.sheet.on[aria-label="action confirmation"]');
    const recipients = () => [...sheet().querySelectorAll(".pauserecipients li")].map(text);
    const scopeOptions = () => [...sheet().querySelectorAll(".pausescope option")].map((o) => ({ value: o.value, label: text(o) }));
    const scopeCount = () => [...sheet().querySelectorAll(".cmdnote")].map(text).find((t) => /sessions? in this scope:/.test(t));
    const selectScope = async (value) => {
        sheet().querySelector(".pausescope").value = value;
        sheet().querySelector(".pausescope").dispatchEvent(new Event("change", { bubbles: true }));
        await quiet();
    };
    // A control of a card: its button at a desk, the sector of its pie on the
    // phone, Continue in the place of Pause on a paused card.
    const press = async (kind, name) => {
        if (wide) {
            mount.querySelector(`[aria-label="${kind} session ${name}"]`).click();
            return;
        }
        await openPie(mount.querySelector(`.sc[data-name="${name}"]`));
        sector(kind === "continue" ? "pause" : kind).click();
    };
    try {
        await until(() => pauseButton() && mount.querySelector(".pausechat .talkword, .pausechat .dkword"), "session controls");
        await quiet();
        // The conversation beside the list is a layer of its own under the
        // pies of the cards.
        underPies();
        const card = mount.querySelector('.sc[data-name="lab"]');
        const out = {
            state: text(card.querySelector(".scsay")),
            ends: text(card.querySelector(".sctime")),
            header: text(mount.querySelector(".pausechat .talkword, .pausechat .dkword")),
            feed: [...mount.querySelectorAll(".pausecard .sentlabel")].map(text),
            letters: mount.querySelectorAll(".pausefeed .mletter").length,
            priority: [
                { ...sessions[0], status:"busy" }, { ...sessions[0], status:"waiting" },
                { ...sessions[0], work:{agents:1} }, { ...sessions[0], pauseAlarm:undefined },
                { ...sessions[0], restarting:true },
            ].map((s) => [listState(s).tone, headState(s).tone]),
            expect: { nineToday: local(at(0, 9)), nineTomorrow: local(at(1, 9)), twoHours: local(at(0, 11, 30)),
                nineTodayAt: at(0, 9).toISOString(), nineTomorrowAt: at(1, 9).toISOString() },
        };
        pauseButton().click();
        await until(() => sheet(), "pause sheet");
        out.projectRecipients = recipients();
        await selectScope("group");
        out.groupRecipients = recipients();
        out.opened = { value: field().value, pressed: [...sheet().querySelectorAll('.pausechips .chip[aria-pressed="true"]')].map(text),
            ahead: (new Date(field().value).getTime() - Date.now()) / 60000 };
        out.choices = [...sheet().querySelectorAll(".pausepreset")].map((row) => [...row.querySelectorAll(".pauselabel, .chip")].map(text));
        // Half past eight: until 9 is nine today.
        Date.now = () => at(0, 8, 30).getTime();
        try {
            out.beforeNine = await choose("9:00");
            sheet().querySelector(".pauseapply").click();
            await until(() => sent.length===1 && !sheet(), "pause request");
        } finally {
            Date.now = clock;
        }
        pauseButton().click();
        await until(() => sheet(), "resume sheet");
        sheet().querySelector(".pauseresume").click();
        await until(() => sent.length===2 && !sheet(), "resume request");
        out.pauseSent = sent.slice();
        out.noConfirmation = !gate();
        out.restart = [];
        for (let i=0;i<3;i++) {
            await press("restart", "lab");
            await until(() => gate(), "restart choices");
            const options = [...gate().querySelectorAll('[role="radio"]')];
            if (i===0) out.options=options.map(text);
            options[i].click();
            await quiet();
            gate().querySelector(".btnrow .btn:last-child").click();
            await until(() => sent.length===3+i && !gate(), "restart request");
            out.restart.push(sent.at(-1));
        }
        // One session: the paused card continues it at once, the pause of
        // another opens the sheet with that session as its scope.
        await press("continue", "lab");
        await until(() => sent.length===6, "continue request");
        await press("pause", "lab-docs");
        await until(() => sheet(), "pause sheet of one session");
        out.sessionScope = sheet().querySelector(".pausescope").value;
        out.sessionScopes = scopeOptions();
        out.sessionRecipients = recipients();
        out.sessionCount = scopeCount();
        out.aloneResumeAbsent = !sheet().querySelector(".pauseresume");
        await selectScope("opened");
        out.openedRecipients = recipients();
        out.openedCount = scopeCount();
        out.openedResume = text(sheet().querySelector(".pauseresume"));
        await selectScope("session");
        // Half past nine: two hours is half past eleven, and until 9 is nine
        // tomorrow.
        Date.now = () => at(0, 9, 30).getTime();
        try {
            out.forTwo = await choose("2 h");
            out.afterNine = await choose("9:00");
            sheet().querySelector(".pauseapply").click();
            await until(() => sent.length===7 && !sheet(), "pause request of one session");
        } finally {
            Date.now = clock;
        }
        await press("pause", "lab-docs");
        await until(() => sheet(), "pause sheet with no end");
        await selectScope("opened");
        out.noEnd = { ...(await choose("No end")), off: field().disabled, note: text(sheet().querySelector(".pausenoend")) };
        sheet().querySelector(".pauseapply").click();
        await until(() => sent.length===8 && !sheet(), "pause request with no end");
        await press("pause", "lab-docs");
        await until(() => sheet(), "resume sheet of opened sessions");
        await selectScope("opened");
        sheet().querySelector(".pauseresume").click();
        await until(() => sent.length===9 && !sheet(), "resume request of opened sessions");
        out.oneSent = sent.slice(5);
        if (wide) {
            mount.querySelector(".dkproj").dispatchEvent(new MouseEvent("contextmenu",{bubbles:true,cancelable:true,clientX:50,clientY:100}));
            await until(() => mount.querySelector(".pkrow"), "project menu");
            const pauseItem=[...mount.querySelectorAll(".pkrow")].find((b)=>text(b.querySelector(".pkname"))==="Pause…");
            out.menu=Boolean(pauseItem) && pauseItem.getAttribute("aria-disabled")!=="true";
            pauseItem.click();
            await until(() => sheet(), "pause from the menu");
            sheet().querySelector(".sheetclose").click();
        }
        await quiet();
        show({ ...snapshot, sessions: [sessions[1]] });
        await quiet();
        underPies();
        await press("pause", "lab-docs");
        await until(() => sheet(), "pause sheet without opened sessions");
        out.noOpenedScopes = scopeOptions();
        sheet().querySelector(".sheetclose").click();
        await quiet();
        const sleepers = [{ ...sessions[0], sleeping: true },
            { ...sessions[1], sleeping: true, pauseAlarm: { id: "panel:docs-id", name: "pause-no-end", next: "" } }];
        const sleeping = { ...snapshot, sessions: sleepers };
        show(sleeping);
        await quiet();
        underPies();
        const sleepingCard = mount.querySelector('.sc[data-name="lab"]');
        out.sleep = { states: [...mount.querySelectorAll(".sc .scsay")].map(text) };
        if (wide) {
            out.sleep.controls = [...sleepingCard.querySelectorAll(".scact")].map((el) => ({
                label: el.getAttribute("aria-label"), off: el.getAttribute("aria-disabled") === "true", why: el.getAttribute("data-tip") || "",
            }));
            sleepingCard.dispatchEvent(new MouseEvent("contextmenu", { bubbles:true, cancelable:true, clientX:50, clientY:100 }));
            await until(() => mount.querySelector(".pkrow"), "sleeping menu");
            out.sleep.menu = [...mount.querySelectorAll(".pkrow")].map((el) => ({
                label: text(el.querySelector(".pkname")), off: el.getAttribute("aria-disabled") === "true", why: el.getAttribute("data-tip") || "",
            }));
            [...mount.querySelectorAll(".pkrow")].find((el) => text(el.querySelector(".pkname")) === "Continue now").click();
        } else {
            await openPie(sleepingCard);
            out.sleep.controls = ring();
            sector("pause").click();
        }
        await until(() => sent.length === 10, "sleeping Continue");
        out.sleep.continue = sent.at(-1);
        if (!wide) await settled();
        await press("close", "lab");
        await until(() => gate(), "sleeping Close confirmation");
        gate().querySelector(".btnrow .btn:last-child").click();
        await until(() => sent.length === 11 && !gate(), "sleeping Close");
        out.sleep.close = sent.at(-1);
        if (!wide) await settled();
        return out;
    } finally {
        render(null,mount); mount.remove(); window.fetch=fetch;
    }
}
