// The foreign rows share a walk on the phone and desktop fixtures, so adding
// a server kind costs no extra Chrome page. The host only supplies rows and
// records actions; navigation and confirmation run through the real shells.
export async function foreignTerms({ reset, asked, streams, until, pause, wide }) {
    const now = Math.floor(Date.now() / 1000);
    const rows = [
        { id: "t-12345678", name: "build shell", command: "bash", last: "u@helios shop $" },
        { id: "tm-" + "a".repeat(64), name: "ssh shell", command: "bash", foreign: true, server: "default" },
        { id: "tm-" + "b".repeat(64), name: "logs", command: "bash", foreign: true, server: "ops" },
        { id: "tm-" + "c".repeat(64), name: "work pane", command: "claude", foreign: true,
            server: "default", busy: true, session: "shop", sessionId: "s1" },
    ].map((t, i) => ({ ...t, place: "/srv/proj/shop", label: "shop", activity: now - (i + 1) * 60, created: now - (4 - i) * 600, clients: 0 }));
    reset(rows);
    const all = (sel) => Array.from(document.querySelectorAll(sel));
    const text = (el) => el ? el.textContent.replace(/\s+/g, " ").trim() : "";
    const card = (name) => all(".tcardcell").find((c) => text(c.querySelector(".tcardname")) === name);
    await until(() => all(".tcard").length === 4);
    const out = { rows: all(".tcard").map((c) => ({
        name: text(c.querySelector(".tcardname")), mark: text(c.querySelector(".tcardkind")),
        foreign: c.classList.contains("foreign"),
    })) };
    await pause("F");
    const termStreams = () => streams.filter((s) => s.url.startsWith("/api/term/stream"));
    card("logs").querySelector(".tcard").click();
    await until(() => termStreams().some((s) => s.url.includes(rows[2].id)));
    out.stream = termStreams().at(-1).url.split("&")[0];
    out.newTab = Boolean(document.querySelector(".tadd"));
    if (wide) {
        out.window = all(".dkterm .btn").some((b) => text(b) === "Open in a window");
    } else {
        document.querySelector(".pmore").click();
        await until(() => document.querySelector(".sheet.on .tools"));
        out.menu = all(".sheet.on .toollabel").map(text);
        // Close tab opens the same confirmation as the card's ×.
        all(".sheet.on .toolrow").find((b) => text(b).startsWith("Close tab")).click();
        await until(() => document.querySelector(".sheet.on .tclose"));
        all(".sheet.on .btn").find((b) => text(b) === "Cancel").click();
        await until(() => !document.querySelector(".sheet.on"));
    }
    document.querySelector(wide ? ".dkterm .tback" : ".pback").click();
    await until(() => card("logs"));
    const before = asked.length;
    card("logs").querySelector(".tcardx").click();
    await until(() => document.querySelector(".sheet.on .tclose"));
    out.confirmed = text(document.querySelector(".sheet.on .stitle"));
    out.closedBefore = asked.length - before;
    all(".sheet.on .btn").find((b) => text(b) === "Close tab").click();
    await until(() => !card("logs") && !document.querySelector(".sheet.on"));
    out.closed = asked[before];
    const claudeBefore = asked.length;
    card("work pane").querySelector(".tcardx").click();
    await until(() => text(document.querySelector(".sheet.on .stitle")) === "Close session shop?");
    out.claudeConfirmed = text(document.querySelector(".sheet.on .stitle"));
    out.claudeClosedBefore = asked.length - claudeBefore;
    all(".sheet.on .btn").find((b) => text(b) === "Close gently").click();
    await until(() => asked.length > claudeBefore && !document.querySelector(".sheet.on"));
    out.claudeClose = asked[claudeBefore];
    const rawBefore = termStreams().filter((s) => s.url.includes(rows[3].id)).length;
    card("work pane").querySelector(".tcard").click();
    await until(() => all(".midname").some((h) => h.title === "shop") && !document.querySelector(".tcard"));
    out.sessionScreen = all(".midname").some((h) => h.title === "shop") && !document.querySelector(".tcard");
    out.rawClaude = termStreams().filter((s) => s.url.includes(rows[3].id)).length - rawBefore;
    return out;
}
