import { h, render } from "preact";
import { html } from "/src/html.js";
import { Chat } from "/src/screens/chat.js";
import { MenuHost } from "/src/desktop/menu.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";
import { counted, quiet } from "/check/fixtures/quiet.js";
import { press, tap, until } from "/check/fixtures/pie.js";

export async function callPlaces(root) {
    const started = performance.now();
    const at = new Date().toISOString();
    const heading = "> [Bash · make check · failed · use u40 · pos 40.2]";
    const output = `discard\n\nfirst\n${"x".repeat(170)}\nlast\n\n`;
    const goodArg = "read\n " + "y".repeat(90);
    const own = "Explain this\nKeep this line\nAnd this one";
    const calls = [
        { name: "Bash", arg: "make check", use: "u40", pos: 40, index: 2, at, failed: true },
        { name: "Read", arg: goodArg, use: "u41", pos: 41, index: 0, at },
        { name: "Read", arg: "", use: "u42", pos: 42, index: 0, at },
        { name: "Read", arg: "", use: "", pos: 43, index: 0, at },
    ];
    const items = [
        { role: "me", pos: 10, at, text: `${heading}\n> history\n\n${own}` },
        { role: "me", pos: 20, at, text: `${heading} trailing\n\nLeave this\nas words` },
        { role: "me", pos: 21, at, text: `An example\n${heading}` },
        { role: "ai", pos: 30, at, text: "Here is the file", files: [
            { path: "/srv/proj/notes.txt", name: "notes.txt" },
            { path: "/srv/proj/private.txt", name: "private.txt", outside: true },
        ] },
        { role: "tools", kind: "bash", run: 40, pos: 40, at, calls },
        { role: "ai", pos: 50, at, text: "The calls are over" },
    ];
    const requested = [], sent = [], copied = [], files = [];
    const json = (data, status = 200) => new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
    let refuse = false;
    window.fetch = counted(async (url, init) => {
        const u = new URL(String(url), location.href);
        if (u.pathname === "/api/chat") return json({ items, first: 10, last: 50, total: items.length, moreBefore: false, state: {} });
        if (u.pathname === "/api/chat/call") {
            requested.push(Object.fromEntries(u.searchParams));
            if (refuse) { refuse = false; return json({ error: "call unavailable" }, 503); }
            return json({ tool: "Bash", args: "{}", result: u.searchParams.get("pos") === "40" ? output : "", failed: u.searchParams.get("pos") === "40" });
        }
        if (u.pathname === "/api/chat/file") {
            files.push(Object.fromEntries(u.searchParams));
            return json({ name: "notes.txt", text: "file contents", size: 13, next: 0 });
        }
        if (u.pathname === "/api/actions") { sent.push(JSON.parse(init.body)); return json({ logged: true }); }
        if (u.pathname === "/api/term") return json({ available: false });
        if (u.pathname === "/api/acts") return json({ acts: [] });
        return json({});
    });
    window.EventSource = class { constructor() { this.readyState = 1; } addEventListener() {} close() {} };
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (text) => copied.push(text) } });
    const exec = { available: true, kinds: ["session.send", "session.stop"] };
    const live = { status: "busy", turnOver: true, transport: "stream", sessionId: "s1", cwd: "/srv/proj", pct: 12 };
    render(h(ToastHost, null, h(GateHost, null, h(MenuHost, null,
        html`<${Chat} name="lab" id="s1" live=${live} exec=${exec} onBack=${() => {}} onUsage=${() => {}} />`))), root);
    const one = (sel) => root.querySelector(sel);
    const all = (sel) => [...root.querySelectorAll(sel)];
    const words = (el) => el?.textContent.replace(/\s+/g, " ").trim() || "";
    const menu = () => one(".ctxmenu");
    const sheet = () => one('.sheet.on[aria-label="tool calls"]');
    const area = () => one(".composer textarea");
    const chip = () => one(".placedraft");
    const depth = () => history.state?.depth || 0;
    const write = async (text) => {
        area().value = text;
        area().dispatchEvent(new Event("input", { bubbles: true }));
        await until(() => text || chip() ? one(".composer .sendbtn") && !one(".composer .sendbtn").disabled
            : !one(".composer .sendbtn"), "the composer's send state");
    };
    const choose = async (label) => {
        const item = [...menu().querySelectorAll('[role="menuitem"]')].find((el) => words(el.querySelector(".pkname")) === label);
        if (!item) throw new Error(`No item ${label}`);
        item.click();
        await until(() => !menu(), "the menu to close");
    };
    const openMenu = async (el, timer = false) => {
        // The feed follows a resized composer; moving the source row closes
        // its menu, so only a menu opened from the feed waits for that motion.
        if (el.closest(".chatfeed")) await quiet();
        if (innerWidth >= 1100) {
            el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, button: 2 }));
        } else {
            press("pointerdown", el);
            if (!timer) el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true }));
        }
        await until(menu, "the held object's menu");
        if (innerWidth < 1100) {
            press("pointerup", el);
            el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, detail: 1 }));
        }
    };
    const openCalls = async () => {
        const badge = one(".railbadge, .tlentry");
        if (!badge) throw new Error("The feed has no call badge");
        badge.click();
        await until(() => sheet()?.querySelector(".callnode") && depth() === 2, "the calls list");
    };
    const shutSheet = async () => {
        sheet().querySelector(".sheetclose").click();
        await until(() => !sheet() && depth() === 1, "the calls sheet to close");
    };
    const send = async () => {
        const before = sent.length;
        one(".composer .sendbtn").click();
        await until(() => sent.length > before && !chip() && one(".chatfeed .msg.me.queued") && !area().disabled,
            "the message to go out");
        return sent.at(-1);
    };
    await until(() => area() && one(".chatfeed .placecard") && one(".railbadge, .tlentry"), "the feed and composer");
    const out = { cards: all(".chatfeed .placecard").length,
        placedBreaks: all(".chatfeed .msg.me")[0].querySelectorAll("p br").length,
        plainBreaks: all(".chatfeed .msg.me")[1].querySelectorAll("p br").length,
        malformed: all(".chatfeed .msg.me").filter((el) => !el.querySelector(".placecard")).map(words) };
    const outside = one(".mfile.outside");
    tap(outside);
    const outsideMenu = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    outside.dispatchEvent(outsideMenu);
    await quiet();
    out.outsideFile = outside.tagName === "DIV" && (!menu()
        || ![...menu().querySelectorAll(".pkname")].some((el) => words(el) === "Open"));
    if (menu()) {
        window.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "Escape" }));
        await until(() => !menu(), "the enclosing message's menu to close");
    }
    if (innerWidth >= 1100) {
        await openMenu(one(".chatfeed .msg.me"));
        await choose("Edit");
        await until(chip, "the edited place");
        out.edited = words(chip().querySelector(".placehead")) === "Bash · make check · failed"
            && chip().querySelector("pre")?.textContent === "history" && area().value === own;
        chip().querySelector(".clipoff").click();
        await write("");
    } else {
        const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
        one(".chatfeed .msg.me").dispatchEvent(event);
        out.native = !event.defaultPrevented && !menu();
    }
    one(".chatfeed .placecard").click();
    await until(() => requested.length > 0 && sheet()?.querySelector(".callpre"), "the historical call");
    out.history = requested.at(-1);
    await shutSheet();
    out.outsideFile = out.outsideFile && files.length === 0;

    await openCalls();
    const row = sheet().querySelector(".callnode");
    await openMenu(row, true);
    out.menu = [...menu().querySelectorAll(".pkname")].map(words);
    out.holdOnlyMenu = !sheet().querySelector(".callbody");
    // An outside tap dismisses the menu without taking the list with it.
    area().dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0 }));
    await until(() => !menu(), "the outside tap");
    out.outside = Boolean(sheet()?.querySelector(".callnode"));
    await openMenu(row);
    if (innerWidth < 1100) history.back();
    else window.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "Escape" }));
    await until(() => !menu(), "back from the menu");
    out.back = Boolean(sheet()?.querySelector(".callnode"));
    await quiet();
    await openMenu(row);
    await choose("Copy the output");
    await until(() => copied.length > 0, "the copied output");
    out.copied = copied.at(-1);
    out.copyRequest = requested.at(-1);
    tap(row);
    await until(() => sheet()?.querySelector(".callbody .callpre"), "the call opened by a tap");
    out.tapRequest = requested.at(-1);
    sheet().querySelector(".pback").click();
    await until(() => sheet()?.querySelector(".callnode"), "back to the list");
    await openMenu(sheet().querySelector(".callnode"));
    await choose("Open");
    await until(() => sheet()?.querySelector(".callbody .callpre"), "the call opened by the menu");
    out.openRequest = requested.at(-1);
    sheet().querySelector(".pback").click();
    await until(() => sheet()?.querySelector(".callnode"), "the list again");

    await write("Why?");
    await openMenu(sheet().querySelector(".callnode"));
    await choose("Ask about this");
    await until(() => chip() && !sheet() && document.activeElement === area(), "the place in the focused composer");
    out.draft = { head: words(chip().querySelector(".placehead")), excerpt: chip().querySelector("pre")?.textContent,
        text: area().value, focused: document.activeElement === area(), count: all(".placedraft").length };
    out.send = await send();
    out.local = all(".chatfeed .msg.me").filter((el) => el.querySelector(".placecard")).map(words).at(-1);
    out.localQueued = Boolean(one(".chatfeed .msg.me.queued .placecard"));

    const file = one(".chatfeed .mfile");
    await openMenu(file);
    out.fileMenu = [...menu().querySelectorAll(".pkname")].map(words);
    await choose("Ask about this");
    await until(chip, "the file place");
    out.fileDraft = words(chip().querySelector(".placehead"));
    out.fileSend = await send();
    all(".chatfeed .placecard").at(-1).click();
    await until(() => files.length > 0 && one('.sheet.on[aria-label="project file"] [data-file] .callpre'), "the quoted file");
    out.fileRequest = files.at(-1);
    one('.sheet.on[aria-label="project file"] .sheetclose').click();
    await until(() => !one('.sheet.on[aria-label="project file"]') && depth() === 1, "the file viewer to close");

    await openCalls();
    await openMenu(sheet().querySelectorAll(".callnode")[1]);
    await choose("Ask about this");
    await until(chip, "the successful call");
    out.successHead = words(chip().querySelector(".placehead"));
    // A second place replaces the first while the words stay in the field.
    await openMenu(file);
    await choose("Ask about this");
    await until(() => chip()?.textContent.includes("notes.txt"), "the replacement place");
    out.replaced = all(".placedraft").length === 1 && words(chip().querySelector(".placehead")) === "notes.txt";
    chip().querySelector(".clipoff").click();
    await until(() => !chip(), "the removed place");
    out.removed = !chip();

    await openCalls();
    await openMenu(sheet().querySelectorAll(".callnode")[1]);
    await choose("Ask about this");
    await until(chip, "a place without output");
    out.successSend = await send();
    await openCalls();
    await openMenu(sheet().querySelectorAll(".callnode")[2]);
    out.emptyEnabled = menu().querySelector('[role="menuitem"]').getAttribute("aria-disabled") !== "true";
    await choose("Ask about this");
    await until(chip, "the call without an argument");
    out.emptyDraft = words(chip().querySelector(".placehead"));
    out.emptySend = await send();
    const emptyCard = all(".chatfeed .placecard").at(-1);
    out.emptyCard = words(emptyCard.querySelector(".placehead"));
    emptyCard.click();
    await until(() => requested.at(-1)?.pos === "42" && sheet()?.querySelector(".callpre"), "the call without an argument to open");
    out.emptyRequest = requested.at(-1);
    await shutSheet();
    await openCalls();
    await openMenu(sheet().querySelectorAll(".callnode")[3]);
    out.noUseDisabled = menu().querySelector('[role="menuitem"]').getAttribute("aria-disabled") === "true";
    window.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "Escape" }));
    await until(() => !menu(), "the unaddressed call's menu to close");
    await shutSheet();
    await openCalls();
    refuse = true;
    await openMenu(sheet().querySelector(".callnode"));
    await choose("Ask about this");
    await until(() => root.textContent.includes("call unavailable"), "the refused call");
    out.refused = !chip() && root.textContent.includes("call unavailable");
    await shutSheet();
    render(null, root);
    out.elapsed = performance.now() - started;
    return out;
}
