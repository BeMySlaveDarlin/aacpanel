// The repository as a phone reads it: a page opened from the head of a
// conversation, walked as a stack — the run, its changes, one file — and left
// one step at a time.
//
// A stack rather than strips: the viewer is opened from a conversation and has
// to give it back, and a strip that scrolls sideways under a diff that also
// scrolls sideways leaves nothing to swipe the page by.

import { useCallback, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Icon } from "../../ui/icons.js";
import { changesOf, diffOf, fileOf, treeOf, useAsk } from "./data.js";
import { FileLines, Hunk } from "./lines.js";

// How much of a file a window carries. The number is the one the service and
// the agent hold too: three places counting differently is a screen that asks
// for two hundred lines and draws a hundred.
const WINDOW = 200;

export function RepoView({ cwd, name, onBack }) {
    // One screen, one head, one claim on the back gesture — whatever is drawn
    // inside it. Two pages side by side, each with a head of its own, is a
    // shape that can stack: any redraw that fails to match them up leaves the
    // old one standing, and the way back then belongs to a screen nobody sees.
    // The stack lives here as data instead.
    const [stack, setStack] = useState([{ kind: "changes" }]);
    const top = stack[stack.length - 1];
    const push = useCallback((page) => setStack((was) => [...was, page]), []);

    const back = useCallback(() => {
        setStack((was) => {
            if (was.length > 1) return was.slice(0, -1);
            onBack();
            return was;
        });
    }, [onBack]);
    useBackClose(true, back);

    // One base for the whole stack: a file opened from a list measured against
    // one branch and then measured against another is two answers to one
    // question.
    const [base, setBase] = useState("");
    const [wrap, setWrap] = useState(false);
    const [tab, setTab] = useState("feed");

    const [changes] = useAsk(() => changesOf(cwd, base), [cwd, base]);
    const data = changes.kind === "ready" ? changes.data : null;
    const file = top.kind === "file" ? top.path : "";

    return html`
        <${BackHead} onBack=${back} label=${file ? "to the changes" : "to the conversation"}
                     tools=${html`<${WrapButton} on=${wrap} onClick=${() => setWrap((w) => !w)} />`}>
            <div class="chathead">
                <h2>${file ? file.split("/").pop() : (data && !data.noRepo ? data.branch : name)}</h2>
                <div class="chatsub">
                    ${file
                        ? html`<span>${file.split("/").slice(0, -1).join("/") || "/"}</span>`
                        : data && !data.noRepo
                        ? html`<span class="cdbase" title=${`the base comes from the ${data.baseFrom}`}>
                                 against <b>${data.base || "nothing"}</b>
                               </span>
                               <span>${data.total} ${data.total === 1 ? "file" : "files"}</span>`
                        : html`<span>${cwd}</span>`}
                </div>
            </div>
        <//>
        <div class=${`cdpage${wrap ? " wrap" : ""}`}>
            ${file
                ? html`<${FileBody} cwd=${cwd} path=${file} base=${base} />`
                : html`<${ChangesBody}
                           cwd=${cwd} state=${changes} data=${data} base=${base}
                           tab=${tab} onTab=${setTab} onBase=${setBase}
                           onFile=${(path) => push({ kind: "file", path })} />`}
        </div>
    `;
}

// WrapButton switches the wrapping of long lines. Off by default: a wrapped
// line of code is the thing people ask to be able to turn off first, and a
// line that runs off the edge is read by scrolling the block it is in.
function WrapButton({ on, onClick }) {
    return html`
        <button class=${`viewbtn${on ? " on" : ""}`} type="button"
                aria-pressed=${on} title="wrap long lines" onClick=${onClick}>
            ${Icon.wrap ? Icon.wrap() : "↵"}
        </button>
    `;
}

function ChangesBody({ cwd, state, data, base, tab, onTab, onBase, onFile }) {
    return html`
        <div class="cdstrip" hidden=${Boolean(data && data.noRepo)}>
            <button class="chip" type="button" aria-pressed=${tab === "feed"}
                    onClick=${() => onTab("feed")}>Changes</button>
            <button class="chip" type="button" aria-pressed=${tab === "tree"}
                    onClick=${() => onTab("tree")}>Files</button>
            ${data && data.base && html`
                <button class="chip cdbasepick" type="button"
                        onClick=${() => onBase(base ? "" : data.base)}
                        title="the base of this reading only">
                    ${base ? "project base" : "this reading"}
                </button>
            `}
        </div>

        ${state.kind === "loading" && html`<p class="hint">Reading the repository…</p>`}
        ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}

        ${data && data.noRepo && html`
            <p class="hint">
                This project keeps no git repository — there is nothing here to compare or review.
                <br />The directory itself is at <b>${data.root || cwd}</b>.
            </p>
        `}
        ${data && !data.noRepo && tab === "tree" && html`<${TreePane} cwd=${cwd} changes=${data} onFile=${onFile} />`}
        ${data && !data.noRepo && tab === "feed" && html`<${ChangeFeed} cwd=${cwd} data=${data} onFile=${onFile} />`}
    `;
}

// ChangeFeed is the run of changes: every file this branch touched, in one
// document. The diff of a file is fetched when it is opened rather than all at
// once — a day here has been seventy-seven files and five thousand lines.
function ChangeFeed({ cwd, data, onFile }) {
    const files = data.files || [];
    if (!files.length) {
        return html`<p class="hint">Nothing has changed against <b>${data.base || "the base"}</b>.</p>`;
    }
    return html`
        <div class="cdfeed">
            ${files.map((f) => html`
                <${FileCard} key=${f.path} cwd=${cwd} file=${f} rev=${data.rev}
                             base=${data.base} onOpen=${() => onFile(f.path)} />
            `)}
            ${data.cut && html`
                <p class="cdcut">Showing <b>${files.length}</b> of <b>${data.total}</b> files — the rest is a tap away, not quietly dropped.</p>
            `}
        </div>
    `;
}

// FileCard is one file of the run: its name and counts, and its diff once it
// has been unfolded.
function FileCard({ cwd, file, rev, base, onOpen }) {
    const [open, setOpen] = useState(false);
    const [state] = useAsk(() => diffOf(cwd, file.path, base, rev), [cwd, file.path, base, rev], open);
    const diff = state.kind === "ready" ? state.data : null;

    return html`
        <div class="cdfile">
            <button class="cdfilehead" type="button" onClick=${() => setOpen((o) => !o)}>
                <i class=${`cddot ${file.layer === "worktree" ? "wt" : "done"}`}></i>
                <span class="cdname">${file.path}</span>
                <span class="cdstat">
                    <span class="plus">+${file.add}</span> <span class="minus">−${file.delete}</span>
                </span>
                <span class="cdchev">${open ? "▾" : "▸"}</span>
            </button>
            ${open && html`
                <div class="cdbody">
                    ${state.kind === "loading" && html`<p class="hint">Reading the diff…</p>`}
                    ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
                    ${diff && diff.stale && html`
                        <p class="hint">The repository moved while this was being read. Pull the list again.</p>
                    `}
                    ${diff && !diff.stale && (diff.files || []).map((f) =>
                        f.hunks.map((h, i) => html`<${Hunk} key=${`${f.path}-${i}`} hunk=${h} path=${f.path} />`))}
                    ${diff && !diff.stale && (diff.files || []).some((f) => f.cut) && html`
                        <p class="cdcut">The diff of this file is longer than one reading — open the file to walk it.</p>
                    `}
                    <button class="cdopen" type="button" onClick=${onOpen}>Open the file</button>
                </div>
            `}
        </div>
    `;
}

// TreePane walks the directories of the working tree. One directory at a time:
// a tree is opened by opening what is asked for, and a listing of everything is
// a number nobody reads.
function TreePane({ cwd, changes, onFile }) {
    const [where, setWhere] = useState("");
    const [state] = useAsk(() => treeOf(cwd, where), [cwd, where]);
    const data = state.kind === "ready" ? state.data : null;
    const counts = new Map((changes.files || []).map((f) => [f.path, f]));

    const up = where ? where.split("/").slice(0, -1).join("/") : null;
    return html`
        <div class="cdtree">
            <div class="cdcrumbs">
                <button class="cdcrumb" type="button" onClick=${() => setWhere("")}>${changes.branch}</button>
                ${where.split("/").filter(Boolean).map((part, i, all) => html`
                    <button key=${part + i} class="cdcrumb" type="button"
                            onClick=${() => setWhere(all.slice(0, i + 1).join("/"))}>${part}</button>
                `)}
            </div>
            ${state.kind === "loading" && html`<p class="hint">Reading the directory…</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${up !== null && html`
                <button class="cdnode" type="button" onClick=${() => setWhere(up)}>
                    <span class="cdcaret">↑</span><span class="cdnm">..</span>
                </button>
            `}
            ${data && (data.entries || []).map((e) => {
                const full = where ? `${where}/${e.name}` : e.name;
                const changed = counts.get(full);
                return html`
                    <button key=${e.name} class="cdnode" type="button"
                            onClick=${() => (e.dir ? setWhere(full) : onFile(full))}>
                        <span class="cdcaret">${e.dir ? "▸" : ""}</span>
                        ${changed && html`<i class=${`cddot ${changed.layer === "worktree" ? "wt" : "done"}`}></i>`}
                        <span class="cdnm">${e.name}${e.dir ? "/" : ""}</span>
                        ${changed && html`
                            <span class="cdstat">
                                <span class="plus">+${changed.add}</span> <span class="minus">−${changed.delete}</span>
                            </span>
                        `}
                    </button>
                `;
            })}
            ${data && data.cut && html`
                <p class="cdcut">Showing <b>${(data.entries || []).length}</b> of <b>${data.total}</b> names.</p>
            `}
        </div>
    `;
}

// FileBody is one file, whole: the window a screen reads it by, with the next
// one a tap away rather than a scroll that never ends.
function FileBody({ cwd, path, base }) {
    const [first, setFirst] = useState(1);
    const [mode, setMode] = useState("file");
    const [file] = useAsk(() => fileOf(cwd, path, "", first, WINDOW), [cwd, path, first], mode === "file");
    const [diff] = useAsk(() => diffOf(cwd, path, base, ""), [cwd, path, base], mode === "diff");

    const data = file.kind === "ready" ? file.data : null;
    const cut = diff.kind === "ready" ? diff.data : null;

    return html`
        <div class="cdstrip">
            <button class="chip" type="button" aria-pressed=${mode === "file"}
                    onClick=${() => setMode("file")}>File</button>
            <button class="chip" type="button" aria-pressed=${mode === "diff"}
                    onClick=${() => setMode("diff")}>Diff</button>
        </div>

        ${mode === "file" && html`
            ${file.kind === "loading" && html`<p class="hint">Reading the file…</p>`}
            ${file.kind === "failed" && html`<p class="hint crit">${file.error}</p>`}
            ${data && data.binary && html`<p class="hint">This file is binary — there is nothing to read here.</p>`}
            ${data && data.tooBig && html`<p class="hint">This file is ${Math.round(data.size / 1024)} KB, past what the panel reads in one piece.</p>`}
            ${data && data.lines && html`
                <${FileLines} path=${path} first=${data.first} lines=${data.lines} spans=${data.spans}
                              head=${`lines ${data.first}\u2013${data.first + data.lines.length - 1} of ${data.total}`} />
                ${data.more && html`
                    <button class="cdopen" type="button"
                            onClick=${() => setFirst(data.first + data.lines.length)}>
                        The next ${WINDOW} lines
                    </button>
                `}
                ${data.first > 1 && html`
                    <button class="cdopen" type="button"
                            onClick=${() => setFirst(Math.max(1, data.first - WINDOW))}>
                        The ${WINDOW} before
                    </button>
                `}
            `}
        `}

        ${mode === "diff" && html`
            ${diff.kind === "loading" && html`<p class="hint">Reading the diff…</p>`}
            ${diff.kind === "failed" && html`<p class="hint crit">${diff.error}</p>`}
            ${cut && !(cut.files || []).length && html`<p class="hint">This file has not changed against <b>${cut.base}</b>.</p>`}
            ${cut && (cut.files || []).map((f) =>
                f.hunks.map((h, i) => html`<${Hunk} key=${`${f.path}-${i}`} hunk=${h} path=${f.path} />`))}
        `}
    `;
}
