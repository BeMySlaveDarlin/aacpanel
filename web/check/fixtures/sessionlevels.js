// The same chain is read in both lists: its deepest descendant stays beside
// its parent at the third level, its question survives either fold, and the
// child with nothing under it stands before its sisters with subtrees.
import { h, render } from "preact";
import { html } from "/src/html.js";
import { SessionColumn } from "/src/desktop/sessions.js";
import { ProjectBlock, blocksOf } from "/src/screens/sessions/blocks.js";
import { kinOf } from "/src/screens/sessions/kin.js";
import { GateHost } from "/src/actions/gate.js";
import { ToastHost } from "/src/ui/toasts.js";

export async function sessionLevels() {
    const mount = document.createElement("div");
    document.getElementById("root").append(mount);
    mount.innerHTML = '<div class="phone"></div><div class="deskshell"><div class="dkbody"></div></div>';
    const project = { id: 91, name: "family", session: "A", path: "/srv/family" };
    const profile = { id: 1, profile: "Personal", groups: [{ name: "work", projects: [project] }] };
    const exec = { available: true, kinds: ["session.open", "session.restart", "session.close"] };
    const make = (session, parent, minute, extra = {}) => ({
        session, parent: parent ? { session: parent } : undefined, sessionId: `id-${session}`,
        project, contour: 1, profile: "Personal", cwd: project.path, transport: "stream", status: "idle", pct: 20,
        startedAt: new Date(Date.now() - (100 - minute) * 60e3).toISOString(), ...extra,
    });
    const chain = [make("D", "C", 4, { status: "waiting" }), make("E", "A", 5), make("G", "A", 11),
        make("C", "B", 3), make("A", "", 1), make("B", "A", 2), make("F", "E", 10)];
    const panels = [
        { root: mount.querySelector(".phone"), prefix: "pj", row: ".pjrow.pjcard", name: ".pjsess" },
        { root: mount.querySelector(".dkbody"), prefix: "dk", row: ".dksess.dklive", name: ".dkname" },
    ];
    const text = (el) => el ? el.textContent.replace(/\s+/g, " ").trim() : "";
    const until = async (ok) => {
        for (let i = 0; i < 100; i++) {
            if (ok()) return;
            await new Promise((resolve) => setTimeout(resolve, 30));
        }
        throw new Error("the session tree did not reach its expected state");
    };
    const wrap = (node) => h(ToastHost, null, h(GateHost, null, node));
    let order = [];
    const draw = (list) => {
        const blocks = blocksOf({ profile, sessions: list });
        render(wrap(html`${blocks.map((block) => html`<${ProjectBlock} key=${block.key}
            block=${block} exec=${exec} live=${list} onOpen=${() => {}} />`)}`), panels[0].root);
        render(wrap(h(SessionColumn, {
            snapshot: { at: Date.now() / 1000, sessions: list, profileMap: [profile], limits: null },
            limits: null, current: "", onPick() {}, picks: [], setPicks() {}, onNames() {}, exec, wait: null,
            onOrder(rows) { order = rows.map((s) => s.name); },
        })), panels[1].root);
    };
    const rowOf = (p, name) => Array.from(p.root.querySelectorAll(p.row))
        .find((row) => text(row.querySelector(p.name)) === name);
    const foldOf = (p, name) => rowOf(p, name)?.closest(`.${p.prefix}tree`)
        .nextElementSibling?.querySelector(`.${p.prefix}fold`);
    const rowsOf = (p) => Array.from(p.root.querySelectorAll(`${p.row}, .${p.prefix}fold, .${p.prefix}stub, .${p.prefix}kin`))
        .map((row) => row.matches(`.${p.prefix}fold`) ? `fold: ${text(row.querySelector(`.${p.prefix}foldtext`))}`
            : row.matches(`.${p.prefix}stub`) ? `stub: ${text(row.querySelector(`.${p.prefix}stubname`))}`
                : row.matches(`.${p.prefix}kin`) ? `runs: ${text(row)}` : text(row.querySelector(p.name)));
    const rowsAre = (want) => panels.every((p) => rowsOf(p).join(",") === want);
    const read = (p) => ({
        rows: rowsOf(p),
        waits: Array.from(p.root.querySelectorAll(`.${p.prefix}foldwaits`)).map(text),
        dots: Array.from(p.root.querySelectorAll(`.${p.prefix}folddots`))
            .map((dots) => Array.from(dots.children).map((dot) => dot.dataset.tone)),
    });
    const geometry = (p) => {
        const origin = p.root.getBoundingClientRect().left;
        const rem = parseFloat(getComputedStyle(document.documentElement).fontSize);
        const rows = Array.from(p.root.querySelectorAll(p.row)).map((row) => {
            const box = row.getBoundingClientRect();
            const name = row.querySelector(p.name).getBoundingClientRect();
            const branch = row.matches(`.${p.prefix}branch`);
            const guide = getComputedStyle(row, p.prefix === "pj" ? "::before" : null);
            const guideHeight = p.prefix === "pj" ? guide.height : guide.backgroundSize.split(" ")[1];
            return { name: text(row.querySelector(p.name)), x: name.left - origin, width: name.width,
                top: box.top, bottom: box.bottom, height: box.height,
                guideX: branch ? box.left - origin + parseFloat(p.prefix === "pj" ? guide.left : guide.backgroundPositionX) : 0,
                guideHeight: branch ? guideHeight.endsWith("%") ? box.height : parseFloat(guideHeight) : 0 };
        });
        const trails = Array.from(p.root.querySelectorAll(`.${p.prefix}trail`)).map((tree) => {
            const box = tree.getBoundingClientRect();
            const guide = getComputedStyle(tree, "::before");
            return { x: box.left - origin + parseFloat(guide.left), top: box.top,
                bottom: box.bottom, height: parseFloat(guide.height), line: parseFloat(guide.borderLeftWidth) };
        });
        const folds = Array.from(p.root.querySelectorAll(`.${p.prefix}fold`)).map((fold) => {
            const box = fold.getBoundingClientRect();
            const chevron = fold.firstElementChild.getBoundingClientRect();
            const label = fold.querySelector(`.${p.prefix}foldtext`).getBoundingClientRect();
            return { height: box.height / rem, x: chevron.left - origin,
                centered: Math.abs((label.top + label.bottom - box.top - box.bottom) / 2) < 1.5 };
        });
        return { rows, trails, folds };
    };
    draw(chain);
    const tree = "A,fold: opened 3 sessions,G,B,fold: opened 2 sessions,C,D,E,fold: opened 1 session,F";
    const childClosed = "A,fold: opened 3 sessions,G,B,fold: opened 2 sessions,E,fold: opened 1 session,F";
    await until(() => rowsAre(tree) && order.join(",") === "A,G,B,C,D,E,F");
    const out = { root: kinOf(chain).rootOf(chain[0]).session, phone: {}, desk: {} };
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].open = { ...read(p), ...geometry(p) }; });
    out.order = order.slice();
    for (const p of panels) foldOf(p, "B").click();
    await until(() => rowsAre(childClosed) && order.join(",") === "A,G,B,E,F");
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].childClosed = read(p); });
    out.childOrder = order.slice();
    for (const p of panels) foldOf(p, "A").click();
    await until(() => rowsAre("A,fold: opened 3 sessions") && order.join(",") === "A");
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].rootClosed = read(p); });
    out.kept = JSON.parse(localStorage.getItem("aacpanel.folds") || "[]").filter((name) => name === "A" || name === "B").sort();
    for (const p of panels) foldOf(p, "A").click();
    await until(() => rowsAre(childClosed));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].reopened = read(p); });
    for (const p of panels) foldOf(p, "B").click();
    await until(() => rowsAre(tree));

    const runs = [make("RA", "A", 6, { outside: true }), make("RB", "B", 7, { outside: true }),
        make("RC", "C", 8, { outside: true }), make("RD", "D", 9, { outside: true })];
    draw([...chain, ...runs]);
    await until(() => rowsAre("A,fold: opened 3 sessions,G,B,fold: opened 2 sessions,C,D,runs: 3 runs started by it,"
        + "E,fold: opened 1 session,F,runs: 1 run started by it"));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].runsClosed = read(p); });
    for (const p of panels) {
        for (const fold of p.root.querySelectorAll(`.${p.prefix}kin:not(.${p.prefix}fold)`)) fold.click();
    }
    await until(() => panels.every((p) => rowOf(p, "RA") && rowOf(p, "RD")));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].runsOpen = geometry(p); });
    for (const p of panels) foldOf(p, "A").click();
    await until(() => panels.every((p) => !rowOf(p, "B") && !rowOf(p, "RD") && rowOf(p, "RA")));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].runsHidden = read(p); });
    for (const p of panels) foldOf(p, "A").click();
    await until(() => panels.every((p) => rowOf(p, "B")));

    draw(chain.filter((s) => s.session !== "A"));
    await until(() => rowsAre("stub: A,G,B,fold: opened 2 sessions,C,D,E,fold: opened 1 session,F"));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].stub = { ...read(p), ...geometry(p) }; });
    draw(chain.filter((s) => !["A", "E", "F"].includes(s.session)));
    await until(() => rowsAre("stub: A,G,B,fold: opened 2 sessions,C,D"));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].last = geometry(p); });
    for (const p of panels) render(null, p.root);
    mount.remove();
    return out;
}
