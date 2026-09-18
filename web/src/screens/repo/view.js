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
    // The stack of the viewer. The page at the top is what is drawn, and going
    // back is dropping it: a conversation left by three taps comes back by
    // three, and none of them lands anywhere else.
    const [stack, setStack] = useState([{ kind: "changes" }]);
    const top = stack[stack.length - 1];
    const push = useCallback((page) => setStack((was) => [...was, page]), []);
    const pop = useCallback(() => setStack((was) => (was.length > 1 ? was.slice(0, -1) : was)), []);

    const back = useCallback(() => {
        if (stack.length > 1) pop();
        else onBack();
    }, [stack.length, pop, onBack]);

    // One base for the whole stack: a file opened from a list measured against
    // one branch and then measured against another is two answers to one
    // question.
    const [base, setBase] = useState("");
    const [wrap, setWrap] = useState(false);

    if (top.kind === "file") {
        return html`<${FilePage}
            cwd=${cwd} path=${top.path} base=${base} wrap=${wrap}
            onWrap=${() => setWrap((w) => !w)} onBack=${back} />`;
    }
    return html`<${ChangesPage}
        cwd=${cwd} name=${name} base=${base} onBase=${setBase} wrap=${wrap}
        onWrap=${() => setWrap((w) => !w)}
        onFile=${(path) => push({ kind: "file", path })} onBack=${back} />`;
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

function ChangesPage({ cwd, name, base, onBase, wrap, onWrap, onFile, onBack }) {
    // Each layer with an arrow of its own catches the gesture itself: one
    // subscription for two layers leaves the second uncovered, and a swipe
    // there closes the app rather than the page.
    useBackClose(true, onBack);
    const [tab, setTab] = useState("feed");
    const [state] = useAsk(() => changesOf(cwd, base), [cwd, base]);
    const data = state.kind === "ready" ? state.data : null;

    const head = html`
        <${BackHead} onBack=${onBack} label="to the conversation"
                     tools=${html`<${WrapButton} on=${wrap} onClick=${onWrap} />`}>
            <div class="chathead">
                <h2>${data ? data.branch : name}</h2>
                <div class="chatsub">
                    ${data
                        ? html`<span class="cdbase" title=${`the base comes from the ${data.baseFrom}`}>
                                 against <b>${data.base || "nothing"}</b>
                               </span>
                               <span>${data.total} ${data.total === 1 ? "file" : "files"}</span>`
                        : html`<span>${cwd}</span>`}
                </div>
            </div>
        <//>
    `;

    return html`
        ${head}
        <div class=${`cdpage${wrap ? " wrap" : ""}`}>
            <div class="cdstrip">
                <button class="chip" type="button" aria-pressed=${tab === "feed"}
                        onClick=${() => setTab("feed")}>Changes</button>
                <button class="chip" type="button" aria-pressed=${tab === "tree"}
                        onClick=${() => setTab("tree")}>Files</button>
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

            ${data && tab === "tree" && html`<${TreePane} cwd=${cwd} changes=${data} onFile=${onFile} />`}
            ${data && tab === "feed" && html`<${ChangeFeed} cwd=${cwd} data=${data} onFile=${onFile} />`}
        </div>
    `;
}

// ChangeFeed is the run of changes: every file this branch touched, in one
// document. The diff of a file is fetched when it is opened rather than all at
// once — a day here has been seventy-seven files and five thousand lines.
function ChangeFeed({ cwd, data, onFile }) {
    if (!data.files.length) {
        return html`<p class="hint">Nothing has changed against <b>${data.base || "the base"}</b>.</p>`;
    }
    return html`
        <div class="cdfeed">
            ${data.files.map((f) => html`
                <${FileCard} key=${f.path} cwd=${cwd} file=${f} rev=${data.rev}
                             base=${data.base} onOpen=${() => onFile(f.path)} />
            `)}
            ${data.cut && html`
                <p class="cdcut">Showing <b>${data.files.length}</b> of <b>${data.total}</b> files — the rest is a tap away, not quietly dropped.</p>
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
                    ${diff && !diff.stale && diff.files.map((f) =>
                        f.hunks.map((h, i) => html`<${Hunk} key=${`${f.path}-${i}`} hunk=${h} path=${f.path} />`))}
                    ${diff && !diff.stale && diff.files.some((f) => f.cut) && html`
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
    const counts = new Map(changes.files.map((f) => [f.path, f]));

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
            ${data && data.entries.map((e) => {
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
                <p class="cdcut">Showing <b>${data.entries.length}</b> of <b>${data.total}</b> names.</p>
            `}
        </div>
    `;
}

// FilePage is one file, whole: the window a screen reads it by, with the next
// one a tap away rather than a scroll that never ends.
function FilePage({ cwd, path, base, wrap, onWrap, onBack }) {
    useBackClose(true, onBack);
    const [first, setFirst] = useState(1);
    const [mode, setMode] = useState("file");
    const [file] = useAsk(() => fileOf(cwd, path, "", first, WINDOW), [cwd, path, first], mode === "file");
    const [diff] = useAsk(() => diffOf(cwd, path, base, ""), [cwd, path, base], mode === "diff");

    const name = path.split("/").pop();
    const dir = path.split("/").slice(0, -1).join("/");
    const data = file.kind === "ready" ? file.data : null;
    const cut = diff.kind === "ready" ? diff.data : null;

    return html`
        <${BackHead} onBack=${onBack} label="to the changes"
                     tools=${html`<${WrapButton} on=${wrap} onClick=${onWrap} />`}>
            <div class="chathead">
                <h2>${name}</h2>
                <div class="chatsub"><span>${dir || "/"}</span></div>
            </div>
        <//>
        <div class=${`cdpage${wrap ? " wrap" : ""}`}>
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
                                  head=${`lines ${data.first}–${data.first + data.lines.length - 1} of ${data.total}`} />
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
                ${cut && !cut.files.length && html`<p class="hint">This file has not changed against <b>${cut.base}</b>.</p>`}
                ${cut && cut.files.map((f) =>
                    f.hunks.map((h, i) => html`<${Hunk} key=${`${f.path}-${i}`} hunk=${h} path=${f.path} />`))}
            `}
        </div>
    `;
}
