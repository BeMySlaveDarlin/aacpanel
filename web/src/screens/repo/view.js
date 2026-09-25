// The repository as a screen reads it: a page opened from the head of a
// conversation, and left one step at a time.
//
// One screen, two shapes. On a phone it is a page at a time — the run, then
// one file — because a strip that scrolls sideways under a diff that also
// scrolls sideways leaves nothing to swipe the page by. At a desk the same
// pieces stand side by side: the code in the middle, the directory in a panel
// beside it, and the open files as tabs above.
//
// The files are there whether git keeps the project or not; the branch is not.
// Without a repository the screen is the directory and the files opened from
// it — no run of changes, no diff, no base and no notes, rather than a screen
// that refuses to show a directory because nothing in it can be compared.

import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Icon } from "../../ui/icons.js";
import { useWide } from "../../ui/wide.js";
import { blameOf, changesOf, diffOf, fileOf, findOf, treeOf, useAsk } from "./data.js";
import { BlameLines } from "./blame.js";
import { FileLines, Hunk, SplitHunk } from "./lines.js";
import { NoteBox, NotesPane, noteAt, place, useReview } from "./notes.js";

// How much of a file a window carries. The number is the one the service and
// the agent hold too: three places counting differently is a screen that asks
// for two hundred lines and draws a hundred.
const WINDOW = 200;

// The window of a file that holds a line. A note points at a line, and a file
// opened at its first page with that line four hundred rows further down is a
// file opened nowhere in particular.
function windowFor(line) {
    if (!line || line < 1) return 1;
    return Math.floor((line - 1) / WINDOW) * WINDOW + 1;
}

// The name a directory goes by: the last part of its path.
function dirName(path) {
    const parts = String(path || "").split("/").filter(Boolean);
    return parts.length ? parts[parts.length - 1] : "/";
}

// Nothing to write on. Notes belong to a reading of a branch, and a project
// without one has no branch to read: a line there is not picked up at all
// rather than picked up into a box that has nowhere to keep what is written.
const NO_NOTES = { notes: [], picked: null, onPick: null, composer: null };

export function RepoView({ cwd, name, onBack, onSend, onConversation }) {
    const wide = useWide();

    // What is open and which one is being read. The viewer never went deeper
    // than "the changes, and then one file", so a list of open files with one
    // of them in front says everything a stack of pages said — and on a wide
    // screen that list is also the row of tabs. Both live in one piece of
    // state: a file closed and a file picked are one move, and two states
    // moved one after the other draw a frame in between where neither holds.
    const [tabs, setTabs] = useState({ open: [], active: "" });

    const openFile = useCallback((path) => {
        setTabs((t) => ({
            open: t.open.includes(path) ? t.open : [...t.open, path],
            active: path,
        }));
    }, []);

    // Closing the one being read hands the screen to its neighbour rather than
    // to the changes: the tab beside it is what the eye is already on.
    const closeFile = useCallback((path) => {
        setTabs((t) => {
            const at = t.open.indexOf(path);
            const open = t.open.filter((p) => p !== path);
            if (t.active !== path) return { open, active: t.active };
            return { open, active: open[at] || open[at - 1] || "" };
        });
    }, []);

    const back = useCallback(() => {
        // On a phone the file is a page over the changes, so the way back goes
        // through it. At a desk the file is a tab, and the tabs are closed by
        // their own crosses — the way back belongs to the conversation.
        if (!wide && tabs.active) {
            setTabs((t) => ({ ...t, active: "" }));
            return;
        }
        onBack();
    }, [onBack, wide, tabs.active]);
    useBackClose(true, back);

    // One base for the whole reading: a file opened from a list measured
    // against one branch and then measured against another is two answers to
    // one question.
    const [base, setBase] = useState("");
    const [wrap, setWrap] = useState(false);
    const [tab, setTab] = useState("feed");
    const [pane, setPane] = useState(true);

    // Which notes have lost their line. The windows of files come and go, and
    // the list of notes is one, so what a window learned is kept here — by
    // file, so that reopening one says what it says now and nothing older.
    const [adrift, setAdrift] = useState({});
    const onStale = useCallback((path, ids) => {
        setAdrift((was) => {
            const now = [...ids].sort().join(" ");
            if ((was[path] || []).join(" ") === now) return was;
            return { ...was, [path]: [...ids].sort() };
        });
    }, []);
    const stale = useMemo(() => new Set(Object.values(adrift).flat()), [adrift]);

    // The reading of this branch, and the line being written on. Both stand
    // here rather than in the bodies below: the same note is drawn in the run
    // of changes, in the file it belongs to and in the list beside them, and
    // three copies of it would disagree the moment one was written to.
    const review = useReview(cwd, name, base);
    const [picked, setPicked] = useState(null);
    const [jump, setJump] = useState(null);

    // Picking the same line twice puts the box away. A number is the only
    // handle a line has, and a handle that cannot be let go of is a box that
    // has to be cancelled to be rid of.
    const pick = useCallback((path, line, quote) => {
        setPicked((was) => (was && was.path === path && was.line === line && was.quote === quote
            ? null
            : { path, line, quote }));
    }, []);

    const held = picked && !review.sentAt
        ? noteAt(review.notes, picked.path, picked.line, picked.quote)
        : null;

    const composer = picked && html`
        <${NoteBox}
            key=${`${picked.path}:${picked.line}`}
            note=${held}
            quote=${picked.quote}
            onSave=${(text) => {
                review.add(picked.path, picked.line, picked.quote, text);
                setPicked(null);
            }}
            onRemove=${() => {
                if (held) review.remove(held.id);
                setPicked(null);
            }}
            onClose=${() => setPicked(null)}
        />
    `;

    const [changes] = useAsk(() => changesOf(cwd, base), [cwd, base]);
    const data = changes.kind === "ready" ? changes.data : null;
    // Whether the project has a branch at all. The changes answer it: a
    // directory git does not keep comes back as exactly that, with its root.
    const plain = Boolean(data && data.noRepo);

    const noting = plain ? NO_NOTES : { notes: review.notes, picked, onPick: pick, composer };

    // A note opened from the list goes back to where it was written: the file,
    // the window of it that holds the line, and the line itself with the box
    // under it. A list that only says "env.go:212" is read with a finger on
    // the screen and the other hand scrolling.
    const openAt = useCallback((path, line, quote) => {
        openFile(path);
        setJump({ path, line, on: Date.now() });
        setPicked({ path, line, quote });
    }, [openFile]);

    // Finding a file by its name. Only where there is a keyboard to press it
    // on: a phone has no Ctrl and nothing to bind this to.
    const [finding, setFinding] = useState(false);
    useEffect(() => {
        if (!wide) return undefined;
        const on = (e) => {
            if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "K")) {
                e.preventDefault();
                setFinding((was) => !was);
                return;
            }
            if (e.key === "Escape") setFinding(false);
        };
        window.addEventListener("keydown", on);
        return () => window.removeEventListener("keydown", on);
    }, [wide]);

    const file = tabs.active;

    // The head names the branch when there is one, and the directory when
    // there is not: a project without git is known by where it lies.
    const head = html`
        <div class="chathead">
            <h2>${file && !wide ? file.split("/").pop()
                : data && !plain ? data.branch
                : plain ? dirName(data.root || cwd)
                : name}</h2>
            <div class="chatsub">
                ${file && !wide
                    ? html`<span>${file.split("/").slice(0, -1).join("/") || "/"}</span>`
                    : data && !plain
                    ? html`<span class="cdbase" title=${`the base comes from the ${data.baseFrom}`}>
                             against <b>${data.base || "nothing"}</b>
                           </span>
                           <span>${data.total} ${data.total === 1 ? "file" : "files"}</span>`
                    : html`<span>${(plain && data.root) || cwd}</span>`}
            </div>
        </div>
    `;

    const tools = html`
        ${wide && html`<${PaneButton} on=${pane} onClick=${() => setPane((p) => !p)} />`}
        <${WrapButton} on=${wrap} onClick=${() => setWrap((w) => !w)} />
    `;

    const label = file && !wide ? (plain ? "to the files" : "to the changes") : "to the conversation";

    return html`
        <${BackHead} onBack=${back} label=${label} tools=${tools}>${head}<//>
        ${finding && html`
            <${FileFinder} cwd=${cwd} onClose=${() => setFinding(false)}
                           onPick=${(path) => { setFinding(false); openFile(path); }} />
        `}
        ${wide
            ? html`<${DeskBody}
                       cwd=${cwd} base=${base} wrap=${wrap} pane=${pane} plain=${plain}
                       tabs=${tabs} state=${changes} data=${data}
                       noting=${noting} review=${review} onSend=${onSend} onNote=${openAt}
                       stale=${stale} onStale=${onStale} onConversation=${onConversation}
                       jump=${jump && jump.path === file ? jump : null}
                       onPick=${(p) => setTabs((t) => ({ ...t, active: p }))}
                       onClose=${closeFile} onFile=${openFile} onBase=${setBase} />`
            : html`
                <div class=${`cdpage${wrap ? " wrap" : ""}`}>
                    ${file
                        ? html`<${FileBody} cwd=${cwd} path=${file} base=${base} noting=${noting}
                                            branch=${!plain} onConversation=${onConversation}
                                            notes=${noting.notes} onStale=${onStale}
                                            jump=${jump && jump.path === file ? jump : null} />`
                        : plain
                        ? html`<${TreePane} cwd=${cwd} changes=${data} onFile=${openFile} />`
                        : html`<${ChangesBody}
                                   cwd=${cwd} state=${changes} data=${data} base=${base} stale=${stale}
                                   noting=${noting} review=${review} onSend=${onSend} onNote=${openAt}
                                   tab=${tab} onTab=${setTab} onBase=${setBase} onFile=${openFile} />`}
                </div>
            `}
    `;
}

// FileFinder opens a file by its name. A repository is walked by its
// directories when the shape of it is the question; when the file is already
// known, walking down to it is four taps spent on something typing answers in
// one.
function FileFinder({ cwd, onPick, onClose }) {
    const [q, setQ] = useState("");
    const [at, setAt] = useState(0);
    const box = useRef(null);
    const [state] = useAsk(() => findOf(cwd, q), [cwd, q], q.trim().length > 0);
    const data = state.kind === "ready" ? state.data : null;
    const paths = (data && data.paths) || [];

    useEffect(() => {
        if (box.current) box.current.focus();
    }, []);

    const keys = (e) => {
        if (e.key === "ArrowDown") {
            e.preventDefault();
            setAt((i) => Math.min(i + 1, Math.max(paths.length - 1, 0)));
            return;
        }
        if (e.key === "ArrowUp") {
            e.preventDefault();
            setAt((i) => Math.max(i - 1, 0));
            return;
        }
        if (e.key === "Enter" && paths[at]) {
            e.preventDefault();
            onPick(paths[at]);
        }
    };

    return html`
        <div class="cdfind" onClick=${onClose}>
            <div class="cdfindbox" onClick=${(e) => e.stopPropagation()}>
                <input class="cdfindin" ref=${box} type="text" value=${q}
                       placeholder="a name, or a piece of a path"
                       aria-label="find a file by name"
                       onKeyDown=${keys}
                       onInput=${(e) => { setQ(e.currentTarget.value); setAt(0); }} />
                <div class="cdfindlist">
                    ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
                    ${q.trim() && state.kind === "ready" && !paths.length && html`
                        <p class="hint">No name here carries that.</p>
                    `}
                    ${paths.map((path, i) => html`
                        <button key=${path} type="button"
                                class=${`cdfindrow${i === at ? " on" : ""}`}
                                onMouseEnter=${() => setAt(i)}
                                onClick=${() => onPick(path)}>
                            <span class="cdfindname">${path.split("/").pop()}</span>
                            <span class="cdfinddir">${path.split("/").slice(0, -1).join("/")}</span>
                        </button>
                    `)}
                    ${data && data.total > paths.length && html`
                        <p class="cdcut">Showing <b>${paths.length}</b> of <b>${data.total}</b> — type more of the name.</p>
                    `}
                    ${data && data.partial && html`
                        <p class="cdcut">The search stopped before it walked the whole directory — what sits deeper may be missing.</p>
                    `}
                </div>
            </div>
        </div>
    `;
}

// DeskBody is the wide shape: the code in the middle under the tabs of what is
// open, and the directory in a panel that folds away. The panel is one panel
// with tabs of its own rather than a second column — the notes of a review
// belong beside the tree, and two narrow columns leave the code nothing.
function DeskBody({ cwd, base, wrap, pane, plain, tabs, state, data, noting, review, jump, stale, onStale,
                   onSend, onNote, onPick, onClose, onFile, onBase, onConversation }) {
    // Which of the two the panel is showing. The tree and the notes are two
    // readings of the same repository, not two places to be — a panel that
    // remembered one of them per file would send the eye looking for the tab
    // it was last on.
    const [side, setSide] = useState("files");
    const notes = noting.notes || [];
    return html`
        <div class=${`cdwide${pane ? "" : " solo"}${wrap ? " wrap" : ""}`}>
            <div class="cdmain">
                ${(!plain || tabs.open.length > 0) && html`
                    <${TabStrip} tabs=${tabs} changes=${!plain} onPick=${onPick} onClose=${onClose} />
                `}
                <div class="cdscroll">
                    ${tabs.active
                        ? html`<${FileBody} cwd=${cwd} path=${tabs.active} base=${base} wide=${true}
                                            branch=${!plain} onConversation=${onConversation}
                                            noting=${noting} notes=${noting.notes}
                                            onStale=${onStale} jump=${jump} />`
                        : plain
                        ? html`<${PlainIdle} />`
                        : html`<${ChangesBody}
                                   cwd=${cwd} state=${state} data=${data} base=${base} noting=${noting}
                                   stale=${stale} review=${review} onSend=${onSend} onNote=${onNote}
                                   tab="feed" onTab=${null} onBase=${onBase} onFile=${onFile} />`}
                </div>
            </div>
            ${pane && html`
                <aside class="cdside">
                    ${!plain && html`
                        <div class="cdsidetabs">
                            <button class="chip" type="button" aria-pressed=${side === "files"}
                                    onClick=${() => setSide("files")}>Files</button>
                            <button class="chip cdnotetab" type="button" aria-pressed=${side === "notes"}
                                    onClick=${() => setSide("notes")}>
                                Notes${notes.length ? html` <b>${notes.length}</b>` : null}
                            </button>
                        </div>
                    `}
                    <div class="cdsidebody">
                        ${side === "notes" && !plain
                            ? html`<${NotesPane} review=${review} wide=${true} stale=${stale}
                                                 onOpen=${onNote} onSend=${onSend} />`
                            : data
                            ? html`<${TreePane} cwd=${cwd} changes=${data} onFile=${onFile} />`
                            : state.kind === "loading"
                            ? html`<p class="hint">Reading the project…</p>`
                            : null}
                    </div>
                </aside>
            `}
        </div>
    `;
}

// PlainIdle is the middle of a project without git while no file is open. Not
// the tree: the tree already stands in the panel beside, and a second copy of
// it in the middle is two places to be in the same directory. What is said
// instead is the two ways to a file.
function PlainIdle() {
    return html`
        <p class="hint">
            No git repository here, so no changes to read — only the files.
            <br />Pick one in the tree beside, or press Ctrl+K to find it by name.
        </p>
    `;
}

// TabStrip is what is open. The changes stay the leftmost tab and cannot be
// closed: they are where a reading starts, and a viewer with every tab shut is
// a blank panel nobody asked for. A project without git has no changes, and
// its strip is the open files alone.
function TabStrip({ tabs, changes, onPick, onClose }) {
    return html`
        <div class="cdtabs" role="tablist">
            ${changes && html`
                <button class="cdtab" type="button" role="tab" aria-selected=${!tabs.active}
                        onClick=${() => onPick("")}>
                    <span class="cdtabname">Changes</span>
                </button>
            `}
            ${tabs.open.map((path) => html`
                <span key=${path} class=${`cdtab${tabs.active === path ? " on" : ""}`}>
                    <button class="cdtabpick" type="button" role="tab"
                            aria-selected=${tabs.active === path} title=${path}
                            onClick=${() => onPick(path)}>
                        <span class="cdtabname">${path.split("/").pop()}</span>
                    </button>
                    <button class="cdtabx" type="button" aria-label=${`close ${path}`}
                            onClick=${() => onClose(path)}>×</button>
                </span>
            `)}
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

// PaneButton folds the side panel away, for a file whose lines are wider than
// what is left of the middle.
function PaneButton({ on, onClick }) {
    return html`
        <button class=${`viewbtn${on ? " on" : ""}`} type="button"
                aria-pressed=${on} title="the panel beside the code" onClick=${onClick}>
            ▥
        </button>
    `;
}

// ChangesBody is the branch: the run of changes, the tree with the changes
// marked on it, and the notes.
function ChangesBody({ cwd, state, data, base, noting, review, stale, tab, onTab, onBase, onFile, onNote, onSend }) {
    const notes = (noting && noting.notes) || [];
    return html`
        <div class="cdstrip">
            ${onTab && html`
                <button class="chip" type="button" aria-pressed=${tab === "feed"}
                        onClick=${() => onTab("feed")}>Changes</button>
                <button class="chip" type="button" aria-pressed=${tab === "tree"}
                        onClick=${() => onTab("tree")}>Files</button>
            `}
            ${data && data.base && html`
                <button class="chip cdbasepick" type="button"
                        onClick=${() => onBase(base ? "" : data.base)}
                        title="the base of this reading only">
                    ${base ? "project base" : "this reading"}
                </button>
            `}
            ${onTab && html`
                <button class="chip cdnotetab" type="button" aria-pressed=${tab === "notes"}
                        onClick=${() => onTab("notes")}>
                    Notes${notes.length ? html` <b>${notes.length}</b>` : null}
                </button>
            `}
        </div>

        ${state.kind === "loading" && html`<p class="hint">Reading the project…</p>`}
        ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}

        ${data && tab === "tree" && html`<${TreePane} cwd=${cwd} changes=${data} onFile=${onFile} />`}
        ${data && tab === "feed" && html`
            <${ChangeFeed} cwd=${cwd} data=${data} noting=${noting} onFile=${onFile} />
        `}
        ${tab === "notes" && html`<${NotesPane} review=${review} onOpen=${onNote} onSend=${onSend} stale=${stale} />`}
    `;
}

// ChangeFeed is the run of changes: every file this branch touched, in one
// document. The diff of a file is fetched when it is opened rather than all at
// once — a day here has been seventy-seven files and five thousand lines.
function ChangeFeed({ cwd, data, noting, onFile }) {
    const files = data.files || [];
    if (!files.length) {
        return html`<p class="hint">Nothing has changed against <b>${data.base || "the base"}</b>.</p>`;
    }
    return html`
        <div class="cdfeed">
            ${files.map((f) => html`
                <${FileCard} key=${f.path} cwd=${cwd} file=${f} rev=${data.rev} noting=${noting}
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
function FileCard({ cwd, file, rev, base, noting, onOpen }) {
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
                        f.hunks.map((h, i) => html`
                            <${Hunk} key=${`${f.path}-${i}`} hunk=${h} path=${f.path}
                                     notes=${noting.notes} picked=${noting.picked}
                                     onPick=${noting.onPick} composer=${noting.composer} />
                        `))}
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
    const counts = useMemo(
        () => new Map((changes.files || []).map((f) => [f.path, f])),
        [changes.files],
    );

    const up = where ? where.split("/").slice(0, -1).join("/") : null;
    return html`
        <div class="cdtree">
            <div class="cdcrumbs">
                <button class="cdcrumb" type="button" onClick=${() => setWhere("")}>
                    ${changes.noRepo ? dirName(changes.root || cwd) : changes.branch}
                </button>
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
function FileBody({ cwd, path, base, wide, branch, noting, jump, notes, onStale, onConversation }) {
    const [first, setFirst] = useState(() => windowFor(jump && jump.line));
    const [mode, setMode] = useState("file");
    // Side by side is offered only where there is room for two columns. On a
    // phone it is two half-width columns of code, which is neither side read.
    const [split, setSplit] = useState(false);
    const [file] = useAsk(() => fileOf(cwd, path, "", first, WINDOW), [cwd, path, first], mode !== "diff");
    // Who wrote the file is read by the same window as the file: the lines
    // and the commits beside them are the same lines.
    const [who] = useAsk(() => blameOf(cwd, path, "", first, WINDOW), [cwd, path, first], mode === "blame");
    const [diff] = useAsk(() => diffOf(cwd, path, base, ""), [cwd, path, base], mode === "diff");

    // A note opened from the list lands on the window that holds its line
    // rather than on the first page of the file.
    useEffect(() => {
        if (jump) setFirst(windowFor(jump.line));
    }, [jump]);

    const data = file.kind === "ready" ? file.data : null;
    const cut = diff.kind === "ready" ? diff.data : null;
    const marks = noting || {};

    // Which notes of this file found no line in the window in front of us. It
    // is said once the window is read rather than guessed from the numbers: a
    // note is outdated because the file no longer says what it said, and only
    // the file can answer that.
    useEffect(() => {
        if (!onStale || !data || !data.lines) return;
        const rows = data.lines.map((text, i) => ({ kind: "ctx", new: data.first + i, text }));
        const here = (notes || []).filter((n) => n.path === path
            && n.line >= data.first && n.line < data.first + data.lines.length);
        onStale(path, place(here, path, rows).stale);
    }, [onStale, notes, path, data]);

    // The file against the diff of it is a question about a branch. Without one
    // there is only the file, and a strip with one chip in it is a switch that
    // switches nothing.
    return html`
        ${branch && html`<div class="cdstrip">
            <button class="chip" type="button" aria-pressed=${mode === "file"}
                    onClick=${() => setMode("file")}>File</button>
            <button class="chip" type="button" aria-pressed=${mode === "diff"}
                    onClick=${() => setMode("diff")}>Diff</button>
            <button class="chip" type="button" aria-pressed=${mode === "blame"}
                    onClick=${() => setMode("blame")}>Who wrote</button>
            ${wide && mode === "diff" && html`
                <button class="chip cdsplitpick" type="button" aria-pressed=${split}
                        onClick=${() => setSplit((s) => !s)}
                        title="before and after, side by side">
                    ${split ? "Side by side" : "In one column"}
                </button>
            `}
        </div>`}

        ${mode === "file" && html`
            ${file.kind === "loading" && html`<p class="hint">Reading the file…</p>`}
            ${file.kind === "failed" && html`<p class="hint crit">${file.error}</p>`}
            ${data && data.binary && html`<p class="hint">This file is binary — there is nothing to read here.</p>`}
            ${data && data.tooBig && html`<p class="hint">This file is ${Math.round(data.size / 1024)} KB, past what the panel reads in one piece.</p>`}
            ${data && data.lines && html`
                <${FileLines} path=${path} first=${data.first} lines=${data.lines} spans=${data.spans}
                              notes=${marks.notes} picked=${marks.picked}
                              onPick=${marks.onPick} composer=${marks.composer}
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

        ${mode === "blame" && html`
            ${(file.kind === "loading" || who.kind === "loading") && html`<p class="hint">Reading who wrote the file…</p>`}
            ${file.kind === "failed" && html`<p class="hint crit">${file.error}</p>`}
            ${who.kind === "failed" && html`<p class="hint crit">${who.error}</p>`}
            ${data && (data.binary || data.tooBig) && html`<p class="hint">There are no lines to name the writers of.</p>`}
            ${data && data.lines && who.kind === "ready" && html`
                <${BlameLines} cwd=${cwd} first=${data.first} lines=${data.lines} spans=${data.spans}
                               blame=${who.data.blame || []} commits=${who.data.commits || {}}
                               onConversation=${onConversation}
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
            ${cut && !(cut.files || []).length && html`<p class="hint">This file has not changed against <b>${cut.base}</b>.</p>`}
            ${cut && (cut.files || []).map((f) =>
                f.hunks.map((h, i) => (wide && split
                    ? html`
                        <${SplitHunk} key=${`${f.path}-${i}`} hunk=${h} path=${f.path}
                                      notes=${marks.notes} picked=${marks.picked}
                                      onPick=${marks.onPick} composer=${marks.composer} />
                    `
                    : html`
                        <${Hunk} key=${`${f.path}-${i}`} hunk=${h} path=${f.path}
                                 notes=${marks.notes} picked=${marks.picked}
                                 onPick=${marks.onPick} composer=${marks.composer} />
                    `)))}
        `}
    `;
}
