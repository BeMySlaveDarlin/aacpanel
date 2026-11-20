// The command list and its ring share the conversation already on the page.
import { closePie, middle, pie, plate, press, sector, settled, tap, underPies, until } from "/check/fixtures/pie.js";

const chip = () => document.querySelector(".decktools .pkcmds");
const frame = () => new Promise((r) => requestAnimationFrame(r));
const assert = (ok, why) => { if (!ok) throw new Error(why); };
const ready = () => until(() => sector("quick-0")?.getAttribute("aria-disabled") !== "true" && sector("quick-0"), "the quick phrases");
const open = async (keys = false) => {
    await settled();
    if (keys) chip().click(); else tap(chip());
    await ready();
};
// A contextmenu during the held press fires the same hold immediately. One
// press below also waits for its timer, so the fixture covers both entrances.
const hold = async (timer = false) => {
    await settled();
    const start = press("pointerdown", chip());
    if (!timer) chip().dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true }));
    await ready();
    return start;
};
const release = async (key) => {
    const at = middle(key);
    press("pointermove", chip(), at);
    await frame();
    press("pointerup", chip(), at);
};
const letters = () => [...document.querySelectorAll(".piemenu .piesec")].map((el) => el.textContent.trim());
const noChoice = () => !pie().querySelector(".pieon");
const confirm = async () => {
    await until(() => document.querySelector(".sheet.on .btnrow .btn:last-child"), "the restart gate");
    document.querySelector(".sheet.on .btnrow .btn:last-child").click();
    await until(() => !document.querySelector(".sheet.on"), "the gate to close");
};

export async function quickCommands({ show, live, sent, messages }) {
    const out = {};
    const started = performance.now();
    try {
        show(live);
        await until(() => chip() && document.querySelector("textarea"), "the composer");
        await underPies(1);
        const area = document.querySelector("textarea");
        area.value = "Keep this draft";
        area.dispatchEvent(new Event("input", { bubbles: true }));
        await frame();
        await open();
        out.initial = { selected: !noChoice(), phrases: letters(), plate: plate() };
        assert(pie().contains(document.activeElement), "the menu did not receive focus");
        tap(sector("quick-1"));
        await settled();
        await until(() => sent.length === 1, "the tapped phrase");
        out.tap = sent[0];
        out.draft = document.querySelector("textarea").value;
        await until(() => document.querySelector(".msg.me")?.textContent.includes("Go on"), "the sent phrase in the feed");

        const start = await hold(innerWidth < 1100);
        assert(noChoice(), "the held opener selected a sector");
        press("pointermove", chip(), middle("quick-0"));
        await frame();
        press("pointerup", chip(), start);
        await frame();
        assert(noChoice() && sent.length === 1, "releasing at the opener did something");
        await closePie();
        await hold();
        await release("quick-2");
        await settled();
        await until(() => sent.length === 2, "the released phrase");
        out.drag = sent[1];

        out.tapOnly = {};
        for (const key of ["restart", "report"]) {
            const before = sent.length;
            await hold();
            await release(key);
            await frame();
            assert(pie() && sent.length === before && !document.querySelector(".sheet.on"), `${key} ran on release`);
            out.tapOnly[key] = plate();
            tap(sector(key));
            if (key === "restart") await confirm();
            await settled();
            await until(() => sent.length === before + 1, `the tapped ${key}`);
            out[key] = sent[before];
        }

        await open();
        tap(sector("pause"));
        await until(() => document.querySelector(".piecrownface"), "the pause dial");
        assert(!document.querySelector(".pausetoggle"), "the pause includes opened sessions");
        const dial = document.querySelector(".piecrownface");
        const now = [...document.querySelectorAll(".piecrownword")].find((el) => el.textContent === "now").getBoundingClientRect();
        const near = { x: now.left + now.width / 2, y: now.top + now.height / 2 };
        press("pointerdown", dial, near, 2);
        press("pointerup", dial, near, 2);
        dial.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, detail: 1, clientX: near.x, clientY: near.y }));
        await until(() => !document.querySelector(".pauseapply").disabled, "a selected pause end");
        document.querySelector(".pauseapply").click();
        await settled();
        await until(() => sent.some((s) => s.kind === "project.pause"), "the pause request");
        out.pause = sent.find((s) => s.kind === "project.pause");

        await open(true);
        out.keyboard = document.activeElement.dataset.key;
        tap(sector("commands"));
        await until(() => document.querySelector(".sheet.on .cmdhead"), "all commands");
        out.all = document.querySelector(".sheet.on .cmdhead").textContent;
        document.querySelector(".sheet.on .sheetclose").click();
        await settled();

        messages.quick = "First custom\nSecond custom\nThird custom\nFourth custom\nFifth custom";
        messages.inherited = "Inherited one\nInherited two\nInherited three";
        await open();
        out.five = letters();
        tap(sector("quick-4"));
        await settled();
        await until(() => sent.at(-1)?.params?.text === "Fifth custom", "the custom phrase");
        out.custom = sent.at(-1);
        messages.quick = "First custom\nSecond custom\nThird custom";
        await open();
        out.three = letters();
        await closePie();
        messages.quick = "";
        await open();
        out.inherited = letters();
        await closePie();

        show({ ...live, agent: "codex", title: "A Codex session" });
        await until(() => document.querySelector('[data-pick="think"]'), "the Codex composer");
        await open();
        tap(sector("commands"));
        await until(() => document.querySelector(".sheet.on .cmdhead"), "Codex all commands");
        out.codex = document.querySelector(".sheet.on .cmdhead").textContent;
    } catch (err) {
        out.error = String(err.stack || err);
    }
    out.elapsed = (performance.now() - started) / 1000;
    return out;
}
