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
    const chain = [make("D", "C", 4, { status: "waiting" }), make("E", "A", 5), make("G", "A", 11, { cap: 70, autoRestart: false }),
        make("C", "B", 3, { cap: 65, autoRestart: true, pct: 82 }),
        make("A", "", 1, { cap: 80, autoRestart: true, checklist: { items: [{ text: "Review the branch", status: "active" }] } }),
        make("B", "A", 2, { cap: 70, autoRestart: true, remote: true,
            checklist: { items: [{ text: "Read the brief", status: "done" }, { text: "Check the layout", status: "active" }] } }),
        make("F", "E", 10, { cap: 100, autoRestart: true })];
    const panels = [
        { root: mount.querySelector(".phone"), desk: false },
        { root: mount.querySelector(".dkbody"), desk: true },
    ];
    const text = (el) => el ? el.textContent.replace(/\s+/g, " ").trim() : "";
    const until = async (ok) => {
        const end = performance.now() + 3000;
        while (!ok() && performance.now() < end) await new Promise(requestAnimationFrame);
        if (!ok()) throw new Error("the session tree did not reach its expected state");
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
    const CARD = ".sc:not(.scpast):not(.scghost)";
    const rowOf = (p, name) => p.root.querySelector(`.sc[data-name="${name}"]`);
    const foldOf = (p, name) => rowOf(p, name)?.closest(".pjtree").nextElementSibling?.querySelector(".scfold");
    const runsFold = (row) => text(row.querySelector(".scfoldtext")).endsWith("started by it");
    const rowsOf = (p) => Array.from(p.root.querySelectorAll(`${CARD}, .scfold, .scstub`))
        .map((row) => row.matches(".scfold") ? `${runsFold(row) ? "runs" : "fold"}: ${text(row.querySelector(".scfoldtext"))}`
            : row.matches(".scstub") ? `stub: ${text(row.querySelector(".scstubname"))}` : row.dataset.name);
    const rowsAre = (want) => panels.every((p) => rowsOf(p).join(",") === want);
    const read = (p) => ({
        rows: rowsOf(p),
        waits: Array.from(p.root.querySelectorAll(".scfoldwaits")).map(text),
    });
    const depthOf = (el) => Number(el.style.getPropertyValue("--depth") || 0);
    // levels reads the cards of a list by the level each stands at, with what
    // a card says of its cap and its restart, and the folds by their levels.
    const levels = (p) => ({
        rows: Array.from(p.root.querySelectorAll(CARD)).map((row) => {
            const cap = row.querySelector(".scedge .sccap");
            const mark = row.querySelector(".scl5 .scb:nth-child(3)");
            return { name: row.dataset.name, depth: depthOf(row),
                cap: Boolean(cap), capAuto: Boolean(cap && cap.classList.contains("scauto")),
                restart: text(mark), restartIcon: Boolean(mark?.querySelector("svg")), restartTip: mark ? mark.getAttribute("title") || "" : "" };
        }),
        folds: Array.from(p.root.querySelectorAll(".scfold")).filter((fold) => !runsFold(fold)).map(depthOf),
    });
    draw(chain);
    const tree = "A,fold: 3 opened,G,B,fold: 2 opened,C,D,E,fold: 1 opened,F";
    const childClosed = "A,fold: 3 opened,G,B,fold: 2 opened,E,fold: 1 opened,F";
    await until(() => rowsAre(tree) && order.join(",") === "A,G,B,C,D,E,F");
    const out = { root: kinOf(chain).rootOf(chain[0]).session, phone: {}, desk: {} };
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].open = { ...read(p), ...levels(p) }; });
    out.order = order.slice();
    for (const p of panels) foldOf(p, "B").click();
    await until(() => rowsAre(childClosed) && order.join(",") === "A,G,B,E,F");
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].childClosed = read(p); });
    out.childOrder = order.slice();
    for (const p of panels) foldOf(p, "A").click();
    await until(() => rowsAre("A,fold: 3 opened") && order.join(",") === "A");
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
    await until(() => rowsAre("A,fold: 3 opened,G,B,fold: 2 opened,C,D,runs: 3 runs started by it,"
        + "E,fold: 1 opened,F,runs: 1 run started by it"));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].runsClosed = read(p); });
    for (const p of panels) {
        for (const fold of p.root.querySelectorAll(".scfold")) if (runsFold(fold)) fold.click();
    }
    await until(() => panels.every((p) => rowOf(p, "RA") && rowOf(p, "RD")));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].runsOpen = levels(p); });
    for (const p of panels) foldOf(p, "A").click();
    await until(() => panels.every((p) => !rowOf(p, "B") && !rowOf(p, "RD") && rowOf(p, "RA")));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].runsHidden = read(p); });
    for (const p of panels) foldOf(p, "A").click();
    await until(() => panels.every((p) => rowOf(p, "B")));

    draw(chain.filter((s) => s.session !== "A"));
    await until(() => rowsAre("stub: A,G,B,fold: 2 opened,C,D,E,fold: 1 opened,F"));
    panels.forEach((p, i) => { out[i ? "desk" : "phone"].stub = { ...read(p), ...levels(p) }; });
    draw(chain.filter((s) => !["A", "E", "F"].includes(s.session)));
    await until(() => rowsAre("stub: A,G,B,fold: 2 opened,C,D"));
    for (const p of panels) render(null, p.root);
    mount.remove();
    return out;
}
