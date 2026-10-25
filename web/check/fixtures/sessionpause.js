// Pause and restart controls share the pages that already exercise sessions.
import { render } from "preact";
import { html } from "/src/html.js";
import { ProjectBlock } from "/src/screens/sessions/blocks.js";
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
    const empty = { id: 503, name: "empty", session: "empty", path: "/srv/empty" };
    const profile = { id: 50, profile: "Work", groups: [{ name: "Services", projects: [project, peer] }, { name: "Other", projects: [empty] }] };
    const next = new Date(Date.now() + 7200000).toISOString();
    const pause = { id: "0123456789abcdef", name: "pause", next };
    const alarm = { id: "fedcba9876543210", name: "review", next: new Date(Date.now()+3600000).toISOString() };
    const sessions = [
        { session: "lab", sessionId: "lab-id", project, contour: 50, status: "idle", transport: "stream", pct: 35,
            pauseAlarm: pause, nextAlarm: alarm, work: { alarms: 2 }, checklist: { items: [{ text: "Review the implementation", status: "active" }] } },
        { session: "thread", sessionId: "thread-id", project: peer, agent: "codex", contour: 50, status: "idle", pct: 10, transport: "stream" },
        { session: "quiet", sessionId: "quiet-id", project: peer, contour: 50, status: "idle", pct: 5, transport: "stream" },
        { session: "outside", sessionId: "outside-id", project, contour: 50, outside: true, status: "idle", pct: 5 },
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
    setLiveSessions(sessions);
    const block = (p, list) => ({ key: p.id, name:p.name, project:p, group:"Services", live:list, ghosts:[] });
    const messages = [
        { role:"mail", source:"panel", from:"pause", until:next, text:"Words without a date.", pos:1 },
        { role:"mail", source:"panel", from:"pause over", text:"Lifted.", pos:2 },
        { role:"mail", source:"alarm", from:"pause", text:"Rang.", pos:3 },
    ];
    render(html`<${ToastHost}><${GateHost}><${MenuHost}>
        ${wide ? html`<div class="deskshell" style="position:static;height:650px"><div class="dkbody">
            <${SessionColumn} snapshot=${snapshot} limits=${null} current="" onPick=${() => {}} picks=${[]} setPicks=${() => {}} exec=${exec} />
        </div></div>` : html`<div class="page">
            <${ProjectBlock} block=${block(project,[sessions[0]])} profile=${profile} live=${sessions} exec=${exec} />
            <${ProjectBlock} block=${block(empty,[])} profile=${profile} live=${sessions} exec=${exec} />
        </div>`}
        <div class="pausechat" style="height:500px;display:flex;flex-direction:column">
            <${Chat} name="lab" id="lab-id" live=${sessions[0]} snapshot=${snapshot} exec=${exec} onBack=${() => {}} />
        </div>
        <div class="chatfeed pausefeed">${messages.map((item) => html`<${Row} key=${item.pos} item=${item} />`)}</div>
    <//><//><//>`, mount);
    const text = (el) => el?.textContent.replace(/\s+/g," ").trim() || "";
    const pauseButton = () => mount.querySelector('[aria-label="pause sessions in project lab"]');
    const sheet = () => mount.querySelector('.sheet.on[aria-label="pause sessions"]');
    const gate = () => mount.querySelector('.sheet.on[aria-label="action confirmation"]');
    try {
        await until(() => pauseButton() && mount.querySelector(".pausechat .talkword, .pausechat .dkword"), "session controls");
        await quiet();
        const card = wide ? mount.querySelector(".dklive") : mount.querySelector(".pjcard");
        const out = {
            state: text(card.querySelector(wide ? ".dksay" : ".pjtext")),
            alarm: text(card.querySelector(".sessionalarm")),
            header: text(mount.querySelector(".pausechat .talkword, .pausechat .dkword")),
            feed: [...mount.querySelectorAll(".pausecard .sentlabel")].map(text),
            letters: mount.querySelectorAll(".pausefeed .mletter").length,
            priority: [
                { ...sessions[0], status:"busy" }, { ...sessions[0], status:"waiting" },
                { ...sessions[0], work:{agents:1} }, { ...sessions[0], pauseAlarm:undefined },
                { ...sessions[0], restarting:true },
            ].map((s) => [listState(s).tone, headState(s).tone]),
        };
        pauseButton().click();
        await until(() => sheet(), "pause sheet");
        out.pauseFits = sheet().querySelector(".btnrow").getBoundingClientRect().bottom <= innerHeight;
        out.projectRecipients = [...sheet().querySelectorAll(".pauserecipients li")].map(text);
        sheet().querySelector(".pausescope").value="group";
        sheet().querySelector(".pausescope").dispatchEvent(new Event("change",{bubbles:true}));
        await quiet();
        out.groupRecipients = [...sheet().querySelectorAll(".pauserecipients li")].map(text);
        sheet().querySelector(".pausepresets button").click();
        await quiet();
        out.chosen = sheet().querySelector(".pauseuntil").value;
        sheet().querySelector(".pauseapply").click();
        await until(() => sent.length===1 && !sheet(), "pause request");
        pauseButton().click();
        await until(() => sheet(), "resume sheet");
        sheet().querySelector(".pauseresume").click();
        await until(() => sent.length===2 && !sheet(), "resume request");
        out.pauseSent = sent.slice();
        out.noConfirmation = !gate();
        out.restart = [];
        for (let i=0;i<3;i++) {
            mount.querySelector('[aria-label="restart session lab"]').click();
            await until(() => gate(), "restart choices");
            const options = [...gate().querySelectorAll('[role="radio"]')];
            if (i===0) out.options=options.map(text);
            options[i].click();
            await quiet();
            if (i===0) out.restartFits=gate().querySelector(".btnrow").getBoundingClientRect().bottom<=innerHeight;
            gate().querySelector(".btnrow .btn:last-child").click();
            await until(() => sent.length===3+i && !gate(), "restart request");
            out.restart.push(sent.at(-1));
        }
        if (wide) {
            mount.querySelector(".dkproj").dispatchEvent(new MouseEvent("contextmenu",{bubbles:true,cancelable:true,clientX:50,clientY:100}));
            await until(() => mount.querySelector(".pkrow"), "project menu");
            const pauseItem=[...mount.querySelectorAll(".pkrow")].find((b)=>text(b.querySelector(".pkname"))==="Pause…");
            out.menu=Boolean(pauseItem) && pauseItem.getAttribute("aria-disabled")!=="true";
            pauseItem.click();
            await until(() => sheet(), "pause from the menu");
            sheet().querySelector(".sheetclose").click();
        } else {
            const button=mount.querySelector('[aria-label="pause sessions in project empty"]');
            out.empty=button.getAttribute("aria-disabled")==="true" && button.title.includes("No live sessions");
        }
        return out;
    } finally {
        render(null,mount); mount.remove(); window.fetch=fetch;
    }
}
