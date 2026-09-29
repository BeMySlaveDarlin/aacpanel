// The "Terminals" tab: a page per place, paged by swipe the way the sessions
// page contours, and the terminal of a place opened over them with the tabs
// of the place instead of a head.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Icon } from "../ui/icons.js";
import { Sheet } from "../ui/sheet.js";
import { shortPath } from "./chat/head.js";
import { Term } from "./chat/term.js";
import { Pages, useProfilePage } from "./sessions/pages.js";
import { samePlace, tabName, useTerms } from "../data/terms.js";
import {
    afterClose, homeOf, pickOf, PLACE_KEY, placeLabel, placesOf, SHOW_KEY, tabsOf,
} from "./terms/places.js";
import { useTermActs } from "./terms/acts.js";
import { CloseTab, NewButton, PlacePicker, RenameTab, TabMenu, TermCard, TermTabs } from "./terms/parts.js";

// Terminals renders the tab. want is a terminal another screen sent here —
// the button of a session opens the terminal of its project this way.
export function Terminals({ snapshot, exec, onLayer, want, onWanted }) {
    const { terms, error, reload } = useTerms();
    const acts = useTermActs(exec, reload);
    const home = homeOf(snapshot, terms);
    const places = placesOf(terms || [], home);
    const names = places.map((p) => p.place);
    const [current, pick] = useProfilePage(names, PLACE_KEY);
    const [open, setOpen] = useState(null);
    const [picking, setPicking] = useState(false);

    const show = (target) => {
        if (!target) return;
        const at = places.find((p) => samePlace(p.place, target.place));
        if (at) pick(at.place);
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
                // The place the terminal was opened in is the page to come back
                // to, whether it was opened from there or sent from a session.
                const at = places.find((p) => samePlace(p.place, open.place));
                if (at) pick(at.place);
                setOpen(null);
            }}
        />`;
    }

    const counts = new Map(places.map((p) => [p.place, p.terms.length]));
    return html`
        ${error && html`<p class="hint crit">${error}</p>`}
        ${terms === null && !error && html`<p class="empty">Reading the terminals…</p>`}
        ${terms !== null && html`
            <${Pages}
                names=${names}
                current=${current}
                onPick=${pick}
                live=${counts}
                label=${(place) => placeLabel(places, place)}
                what="places"
                keep=${SHOW_KEY}
                page=${(place) => html`
                    <${PlacePage}
                        key=${place}
                        entry=${places.find((p) => p.place === place)}
                        onNew=${() => setPicking(true)}
                        onOpen=${(t) => show({ id: t.id, place: t.place })}
                    />
                `}
            />
        `}
        <${Sheet} open=${picking} onClose=${() => setPicking(false)} label="new terminal">
            ${picking && html`<${PlacePicker} pick=${pickOf(snapshot && snapshot.profileMap, home)}
                                              why=${acts.can.start ? "" : acts.why.start} onPick=${startIn} />`}
        <//>
    `;
}

// PlacePage is the page of one place: its name and where it is, the button
// of a new terminal, and a card per terminal.
function PlacePage({ entry, onNew, onOpen }) {
    if (!entry) return null;
    return html`
        <div class="tpage">
            <div class="ttitle">
                <h2>${entry.label}</h2>
                <span class="ttitlepath" title=${entry.place}><bdi>${shortPath(entry.place)}</bdi></span>
                <${NewButton} onNew=${onNew} />
            </div>
            ${entry.terms.length === 0
                ? html`<p class="empty">No terminals here yet: New starts a shell in a place.</p>`
                : entry.terms.map((t) => html`<${TermCard} key=${t.id} t=${t} onOpen=${onOpen} />`)}
        </div>
    `;
}

// TermLayer is the terminal of a place on a phone: the place in the head, its
// tabs under it, the terminal itself, and what is done to the tab behind ⋯.
function TermLayer({ open, places, acts, onOpen, onNew, onBack }) {
    useBackClose(true, onBack);
    const [look, setLook] = useState("");
    const { entry, tabs, t } = tabsOf(places, open);

    // A closed tab gives way to the tab typed into last; the last one closed
    // puts the place down.
    const closed = (gone) => {
        const next = afterClose(entry, gone);
        if (next) onOpen({ id: next.id, place: open.place });
        else onBack();
    };

    return html`
        <${BackHead} kind="talk" onBack=${onBack} label="to the places"
                     tools=${html`
                         <button class="pmore" type="button" aria-label="what to do with the tab"
                                 onClick=${() => setLook("menu")}>${Icon.more()}</button>
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
            onPick=${(it) => onOpen({ id: it.id, place: open.place })}
            onNew=${() => onNew(open.place)}
        />
        <${Term} key=${open.id} term=${open.id} />
        <${Sheet} open=${Boolean(look)} onClose=${() => setLook("")} inner
                  label=${look === "rename" ? "rename the tab" : look === "close" ? "close the tab" : tabName(t)}>
            ${look === "menu" && html`<${TabMenu} t=${t} acts=${acts} onLook=${setLook}
                                                   onNew=${() => onNew(open.place)} onDone=${() => setLook("")} />`}
            ${look === "rename" && html`<${RenameTab} t=${t} acts=${acts} onDone=${() => setLook("")} />`}
            ${look === "close" && html`<${CloseTab} t=${t} acts=${acts} onClosed=${closed}
                                                    onDone=${() => setLook("")} />`}
        <//>
    `;
}
