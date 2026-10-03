// The terminals section of the wide screen: the places stand side by side as
// columns, the way the sessions stack contours, and a terminal opens over
// them with the tabs of its place in place of a head.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { useBackClose } from "../ui/back.js";
import { Icon } from "../ui/icons.js";
import { Sheet } from "../ui/sheet.js";
import { shortPath } from "../screens/chat/head.js";
import { Term } from "../screens/chat/term.js";
import { Pages, useProfilePage } from "../screens/sessions/pages.js";
import { useTerms } from "../data/terms.js";
import {
    homeOf, pickOf, PLACE_KEY, placeLabel, placesOf, SHOW_KEY, tabsOf,
} from "../screens/terms/places.js";
import { useTabClose, useTermActs } from "../screens/terms/acts.js";
import { CloseTab, NewButton, PlaceHead, PlacePicker, TermCard, TermTabs } from "../screens/terms/parts.js";

// TermsDesk is the centre of the section. open is the terminal on screen,
// kept by the shell so a terminal sent here from a conversation opens at once.
export function TermsDesk({ snapshot, exec, open, onOpen }) {
    const { terms, error, reload } = useTerms();
    const acts = useTermActs(exec, reload);
    const home = homeOf(snapshot, terms);
    const places = placesOf(terms || [], home);
    const names = places.map((p) => p.place);
    const [current, pick] = useProfilePage(names, PLACE_KEY);
    const [picking, setPicking] = useState(false);
    // asking is the id of the terminal whose card asked the question before
    // closing, since something runs in it. The terminal is read from the
    // list, so the question follows what runs in it.
    const [asking, setAsking] = useState("");
    const about = asking ? (terms || []).find((t) => t.id === asking) : null;
    const closing = useTabClose({ acts, ask: (t) => setAsking(t.id) });

    const startIn = async (place) => {
        setPicking(false);
        const made = await acts.start(place);
        if (made) onOpen(made);
    };

    const picker = html`
        <${Sheet} open=${picking} onClose=${() => setPicking(false)} label="new terminal">
            ${picking && html`<${PlacePicker} pick=${pickOf(snapshot && snapshot.profileMap, home)}
                                              why=${acts.can.start ? "" : acts.why.start} onPick=${startIn} />`}
        <//>
    `;

    if (open) {
        return html`<${DeskTerm} open=${open} places=${places} acts=${acts} onOpen=${onOpen}
                                  onNew=${startIn} onBack=${() => onOpen(null)} />`;
    }

    const counts = new Map(places.map((p) => [p.place, p.terms.length]));
    const entry = (place) => places.find((p) => p.place === place);
    return html`
        <section class="dkcenter dkterms">
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
                    row
                    tools=${html`<${NewButton} onNew=${() => setPicking(true)} />`}
                    head=${(place) => html`
                        <${PlaceHead} label=${placeLabel(places, place)} path=${shortPath(place)}
                                      count=${counts.get(place) || 0} />
                    `}
                    page=${(place) => {
                        const it = entry(place);
                        if (!it) return null;
                        return it.terms.length === 0
                            ? html`<p class="empty">No terminals here yet.</p>`
                            : it.terms.map((t) => html`
                                <${TermCard} key=${t.id} t=${t} acts=${acts} going=${closing.going}
                                             onOpen=${() => onOpen({ id: t.id, place: it.place })}
                                             onClose=${closing.press} />
                            `);
                    }}
                />
            `}
            ${picker}
            <${Sheet} open=${Boolean(about)} onClose=${() => setAsking("")} label="close the terminal">
                ${about && html`<${CloseTab} t=${about} acts=${acts} onClosed=${closing.closed}
                                             onDone=${() => setAsking("")} />`}
            <//>
        </section>
    `;
}

// DeskTerm is a terminal of a place on the wide screen: the way back to the
// places and the tabs of this one, each with its ×, where a head would be, the
// window at the end of the line, and the terminal under them.
function DeskTerm({ open, places, acts, onOpen, onNew, onBack }) {
    // The back of the browser puts the terminal down, as the crumb does.
    useBackClose(true, onBack);
    const { entry, tabs, t } = tabsOf(places, open);
    // asking is the id of the tab the question before closing is open for,
    // which need not be the open one: the × of any tab where something runs
    // asks it. The tab is read from the list, so the question follows what
    // runs in it.
    const [asking, setAsking] = useState("");
    const about = tabs.find((it) => it.id === asking);
    const closing = useTabClose({ acts, entry, open, onOpen, onBack, ask: (it) => setAsking(it.id) });

    return html`
        <section class="dkcenter dkterm">
            <div class="dkhead">
                <div class="dkheadtop">
                    <button class="dktab tback" type="button" aria-label="back to the places" onClick=${onBack}>
                        <span class="chev back">${Icon.chevron()}</span>${entry.label}
                    </button>
                    <${TermTabs} wide tabs=${tabs} current=${open.id} acts=${acts} going=${closing.going}
                                 onPick=${(it) => onOpen({ id: it.id, place: open.place })}
                                 onClose=${closing.press}
                                 onNew=${() => onNew(open.place)} />
                    <span class="tgrow"></span>
                    <button class="btn" type="button" disabled=${!acts.can.console}
                            data-tip=${acts.can.console ? "A window on the desktop of the machine, on the same tmux" : acts.why.console}
                            onClick=${() => acts.window(t)}>Open in a window</button>
                </div>
            </div>
            <${Term} key=${open.id} term=${open.id} />
            <${Sheet} open=${Boolean(about)} onClose=${() => setAsking("")} label="close the tab">
                ${about && html`<${CloseTab} t=${about} acts=${acts} onClosed=${closing.closed}
                                             onDone=${() => setAsking("")} />`}
            <//>
        </section>
    `;
}
