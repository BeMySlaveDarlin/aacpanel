import { h } from "preact";
import { useEffect } from "preact/hooks";
import { FilesButton, RepoView, useRepo } from "/src/screens/repo/view.js";
import { useBackClose } from "/src/ui/back.js";

export { counted, quiet } from "./quiet.js";
export const sel = (q) => document.querySelector(q);
export const all = (q) => [...document.querySelectorAll(q)];
export const text = (el) => el ? el.textContent.replace(/\s+/g, " ").trim() : "";
export async function until(ok, label = "the repository state") {
    const end = performance.now() + 3000;
    while (performance.now() < end) {
        const found = typeof ok === "string" ? sel(ok) : ok();
        if (found) return found;
        await new Promise(requestAnimationFrame);
    }
    throw new Error(`Did not reach ${label}`);
}
export async function showChanged() {
    (await until('.cdlisttab[data-tab="changed"]', "the Changed tab")).click();
    return until(".cdchangedfile", "a changed file");
}
export async function showFiles() {
    (await until('.cdlisttab[data-tab="files"]', "the All files tab")).click();
}
export function RepoScene({ cwd, name, onBack = () => {}, onSend, onConversation, autoOpen = true }) {
    useBackClose(true, onBack);
    const repo = useRepo({ cwd, name });
    useEffect(() => { repo.setOpen(autoOpen); }, []);
    return h("div", { style: "display:flex;flex:1;min-height:0;flex-direction:column" },
        h("header", { class: "phead" }, h("h2", null, name), h(FilesButton, { repo })),
        h(RepoView, { repo, onSend, onConversation, feed: h("div", { class: "chatfeed" }, "Conversation feed") },
            h("div", { class: "composerbox" }, "Conversation composer")));
}
