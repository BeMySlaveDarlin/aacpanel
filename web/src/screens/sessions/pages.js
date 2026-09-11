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

// useProfilePage returns the chosen contour and how to change it.
export function useProfilePage(names) {
    const [picked, setPicked] = useState(() => {
        try {
            return localStorage.getItem(PICK_KEY) || "";
        } catch {
            return "";
        }
    });

    const pick = useCallback((name) => {
        setPicked(name);
        try {
            localStorage.setItem(PICK_KEY, name);
        } catch {
        }
    }, []);

    return [names.includes(picked) ? picked : (names[0] || ""), pick];
}

// useProfilePicks returns which contours are shown on the wide screen.
export function useProfilePicks(names) {
    const [picks, setPicks] = useState(readShown);

    const toggle = useCallback((name) => {
        setPicks((prev) => saveShown(prev.includes(name)
            ? prev.filter((n) => n !== name)
            : [...prev, name]));
    }, []);

    const all = useCallback(() => setPicks(saveShown([])), []);

    return [picks.filter((name) => names.includes(name)), toggle, all];
}

function readShown() {
    try {
        const raw = JSON.parse(localStorage.getItem(SHOW_KEY) || "[]");
        return Array.isArray(raw) ? raw.filter((v) => typeof v === "string") : [];
    } catch {
        return [];
    }
}

function saveShown(next) {
    try {
        localStorage.setItem(SHOW_KEY, JSON.stringify(next));
    } catch {
    }
    return next;
}

// Pages renders the pager itself: the row of names above, the pages below it.
export function Pages({ names, current, onPick, live, page, onShown }) {
    const wide = useWide();
    const [picks, togglePick, showAll] = useProfilePicks(names);
    const shown = picks.length ? names.filter((name) => picks.includes(name)) : names;

    useEffect(() => {
        if (!wide || shown.length !== 1 || shown[0] === current) return;
        onPick(shown[0]);
    }, [wide, shown.join("\n"), current, onPick]);

    useEffect(() => {
        if (onShown) onShown(shown);
    }, [shown.join("\n"), onShown]);

    if (names.length < 2) return page(names[0] || "");

    if (wide) {
        return html`
            <div class="pfdesk">
                <${ContourPick}
                    names=${names}
                    picks=${picks}
                    live=${live}
                    onToggle=${togglePick}
                    onAll=${showAll}
                />
                ${shown.map((name) => html`
                    <div class="pfstack" key=${name}>
                        ${shown.length > 1 && html`
                            <div class="pfstackname">
                                <span class="pfstacktitle">${name}</span>
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
                        ${name}
                        ${live && live.get(name) > 0 && html`<span class="n">${live.get(name)}</span>`}
                    </button>
                `)}
            </div>

            <${Deck} names=${names} at=${at} onPick=${onPick} page=${page} />
        </div>
    `;
}

function ContourPick({ names, picks, live, onToggle, onAll }) {
    const [open, setOpen] = useState(false);
    const label = picks.length === 0
        ? `all (${names.length})`
        : picks.length === 1 ? picks[0] : `${picks.length} of ${names.length}`;

    return html`
        <div class="pfsel">
            <span class="pfsellabel">contours</span>
            <div class="pfdrop">
                <button
                    class="pfselbtn"
                    type="button"
                    aria-expanded=${open ? "true" : "false"}
                    onClick=${() => setOpen((v) => !v)}
                >
                    <span class="pfselname">${label}</span>
                    <span class="chev">${Icon.chevron()}</span>
                </button>
                ${open && html`
                    <div class="pfscrim" onClick=${() => setOpen(false)}></div>
                    <div class="pfmenu">
                        <button class="pfopt" type="button" onClick=${() => onAll()}>
                            <span class=${`pfcheck ${picks.length === 0 ? "on" : ""}`}></span>
                            <span>all contours</span>
                        </button>
                        ${names.map((name) => html`
                            <button
                                key=${name}
                                class="pfopt"
                                type="button"
                                onClick=${() => onToggle(name)}
                            >
                                <span class=${`pfcheck ${picks.includes(name) ? "on" : ""}`}></span>
                                <span class="pfoptname">${name}</span>
                                ${live && live.get(name) > 0 && html`<span class="n">${live.get(name)}</span>`}
                            </button>
                        `)}
                    </div>
                `}
            </div>
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

    const onScroll = (event) => {
        const el = event.currentTarget;
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
