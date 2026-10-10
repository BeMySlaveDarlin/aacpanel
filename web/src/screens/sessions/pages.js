// Profile pages: one per contour, paged by swipe.

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useWide } from "../../ui/wide.js";
import { plural } from "../../format.js";

const PICK_KEY = "aacpanel.sessions.profile";

const SHOW_KEY = "aacpanel.sessions.shown";

// pageNames returns which contours have a page of their own.
export function pageNames(profiles, limits) {
    const names = (profiles || []).map((p) => p.profile);
    if (names.length === 0) return names;

    const ids = new Set((profiles || []).map((p) => p.id).filter(Boolean));
    const seen = new Set(names);
    for (const c of (limits && limits.contours) || []) {
        if (!c.profile) continue;
        if (c.contour && ids.has(c.contour)) continue;
        if (seen.has(c.profile)) continue;
        seen.add(c.profile);
        names.push(c.profile);
    }
    return names;
}

function readPick(key) {
    try {
        return localStorage.getItem(key) || "";
    } catch {
        return "";
    }
}

// pageOf returns the kept choice while it still has a page, the first page
// otherwise.
function pageOf(picked, names) {
    return names.includes(picked) ? picked : (names[0] || "");
}

// pickedPage reads the contour the sessions page stands on, without a choice
// of its own: what useProfilePage starts with right now. A screen that only
// shows the contour reads it when it is drawn; a hook of its own would keep
// the contour it was mounted with after the page moved on.
export function pickedPage(names, key = PICK_KEY) {
    return pageOf(readPick(key), names);
}

// useProfilePage returns the chosen contour and how to change it. The pager
// pages places as well as contours; each keeps its choice under its own key.
export function useProfilePage(names, key = PICK_KEY) {
    const [picked, setPicked] = useState(() => readPick(key));

    const pick = useCallback((name) => {
        setPicked(name);
        try {
            localStorage.setItem(key, name);
        } catch {
        }
    }, [key]);

    return [pageOf(picked, names), pick];
}

// useProfilePicks returns which contours are shown on the wide screen.
export function useProfilePicks(names, key = SHOW_KEY) {
    const [picks, setPicks] = useState(() => readShown(key));

    const toggle = useCallback((name) => {
        setPicks((prev) => saveShown(key, prev.includes(name)
            ? prev.filter((n) => n !== name)
            : [...prev, name]));
    }, [key]);

    const all = useCallback(() => setPicks(saveShown(key, [])), [key]);

    return [picks.filter((name) => names.includes(name)), toggle, all];
}

function readShown(key) {
    try {
        const raw = JSON.parse(localStorage.getItem(key) || "[]");
        return Array.isArray(raw) ? raw.filter((v) => typeof v === "string") : [];
    } catch {
        return [];
    }
}

function saveShown(key, next) {
    try {
        localStorage.setItem(key, JSON.stringify(next));
    } catch {
    }
    return next;
}

const same = (name) => name;

// Pages renders the pager itself: the row of names above, the pages below it.
// A name is the key of a page; label is what the person reads for it, what
// is the word for the things paged, keep is where the wide screen keeps its
// choice of the ones shown. A pager given head draws a heading of its own
// over every column of the wide screen, and tools stand beside the choice —
// with either, the wide screen keeps its columns for a single page too.
// row lays the columns side by side instead of one under another.
export function Pages({
    names, current, onPick, live, page, onShown,
    label = same, what = "contours", keep = SHOW_KEY, head = null, tools = null, row = false,
}) {
    const wide = useWide();
    const [picks, togglePick, showAll] = useProfilePicks(names, keep);
    const shown = picks.length ? names.filter((name) => picks.includes(name)) : names;

    useEffect(() => {
        if (!wide || shown.length !== 1 || shown[0] === current) return;
        onPick(shown[0]);
    }, [wide, shown.join("\n"), current, onPick]);

    useEffect(() => {
        if (onShown) onShown(shown);
    }, [shown.join("\n"), onShown]);

    const framed = wide && (head || tools);
    if (names.length < 2 && !framed) return page(names[0] || "");

    if (wide) {
        return html`
            <div class=${`pfdesk${row ? " row" : ""}`}>
                <${ContourPick}
                    names=${names}
                    picks=${picks}
                    live=${live}
                    label=${label}
                    what=${what}
                    tools=${tools}
                    onToggle=${togglePick}
                    onAll=${showAll}
                />
                ${shown.map((name) => html`
                    <div class="pfstack" key=${name}>
                        ${head ? head(name) : shown.length > 1 && html`
                            <div class="pfstackname">
                                <span class="pfstacktitle">${label(name)}</span>
                                ${live && live.get(name) > 0 && html`
                                    <span class="pfstacklive">
                                        ${live.get(name)} live
                                    </span>
                                `}
                            </div>
                        `}
                        ${page(name)}
                    </div>
                `)}
            </div>
        `;
    }

    const at = Math.max(0, names.indexOf(current));
    return html`
        <div class="pfpager">
            <div class="pftabs">
                ${names.map((name) => html`
                    <button
                        key=${name}
                        class="pftab ${name === current ? "on" : ""}"
                        type="button"
                        aria-pressed=${name === current ? "true" : "false"}
                        onClick=${() => onPick(name)}
                    >
                        ${label(name)}
                        ${live && live.get(name) > 0 && html`<span class="n">${live.get(name)}</span>`}
                    </button>
                `)}
            </div>

            <${Deck} names=${names} at=${at} onPick=${onPick} page=${page} />
        </div>
    `;
}

function ContourPick({ names, picks, live, label, what, tools, onToggle, onAll }) {
    const [open, setOpen] = useState(false);
    const chosen = picks.length === 0
        ? `all (${names.length})`
        : picks.length === 1 ? label(picks[0]) : `${picks.length} of ${names.length}`;

    return html`
        <div class="pfsel">
            <span class="pfsellabel">${what}</span>
            <div class="pfdrop">
                <button
                    class="pfselbtn"
                    type="button"
                    aria-expanded=${open ? "true" : "false"}
                    onClick=${() => setOpen((v) => !v)}
                >
                    <span class="pfselname">${chosen}</span>
                    <span class="chev">${Icon.chevron()}</span>
                </button>
                ${open && html`
                    <div class="pfscrim" onClick=${() => setOpen(false)}></div>
                    <div class="pfmenu">
                        <button class="pfopt" type="button" onClick=${() => onAll()}>
                            <span class=${`pfcheck ${picks.length === 0 ? "on" : ""}`}></span>
                            <span>all ${what}</span>
                        </button>
                        ${names.map((name) => html`
                            <button
                                key=${name}
                                class="pfopt"
                                type="button"
                                onClick=${() => onToggle(name)}
                            >
                                <span class=${`pfcheck ${picks.includes(name) ? "on" : ""}`}></span>
                                <span class="pfoptname">${label(name)}</span>
                                ${live && live.get(name) > 0 && html`<span class="n">${live.get(name)}</span>`}
                            </button>
                        `)}
                    </div>
                `}
            </div>
            ${tools}
        </div>
    `;
}

function shift(box, kid) {
    return kid.getBoundingClientRect().left - box.getBoundingClientRect().left;
}

function Deck({ names, at, onPick, page }) {
    const box = useRef(null);
    const mine = useRef(null);

    useLayoutEffect(() => {
        const el = box.current;
        const kid = el && el.children[at];
        if (!kid || mine.current === names[at]) return;
        el.scrollTo({ left: el.scrollLeft + shift(el, kid), behavior: mine.current === null ? "auto" : "smooth" });
        mine.current = names[at];
    }, [at, names]);

    // A scroll reaches the pager with the next frame, so a pager that scrolled
    // and left the screen within one frame still gets its scroll. Out of the
    // document every page stands at zero: the first would be taken for the one
    // turned to, and the page kept would be lost.
    const onScroll = (event) => {
        const el = event.currentTarget;
        if (!el.isConnected) return;
        let best = 0;
        let gap = Infinity;
        for (let i = 0; i < el.children.length; i++) {
            const d = Math.abs(shift(el, el.children[i]));
            if (d < gap) {
                gap = d;
                best = i;
            }
        }
        if (names[best] && names[best] !== names[at]) {
            mine.current = names[best];
            onPick(names[best]);
        }
    };

    return html`
        <div class="pfpages" ref=${box} onScroll=${onScroll}>
            ${names.map((name) => html`
                <div class="pfpage" key=${name}>
                    <div class="pfcol">${page(name)}</div>
                </div>
            `)}
        </div>
    `;
}
