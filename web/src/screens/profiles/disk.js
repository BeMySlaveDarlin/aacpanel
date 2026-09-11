// Picking a project from disk: directories that are not on the map yet.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { plural } from "../../format.js";
import { folderOf } from "./pick.js";

// grouped returns the directories laid out by folder, in walk order.
export function grouped(dirs, roots) {
    const out = new Map();
    for (const dir of dirs || []) {
        const { folder, rel } = folderOf(dir.path, roots);
        if (!out.has(folder)) out.set(folder, []);
        out.get(folder).push({ ...dir, rel });
    }
    return [...out.entries()].map(([folder, items]) => ({ folder, items }));
}

// openFolders returns which folders are open from the start.
export function openFolders(group, profile, roots) {
    const out = new Set();
    for (const project of (group && group.projects) || []) {
        out.add(folderOf(project.path, roots).folder);
    }
    if (out.size === 0 && profile && profile.prefix) {
        const { folder } = folderOf(profile.prefix.replace(/\/+$/, ""), roots);
        if (folder) out.add(folder);
    }
    return out;
}

// diskNote returns what to say next to the button about the directory list.
export function diskNote(disk) {
    if (!disk || disk.state !== "ok") return "the agent did not give a directory list — type the path by hand";
    const n = (disk.dirs || []).length;
    if (n === 0) return "there are no directories on disk beyond the map";
    return `${n} ${plural(n, "directory", "directories")} beyond the map`;
}

// marks returns what the directory was recognised by.
export function marks(dir) {
    const out = [];
    if (dir.git) out.push("git");
    if (dir.claude) out.push("claude was run here");
    return out.join(" · ");
}

export function DiskPicker({ disk, group, profile, onPick }) {
    const [open, setOpen] = useState(() => openFolders(group, profile, disk.roots));
    const sections = grouped(disk.dirs, disk.roots);
    const toggle = (folder) => setOpen((prev) => {
        const next = new Set(prev);
        if (next.has(folder)) next.delete(folder);
        else next.add(folder);
        return next;
    });

    return html`
        <div class="pfdisk">
            ${sections.map(({ folder, items }) => html`
                <div class="pfdiskfolder" key=${folder} data-open=${open.has(folder) ? "1" : "0"}>
                    <button class="pfdiskhead" type="button" onClick=${() => toggle(folder)}>
                        <span class="pfdiskname">${folder}</span>
                        <span class="count">
                            ${items.length} ${plural(items.length, "directory", "directories")}
                        </span>
                    </button>
                    ${open.has(folder) && items.map((dir) => html`
                        <button class="pfdiskitem" type="button" key=${dir.path} onClick=${() => onPick(dir)}>
                            <span class="pfdiskrel">${dir.rel}</span>
                            ${marks(dir) && html`<span class="pfdiskmeta">${marks(dir)}</span>`}
                        </button>
                    `)}
                </div>
            `)}
        </div>
    `;
}
