// Files belong beside the conversation. The directory stays mounted while a
// file takes the feed's place, so returning from it preserves both scrolls.
// The phone uses the same directory as a full page and takes one back step
// through the file before it closes the list.

import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useBackClose } from "../../ui/back.js";
import { Icon } from "../../ui/icons.js";
import { useWide } from "../../ui/wide.js";
import { blameOf, changesOf, diffOf, fileOf, findOf, treeOf, useAsk } from "./data.js";
import { BlameLines } from "./blame.js";
import { FileLines, Hunk, SplitHunk } from "./lines.js";
import { NoteBox, NotesPane, noteAt, place, useReview } from "./notes.js";

const WINDOW = 200;

function windowFor(line) {
    if (!line || line < 1) return 1;
    return Math.floor((line - 1) / WINDOW) * WINDOW + 1;
}

function dirName(path) {
    return String(path || "").split("/").filter(Boolean).pop() || "/";
}

const NO_NOTES = { notes: [], picked: null, onPick: null, composer: null };

export function useRepo({ cwd, name, onFile }) {
    const wide = useWide();
    const [open, setOpen] = useState(false);
    const [file, setFile] = useState("");
    const [side, setSide] = useState("files");
    const [base, setBase] = useState("");
    const [wrap, setWrap] = useState(false);
    const [focus, setFocus] = useState(0);
    const [picked, setPicked] = useState(null);
    const [jump, setJump] = useState(null);
    const mainRef = useRef(null);
    const [changes] = useAsk(() => changesOf(cwd, base), [cwd, base, open], Boolean(cwd));
    const data = changes.kind === "ready" ? changes.data : null;
    const plain = Boolean(data && data.noRepo);
    const review = useReview(cwd, data && !plain ? name : "", base);
    const [adrift, setAdrift] = useState({});
    const onStale = useCallback((path, ids) => {
        setAdrift((was) => {
            const now = [...ids].sort();
            if ((was[path] || []).join(" ") === now.join(" ")) return was;
            return { ...was, [path]: now };
        });
    }, []);
    const stale = useMemo(() => new Set(Object.values(adrift).flat()), [adrift]);
    const openFile = (path) => {
        setFile(path);
        setPicked(null);
        setJump(null);
        setSide("files");
        if (onFile) onFile();
    };
    const openAt = (path, line, quote) => {
        openFile(path);
        setJump({ path, line, on: Date.now() });
        setPicked({ path, line, quote });
    };
    const held = picked && !review.sentAt ? noteAt(review.notes, picked.path, picked.line, picked.quote) : null;
    const composer = picked && html`
        <${NoteBox} key=${`${picked.path}:${picked.line}`} note=${held} quote=${picked.quote}
            onSave=${(text) => { review.add(picked.path, picked.line, picked.quote, text); setPicked(null); }}
            onRemove=${() => { if (held) review.remove(held.id); setPicked(null); }}
            onClose=${() => setPicked(null)} />
    `;
    const noting = plain ? NO_NOTES : {
        notes: review.notes, picked, composer,
        onPick: (path, line, quote) => setPicked((was) => was && was.path === path && was.line === line && was.quote === quote
            ? null : { path, line, quote }),
    };

    useBackClose(!wide && open, () => setOpen(false));
    useBackClose(!wide && open && Boolean(file), () => setFile(""));
    useBackClose(!wide && open && side === "notes", () => setSide("files"));
    useEffect(() => {
        if (!wide || !cwd) return undefined;
        const key = (e) => {
            if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
                e.preventDefault();
                setOpen(true);
                setSide("files");
                setFocus((n) => n + 1);
            }
        };
        window.addEventListener("keydown", key);
        return () => window.removeEventListener("keydown", key);
    }, [wide, cwd]);

    return { cwd, name, wide, open, setOpen, file, setFile, side, setSide, base, setBase,
        wrap, setWrap, focus, mainRef, changes, data, plain, review, noting, jump, stale, onStale, openFile, openAt };
}

export function FilesButton({ repo }) {
    if (!repo.cwd) return null;
    const count = !repo.plain && repo.data ? repo.data.total : 0;
    return html`
        <button class=${`cdfilesbutton ${repo.wide ? "dktab" : "iconbtn"}${repo.open ? " on" : ""}`}
                type="button" aria-label="Files" aria-expanded=${repo.open}
                onClick=${() => repo.setOpen(!repo.open)}>
            ${Icon.files()}${repo.wide ? html`<span>Files${count ? ` · ${count} changed` : ""}</span>`
                : count ? html`<span class="cdfilescount">${count}</span>` : null}
        </button>
    `;
}

// The middle owns its composer and gives only the feed's place to a file.
// Keeping the list mounted also keeps a phone's folder and scroll on return.
export function RepoView({ repo, feed, children, onSend, onConversation }) {
    const { cwd, wide, open, file, side, data, plain, review, noting } = repo;
    const changed = data && !plain && (data.files || []).find((f) => f.path === file);
    const fileShown = Boolean(file) && (wide || open);
    const notesShown = open && side === "notes" && !plain;
    return html`
        <div class="cdsession">
            <div class="cdmain">
                <div class="cdviewslot" ref=${repo.mainRef}>
                    <div class="cdfeedslot" hidden=${fileShown}>${feed}</div>
                    ${fileShown && html`
                        <div class=${`cdfileview${repo.wrap ? " wrap" : ""}`} hidden=${!wide && notesShown}>
                            <${FileBody} key=${file} cwd=${cwd} path=${file} base=${repo.base} wide=${wide}
                                branch=${Boolean(data) && !plain} changed=${changed} noting=${noting}
                                notes=${noting.notes} onStale=${repo.onStale} jump=${repo.jump}
                                onConversation=${onConversation} onFeed=${() => repo.setFile("")}
                                onNotes=${() => { repo.setOpen(true); repo.setSide("notes"); }}
                                wrap=${repo.wrap} onWrap=${() => repo.setWrap((w) => !w)} />
                        </div>
                    `}
                </div>
                ${children}
            </div>
            ${cwd && html`
                <aside class="cdside" aria-label="Files" hidden=${!open || (!wide && fileShown && !notesShown)}>
                    ${!wide && html`
                        <div class="cdfileshead">
                            <div><h2>Files</h2><span>${dirName(cwd)}${data && !plain ? ` · ${data.branch}` : ""}</span></div>
                            <button class="iconbtn" type="button" aria-label="close Files" onClick=${() => repo.setOpen(false)}>${Icon.close()}</button>
                        </div>
                    `}
                    <div class="cdsidebody" hidden=${notesShown}>
                        <${FilesPane} repo=${repo} />
                    </div>
                    ${notesShown && html`
                        <div class="cdsidebody">
                            <button class="cdcontrol cdbackfiles" type="button" onClick=${() => repo.setSide("files")}>‹ Files</button>
                            <${NotesPane} review=${review} wide=${wide} stale=${repo.stale}
                                         onOpen=${repo.openAt} onSend=${onSend} />
                        </div>
                    `}
                </aside>
            `}
        </div>
    `;
}

function FileMarks({ file }) {
    return html`
        <span class=${`cdstatus ${file.status}`} title=${file.layer === "worktree" ? "not yet committed" : "committed"}>${file.status}</span>
        <span class="cdstat"><span class="plus">+${file.add}</span> <span class="minus">−${file.delete}</span></span>
    `;
}

function FilesPane({ repo }) {
    const { data, plain, changes, base } = repo;
    const files = data && !plain ? data.files || [] : [];
    const comparison = data && (data.base || base) || "";
    const baseName = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/i.test(comparison)
        ? comparison.slice(0, 7) : comparison.replace(/^refs\/(?:heads|remotes|tags)\//, "");
    const totals = files.reduce((sum, f) => ({ add: sum.add + f.add, delete: sum.delete + f.delete }), { add: 0, delete: 0 });
    return html`
        ${data && !plain && html`
            <div class="cdbranch">
                <b>${data.branch}</b><span>${data.total} changed</span>
                <span class="cdstat"><span class="plus">+${totals.add}</span> <span class="minus">−${totals.delete}</span></span>
                ${comparison && html`
                    <button class="cdcontrol cdbasepick" type="button" onClick=${() => repo.setBase(base ? "" : data.base)}
                            title=${`comparison base from ${data.baseFrom || "project"}: ${comparison}`}>
                        vs ${baseName}
                    </button>
                `}
            </div>
        `}
        <${FileFinder} cwd=${repo.cwd} focus=${repo.focus} active=${repo.open && repo.side === "files"}
                       onPick=${repo.openFile} />
        ${changes.kind === "loading" && html`<p class="hint">Reading the project…</p>`}
        ${changes.kind === "failed" && html`<p class="hint crit">${changes.error}</p>`}
        ${data && !plain && html`
            <h3 class="cdgroup">Changed <span>${data.total}</span>
                <span class="cdstat"><span class="plus">+${totals.add}</span> <span class="minus">−${totals.delete}</span></span>
            </h3>
            <div class="cdchanged">
                ${files.map((f) => html`
                    <button class=${`cdchangedfile${repo.file === f.path ? " on" : ""}`} type="button" key=${f.path}
                            data-path=${f.path} aria-current=${repo.file === f.path ? "true" : undefined} onClick=${() => repo.openFile(f.path)}>
                        <span class="cdchangedname"><b>${dirName(f.path)}</b><small>${f.path.split("/").slice(0, -1).join("/") || "/"}</small></span>
                        <${FileMarks} file=${f} />
                    </button>
                `)}
                ${!files.length && html`<p class="hint">Nothing has changed against <b>${data.base || "the base"}</b>.</p>`}
                ${data.cut && html`<p class="cdcut">Showing <b>${files.length}</b> of <b>${data.total}</b> changed files. Other files remain in All files.</p>`}
            </div>
        `}
        ${data && html`<${TreePane} repo=${repo} />`}
    `;
}

function FileFinder({ cwd, focus, active, onPick }) {
    const [q, setQ] = useState("");
    const [at, setAt] = useState(0);
    const box = useRef(null);
    const [state] = useAsk(() => findOf(cwd, q), [cwd, q], q.trim().length > 0);
    const data = state.kind === "ready" ? state.data : null;
    const paths = data && data.paths || [];
    useEffect(() => {
        if (focus && active && box.current) box.current.focus();
    }, [focus, active]);
    const choose = (path) => { onPick(path); setQ(""); };
    const keys = (e) => {
        if (e.key === "Escape") { setQ(""); box.current.blur(); }
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
            e.preventDefault();
            setAt((i) => Math.max(0, Math.min(paths.length - 1, i + (e.key === "ArrowDown" ? 1 : -1))));
        }
        if (e.key === "Enter" && paths[at]) { e.preventDefault(); choose(paths[at]); }
    };
    return html`
        <div class="cdfind">
            <label class="cdfindbox">${Icon.search()}
                <input class="cdfindin" ref=${box} type="text" value=${q} placeholder="Find a file"
                       aria-label="find a file by name" onKeyDown=${keys}
                       onInput=${(e) => { setQ(e.currentTarget.value); setAt(0); }} />
                <kbd>Ctrl K</kbd>
            </label>
            ${q.trim() && html`
                <div class="cdfindlist">
                    ${state.kind === "loading" && html`<p class="hint">Finding files…</p>`}
                    ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
                    ${state.kind === "ready" && !paths.length && html`<p class="hint">No name here carries that.</p>`}
                    ${paths.map((path, i) => html`
                        <button key=${path} type="button" class=${`cdfindrow${i === at ? " on" : ""}`}
                                onMouseEnter=${() => setAt(i)} onClick=${() => choose(path)}>
                            <span class="cdfindname">${dirName(path)}</span>
                            <span class="cdfinddir">${path.split("/").slice(0, -1).join("/")}</span>
                        </button>
                    `)}
                    ${data && data.total > paths.length && html`<p class="cdcut">Showing <b>${paths.length}</b> of <b>${data.total}</b> — type more of the name.</p>`}
                    ${data && data.partial && html`<p class="cdcut">The search stopped before it walked the whole directory — what sits deeper may be missing.</p>`}
                </div>
            `}
        </div>
    `;
}

function TreePane({ repo }) {
    const [where, setWhere] = useState("");
    const [expanded, setExpanded] = useState(new Set());
    const counts = useMemo(() => new Map((repo.data.files || []).map((f) => [f.path, f])), [repo.data.files]);
    const toggle = (path) => {
        const closing = expanded.has(path);
        setExpanded((was) => {
            const next = new Set(was);
            if (closing) next.delete(path); else next.add(path);
            return next;
        });
        if (!closing) setWhere(path);
        else if (where === path || where.startsWith(`${path}/`)) setWhere(path.split("/").slice(0, -1).join("/"));
    };
    const visit = (path) => {
        setWhere(path);
        if (repo.wide) setExpanded((was) => new Set([...was].filter((p) => path && !p.startsWith(`${path}/`))));
    };
    return html`
        <div class="cdtree">
            <${TreeDirectory} repo=${repo} path=${repo.wide ? "" : where} depth=${0}
                             where=${where} onWhere=${visit}
                             counts=${counts} expanded=${expanded} onDirectory=${repo.wide ? toggle : setWhere} />
        </div>
    `;
}

function TreeDirectory({ repo, path, depth, where, onWhere, counts, expanded, onDirectory }) {
    const [state] = useAsk(() => treeOf(repo.cwd, path), [repo.cwd, path, repo.open]);
    const data = state.kind === "ready" ? state.data : null;
    return html`
        ${depth === 0 && html`
            <h3 class="cdgroup cdallfiles">
                <button class="cdtreeroot" type="button" onClick=${() => onWhere("")}>All files</button>
                ${data && html`<span>${data.total}</span>`}
                ${where && html`
                    <span class="cdcrumbs"><span>·</span>
                        ${where.split("/").map((part, i, all) => html`
                            <button class="cdcrumb" type="button" key=${i}
                                    onClick=${() => onWhere(all.slice(0, i + 1).join("/"))}>${part}</button>
                        `)}
                    </span>
                    <button class="cdcontrol cdup" type="button" aria-label="up one directory"
                            onClick=${() => onWhere(where.split("/").slice(0, -1).join("/"))}>↑</button>
                `}
            </h3>
        `}
        ${state.kind === "loading" && html`<p class="hint">Reading the directory…</p>`}
        ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
        ${data && (data.entries || []).map((e) => {
            const full = path ? `${path}/${e.name}` : e.name;
            const changed = !repo.plain && counts.get(full);
            const inside = e.dir && !repo.plain ? [...counts.keys()].filter((p) => p.startsWith(`${full}/`)).length : 0;
            const unfolded = repo.wide && expanded.has(full);
            return html`
                <div key=${e.name}>
                    <button class=${`cdnode${repo.file === full ? " on" : ""}`} type="button" data-path=${full}
                            aria-current=${repo.file === full ? "true" : undefined} aria-expanded=${e.dir && repo.wide ? unfolded : undefined}
                            style=${depth ? `padding-left:${12 + depth * 14}px` : undefined}
                            onClick=${() => e.dir ? onDirectory(full) : repo.openFile(full)}>
                        <span class="cdcaret">${e.dir ? unfolded ? "▾" : "▸" : ""}</span>
                        <span class="cdnodeicon">${e.dir ? Icon.files() : Icon.file()}</span>
                        <span class=${`cdnm${e.dir ? " cddirectory" : ""}`}>${e.name}</span>
                        ${inside > 0 && html`<span class="cdinside" aria-label="changed files inside">${inside}</span>`}
                        ${changed && html`<${FileMarks} file=${changed} />`}
                    </button>
                    ${e.dir && unfolded && html`<${TreeDirectory} repo=${repo} path=${full} depth=${depth + 1}
                        counts=${counts} expanded=${expanded} onDirectory=${onDirectory} />`}
                </div>
            `;
        })}
        ${data && data.cut && html`<p class="cdcut">Showing <b>${(data.entries || []).length}</b> of <b>${data.total}</b> names.</p>`}
    `;
}

// FileBody is one file, whole: the window a screen reads it by, with the next
// one a tap away rather than a scroll that never ends.
function FileBody({ cwd, path, base, wide, branch, changed, noting, jump, notes, onStale, onConversation, onFeed, onNotes, wrap, onWrap }) {
    const [first, setFirst] = useState(() => windowFor(jump && jump.line));
    const [mode, setMode] = useState(() => changed && !jump ? "diff" : "file");
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
        if (jump) {
            setFirst(windowFor(jump.line));
            setMode("file");
        }
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

    return html`
        <div class="cdstrip cdfilebar">
            <button class="cdcontrol cdtofeed" type="button" onClick=${onFeed}>‹ ${wide ? "Feed" : "Files"}</button>
            <span class="cdfilepath" title=${path}>${path.split("/").map((part, i) => html`${i > 0 && html`<span class="cdslash"> / </span>`}${part}`)}</span>
            ${changed && html`<${FileMarks} file=${changed} />`}
            ${branch && html`
                <span class="cdmodes" role="group" aria-label="file or changes">
                    <button class="cdcontrol" type="button" aria-pressed=${mode !== "diff"}
                            onClick=${() => setMode("file")}>File</button>
                    <button class="cdcontrol" type="button" aria-pressed=${mode === "diff"}
                            onClick=${() => setMode("diff")}>Changes</button>
                </span>
                <button class="cdcontrol cdnotetab" type="button" onClick=${onNotes}>Notes <b>${notes.length}</b></button>
                <button class="cdcontrol" type="button" aria-pressed=${mode === "blame"}
                        onClick=${() => setMode(mode === "blame" ? "file" : "blame")}>Who wrote</button>
            `}
            <button class="cdcontrol cdwrap" type="button" aria-pressed=${wrap}
                    title="wrap long lines" aria-label="wrap long lines" onClick=${onWrap}>${Icon.wrap()}</button>
            ${wide && mode === "diff" && html`
                <button class="cdcontrol cdsplitpick" type="button" aria-pressed=${split}
                        onClick=${() => setSplit((s) => !s)} title="before and after, side by side">
                    ${split ? "Side by side" : "In one column"}
                </button>
            `}
        </div>

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
