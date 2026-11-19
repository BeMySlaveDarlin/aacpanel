// Both screens reuse their session page and one open pie for the dial's
// selections, gestures and limits. Only the bar commits a chosen deadline.
import { render } from "preact";
import { html } from "/src/html.js";
import { ProjectBlock } from "/src/screens/sessions/blocks.js";
import { CardPieHost } from "/src/screens/sessions/cardpie.js";
import { SessionColumn } from "/src/desktop/sessions.js";
import { Chat } from "/src/screens/chat.js";
import { Row } from "/src/screens/chat/rows.js";
import { stateOf as listState } from "/src/screens/sessions/blocks.js";
import { stateOf as headState } from "/src/screens/chat/head.js";
import { todayScale } from "/src/screens/sessions/pausedial.js";
import { pauseEnd, pauseRows } from "/src/screens/sessions/pause.js";
import { setLiveSessions } from "/src/actions/registry.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";
import { MenuHost } from "/src/desktop/menu.js";
import { SessionMenu } from "/src/desktop/sessionmenu.js";
import { useMenu } from "/src/ui/menu.js";
import { counted, quiet } from "/check/fixtures/quiet.js";
import { closePie, middle, openPie, pie, plate, press, ring, sector, settled, underPies, until } from "/check/fixtures/pie.js";

export async function sessionPause() {
    const clock = Date.now;
    const now = new Date(2026, 10, 18, 15, 20).getTime();
    Date.now = () => now;
    const at = (day, hour, minute = 0) => new Date(2026, 10, 18 + day, hour, minute);
    const wide = matchMedia("(min-width:1100px)").matches;
    const mount = document.createElement("div");
    document.body.append(mount);
    const project = { id: 501, name: "evirma", session: "evirma", path: "/srv/evirma" };
    const peer = { id: 502, name: "peer", session: "peer", path: "/srv/peer" };
    const profile = { id: 50, profile: "Work", groups: [{ name: "backend", projects: [project, peer] }] };
    const pause = { id: "0123456789abcdef", name: "pause", next: at(0, 18).toISOString() };
    const sessions = [
        { session: "evirma-api", sessionId: "api-id", project, contour: 50, status: "idle", transport: "stream", pct: 35 },
        { session: "review", sessionId: "review-id", project: peer, parent: { session: "evirma-api" }, contour: 50, status: "idle", transport: "stream", pct: 12 },
        { session: "tests", sessionId: "tests-id", project: peer, parent: { session: "evirma-api" }, contour: 50, status: "idle", transport: "stream", pct: 10 },
        { session: "grandchild", sessionId: "grand-id", project: peer, parent: { session: "review" }, contour: 50, status: "idle", transport: "stream", pct: 5 },
        { session: "evirma-billing", sessionId: "bill-id", project, contour: 50, status: "idle", transport: "stream", pct: 5 },
        { session: "outside", sessionId: "outside-id", project, contour: 50, outside: true, status: "idle", pct: 5 },
    ];
    const snapshot = { at: now / 1000, sessions, profileMap: [profile] };
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
    const text = (el) => el?.textContent.replace(/\s+/g, " ").trim() || "";
    const messages = [
        { role: "mail", source: "panel", from: "pause", until: at(2, 10).toISOString(), text: "Pause instruction.", pos: 1 },
        { role: "mail", source: "panel", from: "pause over", text: "Lifted.", pos: 2 },
        { role: "mail", source: "alarm", from: "pause", text: "Rang.", pos: 3 },
        { role: "mail", source: "panel", from: "pause with no end", text: "No deadline.", pos: 4 },
    ];
    // The list's card uses its pie; the graph's context menu shares the
    // desktop menu host and supplies the other route to the same dial.
    function MenuProbe({ live }) {
        const menu = useMenu();
        return html`<button class="pausemenuprobe" onContextMenu=${menu(SessionMenu, { s: live[0], exec, snapshot: { ...snapshot, sessions: live }, profile }, "session menu")}>Session menu</button>`;
    }
    const show = async (live = sessions, chat = false) => {
        setLiveSessions(live);
        const snap = { ...snapshot, sessions: live };
        const block = { key: 501, name: project.name, project, group: "backend", live: live.filter((s) => !s.outside && s.project.id === project.id), ghosts: [] };
        render(html`<${ToastHost}><${GateHost}><${MenuHost}><${CardPieHost} snapshot=${snap} exec=${exec}>
            ${wide ? html`<div class="deskshell" style="position:static;height:650px"><div class="dkbody">
                <${SessionColumn} snapshot=${snap} limits=${null} current="" onPick=${() => {}} picks=${[]} setPicks=${() => {}} exec=${exec} />
            </div></div><${MenuProbe} live=${live} />` : html`<div class="page"><${ProjectBlock} block=${block} profile=${profile} live=${live} exec=${exec} /></div>`}
            ${chat && html`<div class="pausechat" style="height:500px;display:flex;flex-direction:column">
                <${Chat} name="evirma-api" id="api-id" live=${live[0]} snapshot=${snap} exec=${exec} onBack=${() => {}} />
            </div><div class="chatfeed pausefeed">${messages.map((item) => html`<${Row} key=${item.pos} item=${item} />`)}</div>`}
        <//><//><//><//>`, mount);
        await quiet();
        await underPies(chat ? 1 : 0);
    };
    const slider = () => pie()?.querySelector('[role="slider"]');
    const apply = () => pie().querySelector(".pauseapply");
    const toggle = () => pie().querySelector('[role="switch"]');
    const picked = () => [...pie().querySelectorAll('.piesec[aria-checked="true"].pieon')].map((el) => el.dataset.key);
    const choice = () => ({ disabled: apply().disabled, off: slider().getAttribute("aria-disabled") === "true",
        value: slider().getAttribute("aria-valuetext"), picked: picked() });
    const hoverSector = (key) => {
        const point = middle(key);
        sector(key).dispatchEvent(new PointerEvent("pointermove", { bubbles: true, pointerType: "mouse", clientX: point.x, clientY: point.y }));
    };
    const crownGone = () => !slider() && !pie().querySelector(".piebar");
    const crownPoint = (fraction) => {
        const box = slider().getBoundingClientRect();
        const radius = (box.width / 2 - 16) * .86;
        const angle = (-150 + fraction * 120) * Math.PI / 180;
        return { x: box.left + box.width / 2 + radius * Math.cos(angle), y: box.top + box.height / 2 + radius * Math.sin(angle) };
    };
    const clickAt = (el, point) => el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, detail: 1, clientX: point.x, clientY: point.y }));
    const choose = async (fraction, changes = true) => {
        const face = slider();
        const before = face.getAttribute("aria-valuetext");
        const hadDays = Boolean(sector("day-1"));
        const enabled = face.getAttribute("aria-disabled") !== "true";
        const point = crownPoint(fraction);
        press("pointerdown", face, point, 2);
        press("pointerup", face, point, 2);
        clickAt(face, point);
        if (changes && enabled) await until(() => Boolean(sector("day-1")) !== hadDays || slider().getAttribute("aria-valuetext") !== before, "the dial choice");
        else await quiet();
    };
    const open = async (name = "evirma-api") => {
        await openPie(mount.querySelector(`.sc[data-name="${name}"]`));
        sector("pause").click();
        await until(slider, "the pause crown");
    };
    const commit = async () => {
        const before = sent.length;
        apply().click();
        await until(() => sent.length === before + 1 && !pie(), "the pause action");
        await settled();
        return sent.at(-1);
    };
    const pickDay = async (n) => {
        const key = `day-${n}`;
        const before = sector(key).getAttribute("aria-checked");
        sector(key).focus();
        sector(key).click();
        await until(() => sector(key).getAttribute("aria-checked") !== before, "the clicked day");
    };
    const openDays = async () => { await choose(.81); await until(() => sector("day-1"), "the days ring"); };
    try {
        const sleeping = sessions.map((s, i) => i < 3 ? { ...s, sleeping: true, pauseAlarm: pause } : s);
        await show([{ ...sleeping[0], pauseAlarm: { ...pause, next: at(2, 10).toISOString() } }, ...sleeping.slice(1)], true);
        const out = { sent: {}, expect: { twoHours: at(0, 17, 20).toISOString(), friday: at(2, 10).toISOString(), six: at(0, 18).toISOString() } };
        out.state = text(mount.querySelector('.sc[data-name="evirma-api"] .scsay'));
        out.header = text(mount.querySelector(".pausechat .talkword, .pausechat .dkword"));
        out.feed = [...mount.querySelectorAll(".pausecard .sentlabel")].map(text);
        // The pause letters of the panel stand as pause cards, not as letters.
        out.letters = mount.querySelectorAll(".pausefeed .mletter").length;
        // A sleeping session holds its context without spending it: its bar
        // is grey, an awake one's keeps the colour of its fill.
        out.edges = { asleep: mount.querySelector('.sc[data-name="evirma-api"] .scedge')?.className || "",
            awake: mount.querySelector('.sc[data-name="evirma-billing"] .scedge')?.className || "" };
        await open();
        out.distantMark = pie().querySelector(".piecrown").getAttribute("data-mark");
        await closePie();
        out.priority = [
            { ...sessions[0], pauseAlarm: pause, status: "busy" }, { ...sessions[0], pauseAlarm: pause, status: "waiting" },
            { ...sessions[0], pauseAlarm: pause, work: { agents: 1 } }, sessions[0], { ...sessions[0], pauseAlarm: pause, restarting: true },
        ].map((s) => [listState(s).tone, headState(s).tone]);
        out.scope = pauseRows(project, profile, sessions, false, sessions[0]).map((s) => s.session);
        out.groupScope = pauseRows(project, profile, sessions, true).map((s) => s.session);
        // Morning stays strictly past +4h, including the early-morning edge.
        out.mornings = [at(0, 4, 50), at(0, 5), at(0, 23)].map((date) => {
            const scale = todayScale(date.getTime());
            return new Date(scale.segments[1].stops.at(-1).value).toISOString();
        });
        out.noEndWords = pauseEnd(null);
        await show();
        const interaction = out.interaction = {};
        const card = mount.querySelector('.sc[data-name="evirma-api"]');
        if (wide) {
            await openPie(card);
            press("pointerdown", sector("restart"), middle("restart"), 1);
        } else {
            press("pointerdown", card, null, 1);
            card.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true }));
            await until(() => pie() && pie().contains(document.activeElement), "the held pie");
        }
        sector("pause").focus();
        await quiet();
        interaction.focusClosed = crownGone();
        if (!interaction.focusClosed) throw new Error("Focusing Pause opened its crown");
        press("pointermove", sector("pause"), middle("pause"), 1);
        await quiet();
        interaction.passClosed = crownGone();
        if (!interaction.passClosed) throw new Error("Passing over Pause opened its crown");
        press("pointerup", sector("pause"), middle("pause"), 1);
        await until(slider, "the pause crown after release");
        interaction.releaseOpened = Boolean(slider());
        sector("pause").click();
        await until(crownGone, "the closed pause crown");
        interaction.repeatClosed = crownGone();
        if (!interaction.repeatClosed) throw new Error("Pressing Pause again kept its crown open");
        hoverSector("pause");
        await quiet();
        interaction.hoverClosed = crownGone();
        if (!interaction.hoverClosed) throw new Error("Hovering Pause opened its crown");
        sector("pause").click();
        await until(slider, "the clicked pause crown");
        interaction.clickOpened = Boolean(slider());
        out.initial = { disabled: apply().disabled, toggle: toggle()?.getAttribute("aria-checked"), words: text(apply()) };
        await choose(.21);
        out.twoHours = { plate: plate(), button: text(apply()) };
        const p4 = crownPoint(.42);
        slider().dispatchEvent(new PointerEvent("pointermove", { bubbles: true, pointerType: "mouse", clientX: p4.x, clientY: p4.y }));
        await quiet();
        out.hover = { plate: plate(), button: text(apply()) };
        if (out.hover.plate !== out.twoHours.plate) throw new Error("Hovering the crown replaced the chosen end's readback");
        hoverSector("restart");
        await quiet();
        interaction.otherPlate = plate();
        if (!interaction.otherPlate.startsWith("Restart the session")) throw new Error("Another sector kept the pause readback instead of its hint");
        // Returning to the crown restores the chosen end, without reading
        // the unpressed pointer's position as another choice.
        slider().dispatchEvent(new PointerEvent("pointermove", { bubbles: true, pointerType: "mouse", clientX: p4.x, clientY: p4.y }));
        await quiet();
        interaction.returnPlate = plate();
        await choose(.21);
        interaction.dialRepeat = choice();
        if (!interaction.dialRepeat.disabled) throw new Error("Clicking the selected hour did not clear it");
        await choose(.21);
        out.sent.alone = await commit();
        out.toast = text(mount.querySelector(".toast.on"));
        mount.querySelector(".toast.on .tact").click();
        await until(() => sent.length === 2, "Undo");
        out.sent.undo = sent.at(-1);
        await open();
        toggle().click();
        await quiet();
        out.together = toggle().getAttribute("aria-checked");
        // Release on the crown selects only. A click ending that drag is
        // consumed even if the release happened over another control.
        const face = slider();
        const point = crownPoint(.21);
        press("pointerdown", sector("pause"), middle("pause"), 3);
        press("pointermove", face, point, 3);
        await until(() => plate().includes("until 17:20"), "the drag readback");
        interaction.livePlate = plate();
        press("pointerup", face, point, 3);
        clickAt(face, point);
        await quiet();
        if (!pie() || sent.length !== 2) throw new Error("Releasing on the crown sent a pause before confirmation");
        out.release = { requests: sent.length, button: text(apply()) };
        press("pointerdown", face, crownPoint(.42), 8);
        press("pointermove", face, point, 8);
        press("pointerup", face, point, 8);
        clickAt(face, point);
        await quiet();
        interaction.dragRepeat = !apply().disabled && slider().getAttribute("aria-valuetext").includes("17:20");
        if (!interaction.dragRepeat) throw new Error("Dragging onto the selected hour cleared it");
        // The bar rejects a release whose down belonged to a sector.
        const button = apply();
        press("pointerdown", sector("pause"), middle("pause"), 4);
        press("pointerup", button, null, 4);
        clickAt(button, { x: 0, y: 0 });
        await quiet();
        if (!pie() || sent.length !== 2) throw new Error("A release from a sector pressed the pause bar");
        const barPoint = { x: button.getBoundingClientRect().left + 10, y: button.getBoundingClientRect().top + 10 };
        press("pointerdown", sector("pause"), middle("pause"), 6);
        press("pointermove", button, barPoint, 6);
        press("pointerup", button, barPoint, 6);
        clickAt(button, barPoint);
        await quiet();
        if (!pie() || sent.length !== 2) throw new Error("A release from a sector pressed the pause bar");
        // Touch capture keeps the original card as the event target even
        // when the finger has travelled over the bar.
        const captured = mount.querySelector('.sc[data-name="evirma-api"]');
        press("pointerdown", sector("pause"), middle("pause"), 7);
        press("pointermove", captured, barPoint, 7);
        press("pointerup", captured, barPoint, 7);
        await quiet();
        if (!pie() || sent.length !== 2) throw new Error("A captured release over the bar selected a sector");
        out.foreignRelease = sent.length;
        out.sent.together = await commit();
        await open();
        await choose(.962);
        await choose(.962);
        interaction.noEndRepeat = choice();
        if (!interaction.noEndRepeat.disabled) throw new Error("Clicking no end again did not clear it");
        await choose(.962);
        out.sent.noEnd = await commit();
        await open();
        await openDays();
        out.days = ring();
        out.dayOff = slider().getAttribute("aria-disabled") === "true";
        hoverSector("day-2");
        sector("day-2").focus();
        await quiet();
        interaction.dayBefore = choice();
        if (!interaction.dayBefore.off || picked().length) throw new Error("Hover or focus selected a day");
        await pickDay(2);
        interaction.dayPicked = choice();
        await choose(3 / 16);
        hoverSector("day-3");
        sector("day-3").focus();
        await quiet();
        interaction.dayHover = choice();
        interaction.dayHint = plate();
        if (picked().join() !== "day-2" || !interaction.dayHover.value.includes("Fri 20 Nov, 10:00")) throw new Error("Hover or focus changed the chosen day or hour");
        await pickDay(3);
        interaction.dayCarry = choice();
        await pickDay(2);
        await pickDay(2);
        interaction.dayRepeat = choice();
        if (!interaction.dayRepeat.off || !interaction.dayRepeat.disabled || picked().length) throw new Error("Clicking the selected day did not clear it and its hour");
        await pickDay(2);
        await choose(3 / 16);
        out.friday = plate();
        out.sent.friday = await commit();
        await open();
        await openDays();
        await pickDay(7);
        await choose((16 - 7) / 16, false);
        out.overLimit = { disabled: apply().disabled, value: slider().getAttribute("aria-valuetext") };
        await choose((15 - 7) / 16);
        out.withinLimit = text(apply());
        await pickDay(2);
        await choose((20 - 7) / 16);
        await pickDay(7);
        interaction.dayLimit = choice();
        await choose((15 - 7) / 16);
        pie().querySelector(".piehub").click();
        await until(() => !sector("day-1"), "the dial after Back");
        out.back = { disabled: apply().disabled, value: slider().getAttribute("aria-valuetext") };
        pie().querySelector(".piecrownzone").focus();
        pie().querySelector(".piecrownzone").click();
        await until(() => sector("day-1") === document.activeElement, "keyboard entry to the days");
        sector("day-2").focus();
        await quiet();
        interaction.keyboardFocusOff = slider().getAttribute("aria-disabled") === "true";
        sector("day-2").click();
        await until(() => slider().getAttribute("aria-disabled") !== "true", "the clicked keyboard day");
        slider().focus();
        slider().dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true }));
        await until(() => slider().getAttribute("aria-valuetext").includes("7:00"), "the day's first keyboard step");
        slider().dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true, cancelable: true }));
        await quiet();
        out.keyboardDay = slider().getAttribute("aria-valuetext");
        await closePie();
        await open("evirma-billing");
        out.noOpened = !toggle();
        await closePie();
        await show(sessions.map((s) => s.session === "evirma-billing" ? { ...s, sleeping: true, pauseAlarm: pause } : s));
        await open("evirma-billing");
        out.sleepingAlone = !toggle();
        out.sent.soloResume = await commit();
        await show(sleeping);
        await open();
        out.sleep = { plate: plate(), mark: pie().querySelector(".piecrown").getAttribute("data-mark"),
            toggle: toggle().getAttribute("aria-checked"), controls: ring() };
        sector("pause").click();
        await until(crownGone, "the closed viewing crown");
        interaction.continueClosed = crownGone();
        if (!interaction.continueClosed) throw new Error("Pressing Continue again kept its crown open");
        sector("pause").click();
        await until(slider, "the continued viewing dial");
        await choose(.21);
        out.sleep.readOnly = slider().getAttribute("aria-valuetext");
        out.sent.resume = await commit();
        await open();
        pie().querySelector(".pausechange").click();
        await until(() => !pie().querySelector(".pausechange"), "the editable end");
        const scale = todayScale(now);
        await choose(scale.fraction(at(0, 18).getTime()));
        out.move = text(apply());
        out.sent.move = await commit();
        await open();
        toggle().click();
        await quiet();
        out.sent.resumeAlone = await commit();
        await show(sessions.map((s, i) => i === 0 ? { ...s, pauseAlarm: { id: "no-end", name: "pause-no-end" }, sleeping: true } : s));
        await open();
        out.indefinite = { mark: pie().querySelector(".piecrown").getAttribute("data-mark"), plate: plate() };
        await closePie();
        // The heading chooses scope on the ring, with no opened toggle.
        const heading = () => mount.querySelector('[aria-label="pause sessions in project evirma"]');
        heading().click();
        await until(slider, "the project dial");
        out.project = { toggle: Boolean(toggle()), sectors: ring() };
        interaction.scopeBefore = picked();
        hoverSector("group");
        sector("group").focus();
        await until(() => plate().startsWith("Pause group backend"), "the group scope hint");
        interaction.scopeHover = picked();
        if (picked().join() !== "project") throw new Error("Hover or focus changed the project scope");
        sector("group").click();
        await until(() => picked().join() === "group", "the clicked group scope");
        interaction.scopeClick = picked();
        hoverSector("project");
        sector("project").focus();
        await until(() => plate().startsWith("Pause project evirma"), "the project scope hint");
        interaction.scopeKept = picked();
        if (picked().join() !== "group") throw new Error("The clicked group scope did not persist");
        await choose(.21);
        out.sent.group = await commit();
        heading().click();
        await until(slider, "the project dial");
        sector("project").click();
        await choose(.21);
        out.sent.project = await commit();
        if (wide) {
            mount.querySelector(".dkproj").dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 50, clientY: 100 }));
            await until(() => mount.querySelector(".ctxmenu"), "the project menu");
            [...mount.querySelectorAll(".ctxmenu .pkrow")].find((b) => text(b.querySelector(".pkname")) === "Pause…").click();
            await until(slider, "the project's menu dial");
            out.projectMenu = true;
            await closePie();
            mount.querySelector(".pausemenuprobe").dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 50, clientY: 150 }));
            await until(() => mount.querySelector(".ctxmenu"), "the session menu");
            [...mount.querySelectorAll(".ctxmenu .pkrow")].find((b) => text(b.querySelector(".pkname")) === "Continue now").click();
            await until(slider, "the session menu's viewing dial");
            out.sessionMenu = slider().getAttribute("aria-disabled") === "true";
            pie().querySelector(".pausechange").click();
            await until(() => !pie().querySelector(".pausechange"), "the menu's editable dial");
            await choose(.21);
            slider().focus();
            slider().dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true }));
            await until(() => text(apply()).includes("17:30"), "the next ten-minute step");
            slider().dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
            await until(() => !pie(), "Enter's pause request");
            await settled();
            out.sent.keyboard = sent.at(-1);
        }
        out.noConfirmation = !mount.querySelector('.sheet.on[aria-label="action confirmation"]');
        // The restart gate remains independent of the pause dial.
        out.restart = [];
        for (let i = 0; i < 3; i += 1) {
            await openPie(mount.querySelector('.sc[data-name="evirma-billing"]'));
            if (i === 0) {
                sector("pause").click();
                await until(slider, "the crown before another action");
            }
            sector("restart").click();
            await until(() => mount.querySelector('.sheet.on[aria-label="action confirmation"]'), "restart choices");
            const gate = mount.querySelector('.sheet.on[aria-label="action confirmation"]');
            const options = [...gate.querySelectorAll('[role="radio"]')];
            if (i === 0) out.options = options.map(text);
            options[i].click();
            await quiet();
            gate.querySelector(".btnrow .btn:last-child").click();
            await until(() => !mount.querySelector('.sheet.on[aria-label="action confirmation"]'), "restart request");
            await settled();
            out.restart.push(sent.at(-1));
        }
        return out;
    } finally {
        render(null, mount);
        mount.remove();
        window.fetch = fetch;
        Date.now = clock;
    }
}
