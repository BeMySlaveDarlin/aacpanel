// Finding words in a conversation: the bar over the feed, the question to the
// host, the walk from one match to the next, and the marks on the matches the
// feed has drawn.
//
// The host searches the whole transcript, not the rows on screen: a match the
// feed has not loaded is reached through a window around it (see show in
// feedwindow.js). The marks are the CSS Custom Highlight API — ranges over the
// text the rows already hold, so the rows are never rebuilt for them and
// nothing a row does with its text (a code block coloured later, a card
// redrawn) fights with them.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useBackClose } from "../../ui/back.js";
import { idParam } from "./api.js";

// The search waits this long after the last keystroke: a question typed
// letter by letter asks the host once, not once a letter.
export const FIND_WAIT_MS = 250;

// What the host takes: the question stripped, in characters, not UTF-16 units.
const SHORTEST = 2;
const LONGEST = 200;

// The names the marks live under in CSS.highlights: every match the feed
// shows, and the one the bar stands on.
export const MARK_ALL = "feedfind";
export const MARK_NOW = "feedfindnow";

// The rows whose words the host searches: what the person and the model said,
// letters — the task of a thread of codex among them — and the cards. Calls, thoughts and the lines of the harness are not
// searched, so they are not marked either — a mark the count does not know of
// reads as a match the walk never reaches.
const SEARCHED = ".feedrow:is(.r-me, .r-ai, .r-mail, .r-task, .r-brief, .r-secret, .r-artifact)[data-pos]";

// The rows a match of each role stands in. One record can draw several rows
// at one position — a letter beside an answer, the letters of several agents
// at once — so a row is found by its position, its role and its number among
// the rows of that role in the record (nth, absent on the first).
const ROWS_OF = {
    me: ["me"],
    assistant: ["ai"],
    letter: ["mail", "task"],
    card: ["brief", "artifact", "secret"],
};

const rowOf = (el, match) => el.dataset.pos === String(match.pos)
    && Number(el.dataset.nth || 0) === (match.nth || 0)
    && (ROWS_OF[match.role] || []).some((role) => el.classList.contains(`r-${role}`));

const sameItem = (a, b) => a.pos === b.pos && a.role === b.role && (a.nth || 0) === (b.nth || 0);

// What a row draws around its words and is not the conversation: the time
// under a message, a note that it was cut, the state of one on its way, the
// head of a card or a letter, who a letter is from, the buttons of a card.
const CHROME = ".mstamp, .hint, .mwait, .magain, .senthead, .mletterwho, .mlettermore, .sksaved, .skfill";

// Blocks: the text of two of them meets with a space between, the way the host
// reads the line break between two paragraphs.
const BLOCKS = new Set(["P", "LI", "PRE", "H1", "H2", "H3", "H4", "H5", "H6", "BLOCKQUOTE",
    "TD", "TH", "TR", "DIV", "DT", "DD", "SUMMARY", "TABLE", "UL", "OL", "SECTION", "HEADER", "FOOTER"]);

const canMark = () => typeof CSS !== "undefined" && Boolean(CSS.highlights) && typeof Highlight === "function";

// lower folds one UTF-16 unit for matching and keeps its length: the offsets in
// the folded text are the offsets in the node. A letter whose lower case is
// longer stays as it is.
function lower(ch) {
    const low = ch.toLowerCase();
    return low.length === 1 ? low : ch;
}

// folded is the question as it is matched: stripped, whitespace one space,
// lower case.
export function folded(q) {
    return Array.from(String(q || "").trim().replace(/\s+/g, " "), lower).join("");
}

// textOf reads a row the way the host reads an item: every run of whitespace
// one space, a space where a block or a line break parts the words. at maps
// every character back to its node and offset; null is a space no node holds.
function textOf(row) {
    let text = "";
    const at = [];
    const space = (spot) => {
        if (!text || text[text.length - 1] === " ") return;
        text += " ";
        at.push(spot);
    };
    const walker = document.createTreeWalker(row, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
        acceptNode: (node) => (node.nodeType === 1 && node.matches(CHROME)
            ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
    });
    let block = null;
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (node.nodeType === 1) {
            if (node.tagName === "BR") space(null);
            continue;
        }
        const own = blockOf(node, row);
        if (block && own !== block) space(null);
        block = own;
        const data = node.data;
        for (let i = 0; i < data.length; i++) {
            if (/\s/.test(data[i])) {
                space([node, i]);
                continue;
            }
            text += lower(data[i]);
            at.push([node, i]);
        }
    }
    return { text, at };
}

function blockOf(node, row) {
    for (let el = node.parentElement; el && el !== row; el = el.parentElement) {
        if (BLOCKS.has(el.tagName)) return el;
    }
    return row;
}

// rangesIn returns a range over every match of the question in the row, in
// the order they are read.
function rangesIn(row, needle) {
    const { text, at } = textOf(row);
    const out = [];
    for (let i = text.indexOf(needle); i >= 0 && needle; i = text.indexOf(needle, i + needle.length)) {
        let s = i;
        let e = i + needle.length - 1;
        while (s <= e && !at[s]) s += 1;
        while (e >= s && !at[e]) e -= 1;
        if (s > e) continue;
        const range = document.createRange();
        range.setStart(at[s][0], at[s][1]);
        range.setEnd(at[e][0], at[e][1] + 1);
        out.push(range);
    }
    return out;
}

// paint marks every match the feed shows and the current one over them, and
// returns what the current match is drawn as: its range, or its row when its
// words are not where the feed draws them — a message cut short, a card that
// shows a title only. Without the API there is no range to mark, and the row
// of the current match is outlined instead.
export function paint(box, q, current, matches) {
    const needle = folded(q);
    const all = [];
    const mine = [];
    let row = null;
    for (const el of box.querySelectorAll(SEARCHED)) {
        const hits = rangesIn(el, needle);
        if (current && rowOf(el, current)) {
            if (!row) row = el;
            mine.push(...hits);
            continue;
        }
        all.push(...hits);
    }
    // One item with several hits is one match a hit, in the order of the text:
    // the current one is the hit of its rank among the matches of its item.
    let now = null;
    if (current && mine.length) {
        const rank = matches.filter((m) => sameItem(m, current)).indexOf(current);
        now = mine[Math.min(Math.max(rank, 0), mine.length - 1)];
    }
    for (const range of mine) if (range !== now) all.push(range);

    const api = canMark();
    if (api) {
        CSS.highlights.set(MARK_ALL, new Highlight(...all));
        if (now) {
            const strong = new Highlight(now);
            strong.priority = 1;
            CSS.highlights.set(MARK_NOW, strong);
        } else {
            CSS.highlights.delete(MARK_NOW);
        }
    }
    const outline = row && (!api || !now) ? row : null;
    for (const el of box.querySelectorAll(".feedrow.findnow")) {
        if (el !== outline) el.classList.remove("findnow");
    }
    if (outline) outline.classList.add("findnow");
    return now || row;
}

// unpaint takes every mark off.
export function unpaint(box) {
    if (canMark()) {
        CSS.highlights.delete(MARK_ALL);
        CSS.highlights.delete(MARK_NOW);
    }
    if (!box) return;
    for (const el of box.querySelectorAll(".feedrow.findnow")) el.classList.remove("findnow");
}

// center scrolls the box so the match stands in the middle of it. A row
// taller than the box is shown from its top: its middle may be words away
// from the match.
function center(box, target) {
    if (!box || !target) return;
    const r = target.getBoundingClientRect();
    const b = box.getBoundingClientRect();
    const tall = r.height > box.clientHeight * 0.8;
    box.scrollTop += tall
        ? r.top - b.top - 16
        : (r.top + r.height / 2) - (b.top + box.clientHeight / 2);
}

// countOf is what the bar says of the answer: "N of M", "N of M+" when the
// host cut the list, "No matches", or nothing while there is no answer to the
// words in the field yet.
function countOf(found, ask, at) {
    if (!found || found.q !== ask) return Array.from(ask).length >= SHORTEST ? "…" : "";
    if (found.error) return "—";
    if (!found.matches.length) return "No matches";
    return `${at} of ${found.matches.length}${found.cut ? "+" : ""}`;
}

// useFeedFind holds the search of one conversation: whether the bar is open,
// the question and its answer, the match the bar stands on, and Ctrl+F.
//
// on says the feed is on screen; under is the back-close handle of the
// conversation, whose isTop says nothing stands over it. show and feedRef come
// from the feed window.
export function useFeedFind({ name, id, feedRef, show, on, under }) {
    const [open, setOpen] = useState(false);
    const [q, setQ] = useState("");
    const [found, setFound] = useState(null);
    // The match the bar stands on: 1 is the newest. step counts the moves, so
    // a move onto the same match — the only one, walked round — goes to it again.
    const [at, setAt] = useState(0);
    const [step, setStep] = useState(0);
    const [note, setNote] = useState("");
    const field = useRef(null);
    const barTop = useRef(() => false);
    const asked = useRef(0);
    const showRef = useRef(show);
    showRef.current = show;

    const ask = q.trim();
    const matches = (found && found.q === ask && found.matches) || [];
    const current = at > 0 && at <= matches.length ? matches[matches.length - at] : null;

    // start opens the bar, or takes the focus back to the bar already open and
    // selects its words, so the next question is typed over the last one.
    const start = () => {
        if (open && field.current) {
            field.current.focus();
            field.current.select();
            return;
        }
        setOpen(true);
    };
    const close = () => setOpen(false);
    const startRef = useRef(start);
    startRef.current = start;

    useEffect(() => {
        setOpen(false);
        setQ("");
        setFound(null);
        setAt(0);
    }, [name, id]);

    useEffect(() => {
        if (!on) setOpen(false);
    }, [on]);

    // Ctrl+F and Cmd+F find in the conversation, not in the page: the
    // browser's own find sees only the rows loaded, and marks over them what
    // the feed would mark again. Taken only while nothing stands over the
    // conversation — a sheet open over it keeps the browser's find for itself.
    useEffect(() => {
        if (!on) return undefined;
        const key = (e) => {
            if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return;
            if (e.code !== "KeyF" && String(e.key).toLowerCase() !== "f") return;
            if (!under.isTop() && !barTop.current()) return;
            e.preventDefault();
            startRef.current();
        };
        window.addEventListener("keydown", key);
        return () => window.removeEventListener("keydown", key);
    }, [on]);

    // The question goes to the host a while after the last keystroke. Every
    // change of the words makes the answers to the earlier ones stale: a slow
    // answer to an old question may come after the answer to the new one, and
    // it would put back the matches of words no longer in the field.
    useEffect(() => {
        const mine = ++asked.current;
        if (!open || Array.from(ask).length < SHORTEST) {
            setFound(null);
            setAt(0);
            return undefined;
        }
        const timer = setTimeout(async () => {
            let body;
            try {
                const r = await fetch(`/api/chat/search?session=${encodeURIComponent(name)}${idParam(id)}`
                    + `&q=${encodeURIComponent(ask)}`);
                if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
                body = await r.json();
            } catch (e) {
                if (mine !== asked.current) return;
                setFound({ q: ask, matches: [], error: String(e.message || e) });
                setAt(0);
                return;
            }
            if (mine !== asked.current) return;
            const list = Array.isArray(body.matches) ? body.matches : [];
            setFound({ q: ask, matches: list, cut: Boolean(body.cut) });
            setAt(list.length ? 1 : 0);
            setStep((n) => n + 1);
        }, FIND_WAIT_MS);
        return () => clearTimeout(timer);
    }, [open, ask, name, id]);

    // The way to the match: the feed puts its row on screen — loading a
    // window around it if it is not among the rows loaded — and the box
    // scrolls it into the middle.
    useEffect(() => {
        if (!open || !current) return undefined;
        let alive = true;
        setNote("");
        showRef.current(current.pos)
            .then((shown) => {
                if (!alive || !shown) return;
                const box = feedRef.current;
                if (box) center(box, paint(box, found.q, current, found.matches));
            })
            .catch((e) => {
                if (alive) setNote(`The match did not load: ${String(e.message || e)}`);
            });
        return () => { alive = false; };
    }, [open, current, step]);

    // The marks follow the rows: a page loaded above, a block of code coloured
    // after it was drawn, a card redrawn — the ranges are made again over the
    // text as it stands, once a frame at most.
    useEffect(() => {
        const box = feedRef.current;
        if (!open || !on || !box || !matches.length) {
            unpaint(box);
            return undefined;
        }
        let frame = 0;
        const again = () => {
            if (frame) return;
            frame = requestAnimationFrame(() => {
                frame = 0;
                paint(box, found.q, current, found.matches);
            });
        };
        again();
        const watch = new MutationObserver(again);
        watch.observe(box, { childList: true, subtree: true, characterData: true });
        return () => {
            watch.disconnect();
            cancelAnimationFrame(frame);
            unpaint(box);
        };
    }, [open, on, found, current]);

    const move = (by) => {
        const n = matches.length;
        if (!n) return;
        setAt((was) => ((was - 1 + by + n) % n) + 1);
        setStep((s) => s + 1);
    };

    return {
        open, start, close, q, setQ, field, barTop, note,
        count: countOf(found, ask, at),
        error: found && found.q === ask ? found.error || "" : "",
        has: matches.length > 0,
        older: () => move(1),
        newer: () => move(-1),
    };
}

// FindBar is the bar over the feed. It holds the back gesture while it is
// open: on a phone the gesture puts the bar down, not the conversation.
export function FindBar({ find }) {
    const back = useBackClose(find.open, find.close);
    find.barTop.current = back.isTop;

    // The bar opens with its field in hand, and the words of the last search
    // selected: typed over, or walked again with Enter.
    useLayoutEffect(() => {
        if (!find.open || !find.field.current) return;
        find.field.current.focus();
        find.field.current.select();
    }, [find.open]);

    if (!find.open) return null;

    const keys = (e) => {
        // Esc here is the bar's: the screen under it does not close a panel of
        // its own on the same key.
        if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            find.close();
            return;
        }
        if (e.key === "Enter" && e.target === find.field.current) {
            e.preventDefault();
            if (e.shiftKey) find.newer();
            else find.older();
        }
    };
    return html`
        <div class="findbar" role="search" onKeyDown=${keys}>
            <span class="findbaricon" aria-hidden="true">${Icon.search()}</span>
            <input ref=${find.field} class="findbarin" type="text" value=${find.q}
                   maxLength=${LONGEST} placeholder="Find in the conversation"
                   aria-label="find in the conversation" autocomplete="off" spellcheck=${false}
                   enterkeyhint="search"
                   onInput=${(e) => find.setQ(e.currentTarget.value)} />
            <span class="findbarn" aria-live="polite" title=${find.error || undefined}>${find.count}</span>
            <button type="button" class="findbarbtn findolder" aria-label="older match (Enter)"
                    disabled=${!find.has} onClick=${find.older}>${Icon.chevron()}</button>
            <button type="button" class="findbarbtn findnewer" aria-label="newer match (Shift+Enter)"
                    disabled=${!find.has} onClick=${find.newer}>${Icon.chevron()}</button>
            <button type="button" class="findbarbtn findclose" aria-label="close the search"
                    onClick=${find.close}>${Icon.close()}</button>
        </div>
        ${(find.error || find.note) && html`<p class="findbarnote">${find.error || find.note}</p>`}
    `;
}
