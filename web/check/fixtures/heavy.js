const now = Math.floor(Date.now() / 1000);
export const heavyQueue = {
    slots: 3,
    running: [
        { unit: "aacp-heavy-1.scope", name: "full check", session: "lab", dir: "/srv/proj/service", ask: 2 * 1024 ** 3, taken: 512 * 1024 ** 2, since: now - 600 },
        { unit: "aacp-heavy-2.scope", name: "", session: "", dir: "", ask: 0, taken: 0 },
    ],
    waiting: [
        { place: 1, name: "build", session: "other", dir: "/srv/proj/client", ask: 1024 ** 3, since: now - 300 },
        { place: 2, name: "terminal check", session: "", dir: "/srv/tools", ask: 256 * 1024 ** 2, since: now - 120 },
    ],
    head: "memory pressure (full avg10 12, below 10 needed)",
};

export const emptyHeavyQueue = { slots: 3, running: [], waiting: [], head: "" };

export function readHeavy(root = document) {
    const section = root.querySelector('[aria-label="Heavy runs"]');
    const text = (el) => el?.textContent.replace(/\s+/g, " ").trim() || "";
    return {
        text: text(section),
        running: [...(section?.querySelectorAll('[data-heavy="running"]') || [])].map(text),
        waiting: [...(section?.querySelectorAll('[data-heavy="waiting"]') || [])].map(text),
        head: text(section?.querySelector('[data-heavy="head"]')),
        controls: section?.querySelectorAll("button").length || 0,
    };
}
