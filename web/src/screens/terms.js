// The "Terminals" tab: a page per contour, paged by swipe the way the sessions
// page them, the terminals of each under the places they stand in, and the
// terminal of a place opened over them with the tabs of the place instead of
// a head.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Icon } from "../ui/icons.js";
import { Sheet } from "../ui/sheet.js";
import { shortPath } from "./chat/head.js";
import { Term } from "./chat/term.js";
import { Pages, useProfilePage } from "./sessions/pages.js";
import { tabName, useTerms } from "../data/terms.js";
import {
    CONTOUR_KEY, contoursOf, homeOf, pageOf, pickOf, placesOf, tabsOf,
} from "./terms/places.js";
import { useTabClose, useTermActs } from "./terms/acts.js";
import {
    CloseTab, NewButton, PlaceHead, PlacePicker, RenameTab, TabMenu, TermCard, TermTabs,
} from "./terms/parts.js";

// Terminals renders the tab. want is a terminal another screen sent here —
// the button of a session opens the terminal of its project this way.
export function Terminals({ snapshot, exec, onLayer, want, onWanted }) {
    const { terms, error, reload } = useTerms();
    const acts = useTermActs(exec, reload);
    const home = homeOf(snapshot, terms);
    const map = snapshot && snapshot.profileMap;
    const places = placesOf(terms || [], home);
    const pages = contoursOf(places, map, home);
    const names = pages.map((p) => p.key);
    const [current, pick] = useProfilePage(names, CONTOUR_KEY);
    const [open, setOpen] = useState(null);
    const [picking, setPicking] = useState(false);
    // The contour whose projects the choice of a place offers: the one of the
    // page New was pressed on, until a chip picks another.
    const [contour, setContour] = useState("");
    // asking is the id of the terminal whose card asked the question before
    // closing, since something runs in it. The terminal is read from the
    // list, so the question follows what runs in it.
    const [asking, setAsking] = useState("");
    const about = asking ? (terms || []).find((t) => t.id === asking) : null;
    const closing = useTabClose({ acts, ask: (t) => setAsking(t.id) });

    const show = (target) => {
        if (!target) return;
        pick(pageOf(pages, target.place));
        setOpen(target);
    };

    useEffect(() => {
        if (!want) return;
        show(want);
        if (onWanted) onWanted();
    }, [want, onWanted]);

    useEffect(() => {
        if (onLayer) onLayer(Boolean(open));
    }, [open, onLayer]);

    const startIn = async (place) => {
        setPicking(false);
        show(await acts.start(place));
    };

    if (open) {
        return html`<${TermLayer}
            open=${open}
            places=${places}
            acts=${acts}
            onOpen=${setOpen}
            onNew=${(place) => acts.start(place).then(show)}
            onBack=${() => {
                // The contour of the place the terminal was opened in is the
                // page to come back to, whether it was opened from there or sent
                // from a session.
                pick(pageOf(pages, open.place));
                setOpen(null);
            }}
        />`;
    }

    const counts = new Map(pages.map((p) => [p.key, p.count]));
    const pageBy = (key) => pages.find((p) => p.key === key);
    return html`
        ${error && html`<p class="hint crit">${error}</p>`}
        ${terms === null && !error && html`<p class="empty">Reading the terminals…</p>`}
        ${terms !== null && html`
            <${Pages}
                names=${names}
                current=${current}
                onPick=${pick}
                live=${counts}
                label=${(key) => pageBy(key).label}
                page=${(key) => html`
                    <${ContourPage}
                        key=${key}
                        page=${pageBy(key)}
                        acts=${acts}
                        going=${closing.going}
                        onNew=${() => {
                            setContour(pageBy(key).contour);
                            setPicking(true);
                        }}
                        onOpen=${(t) => show({ id: t.id, place: t.place })}
                        onClose=${closing.press}
                    />
                `}
            />
        `}
        <${Sheet} open=${picking} onClose=${() => setPicking(false)} label="new terminal">
            ${picking && html`<${PlacePicker} pick=${pickOf(map, home)} contour=${contour} onContour=${setContour}
                                              why=${acts.can.start ? "" : acts.why.start} onPick=${startIn} />`}
        <//>
        <${Sheet} open=${Boolean(about)} onClose=${() => setAsking("")} label="close the terminal">
            ${about && html`<${CloseTab} t=${about} acts=${acts} onClosed=${closing.closed}
                                         onDone=${() => setAsking("")} />`}
        <//>
    `;
}

// ContourPage is the page of one contour: its name, the button of a new
// terminal, and the places with terminals, each under a heading of its name
// and where it is.
function ContourPage({ page, acts, going, onNew, onOpen, onClose }) {
    if (!page) return null;
    return html`
        <div class="tpage">
            <div class="ttitle">
                <h2>${page.label}</h2>
                <${NewButton} onNew=${onNew} />
            </div>
            ${page.count === 0
                ? html`<p class="empty">No terminals here yet: New starts a shell in a place.</p>`
                : page.places.map((entry) => html`
                    <section class="tplace" key=${entry.place}>
                        <${PlaceHead} label=${entry.label} path=${shortPath(entry.place)} />
                        ${entry.terms.map((t) => html`
                            <${TermCard} key=${t.id} t=${t} acts=${acts} going=${going} onOpen=${onOpen}
                                         onClose=${onClose} />
                        `)}
                    </section>
                `)}
        </div>
    `;
}

// TermLayer is the terminal of a place on a phone: the place in the head, its
// tabs under it, the terminal itself, and what is done to the tab behind ⋯.
function TermLayer({ open, places, acts, onOpen, onNew, onBack }) {
    useBackClose(true, onBack);
    const { entry, tabs, t } = tabsOf(places, open);
    // look is the sheet over the terminal and the tab it is about: the menu
    // and the new name are of the open tab, the question before closing of
    // the tab it was asked for — the open one from the menu, any tab from its
    // ×. The tab is read from the list by its id, so the question follows
    // what runs in it while it is open.
    const [look, setLook] = useState(null);
    const lookAt = (what, it = t) => setLook({ what, id: it.id });
    const done = () => setLook(null);
    const about = look && tabs.find((it) => it.id === look.id);
    const what = about ? look.what : "";
    const closing = useTabClose({ acts, entry, open, onOpen, onBack, ask: (it) => lookAt("close", it) });

    return html`
        <${BackHead} kind="talk" onBack=${onBack} label="to the places"
                     tools=${html`
                         <button class="pmore" type="button" aria-label="what to do with the tab"
                                 onClick=${() => lookAt("menu")}>${Icon.more()}</button>
                     `}>
            <div class="chathead">
                <h2>${entry.label}</h2>
                <div class="chatsub">
                    <span class="chatpath" title=${open.place}><bdi>${shortPath(open.place)}</bdi></span>
                </div>
            </div>
        <//>
        <${TermTabs}
            tabs=${tabs}
            current=${open.id}
            acts=${acts}
            going=${closing.going}
            onPick=${(it) => onOpen({ id: it.id, place: open.place })}
            onClose=${closing.press}
            onNew=${() => onNew(open.place)}
        />
        <${Term} key=${open.id} term=${open.id} />
        <${Sheet} open=${Boolean(what)} onClose=${done} inner
                  label=${what === "rename" ? "rename the tab" : what === "close" ? "close the tab" : tabName(about)}>
            ${what === "menu" && html`<${TabMenu} t=${about} acts=${acts} onLook=${(next) => lookAt(next, about)}
                                                   onNew=${() => onNew(open.place)} onDone=${done} />`}
            ${what === "rename" && html`<${RenameTab} t=${about} acts=${acts} onDone=${done} />`}
            ${what === "close" && html`<${CloseTab} t=${about} acts=${acts} onClosed=${closing.closed}
                                                    onDone=${done} />`}
        <//>
    `;
}
