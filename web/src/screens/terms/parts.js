// The pieces the phone and the wide screen draw terminals of places with.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Chips } from "../../ui/chips.js";
import { Icon } from "../../ui/icons.js";
import { hostLabel } from "../../actions/registry.js";
import { runs, tabName, typed } from "../../data/terms.js";

// NAME_MAX is the longest name a tab takes.
export const NAME_MAX = 40;

// TermCard is a terminal on the page of its place: what it is called, whether
// something runs in it, when it was typed into last and the last line on its
// screen.
export function TermCard({ t, onOpen }) {
    const busy = runs(t);
    return html`
        <button class=${`tcard${busy ? " running" : ""}`} type="button" onClick=${() => onOpen(t)}
                aria-label=${`terminal ${tabName(t)}${busy ? ", something runs in it" : ""}`}>
            <span class=${`tdot${busy ? " run" : ""}`}></span>
            <span class="tcardname">${tabName(t)}</span>
            <span class="tcardwhen">${typed(t.activity)}</span>
            ${t.last && html`<span class="tcardlast">${t.last}</span>`}
        </button>
    `;
}

// PlaceHead heads the terminals of a place: its name, how many it holds and
// where it is.
export function PlaceHead({ label, path, count, tools }) {
    return html`
        <div class="thead">
            <span class="theadname">${label}</span>
            ${count > 0 && html`<span class="theadcount">${count}</span>`}
            <span class="theadpath" title=${path}><bdi>${path}</bdi></span>
            ${tools}
        </div>
    `;
}

// NewButton opens the choice of where a new terminal starts.
export function NewButton({ onNew }) {
    return html`
        <button class="btn primary tnew" type="button" onClick=${onNew}>
            ${Icon.plus()}<span>New</span>
        </button>
    `;
}

// TermTabs are the terminals of one place, the open one lit, and a tab that
// starts one more there. The phone draws them as words on a line, the wide
// screen as the segments its conversations switch views with.
export function TermTabs({ tabs, current, onPick, onNew, wide = false }) {
    const box = wide ? "dktabs ttabsdesk" : "ttabs";
    const one = wide ? "dktab" : "ttab";
    return html`
        <div class=${box} role="tablist" aria-label="terminals of the place">
            ${tabs.map((t) => html`
                <button key=${t.id} type="button" role="tab"
                        class=${`${one}${t.id === current ? " on" : ""}`}
                        aria-selected=${t.id === current ? "true" : "false"}
                        onClick=${() => t.id !== current && onPick(t)}>
                    ${runs(t) && html`<span class="tdot run"></span>`}
                    <span class="ttabname">${tabName(t)}</span>
                </button>
            `)}
            <button type="button" class=${`${one} tadd`} aria-label="a new tab in this place" onClick=${onNew}>
                ${Icon.plus()}
            </button>
        </div>
    `;
}

// PlacePicker asks where a new terminal starts: home at the top, the projects
// of the map under their contours. Given onContour, it offers the projects of
// one contour under home, with a chip per contour over them to switch it; a
// contour with none to offer gives way to the first that has some.
export function PlacePicker({ pick, why, onPick, contour = "", onContour = null }) {
    const contours = pick.filter((part) => part.contour);
    const chosen = contours.find((part) => part.contour === contour) || contours[0];
    const section = (part, head) => html`
        <section class="toolsec" key=${part.contour || "home"}>
            ${head && html`<div class="cmdsechead"><span>${part.contour}</span></div>`}
            <ul class="mcplist toollist">
                ${part.items.map((it) => html`
                    <li key=${it.place}>
                        <button type="button" class=${`mcprow toolrow${part.contour ? "" : " thome"}`}
                                disabled=${Boolean(why)} onClick=${() => onPick(it.place)}>
                            <span class="toolicon">${part.contour ? Icon.files() : Icon.orbit()}</span>
                            <span class="toollabel">${it.name}</span>
                            <span class="toolaside"><bdi>${it.path}</bdi></span>
                        </button>
                    </li>
                `)}
            </ul>
        </section>
    `;
    return html`
        <div class="cmdsheet tpick">
            <div class="shead cmdtitle"><span class="cmdhead">New terminal — where?</span></div>
            ${why && html`<p class="cmdnote">${why}</p>`}
            ${onContour
                ? html`
                    ${pick.filter((part) => !part.contour).map((part) => section(part, false))}
                    ${chosen && html`
                        <${Chips} items=${contours.map((part) => ({ id: part.contour, label: part.contour }))}
                                  current=${chosen.contour} onSelect=${onContour} />
                        ${section(chosen, false)}
                    `}
                `
                : pick.map((part) => section(part, Boolean(part.contour)))}
        </div>
    `;
}

// TabMenu is what is done to the open tab on a phone.
export function TabMenu({ t, acts, onLook, onNew, onDone }) {
    const row = (icon, label, note, press, off = "", danger = false) => html`
        <li><button type="button" class=${`mcprow toolrow${danger ? " toolstop" : ""}`} disabled=${Boolean(off)}
                    onClick=${() => { onDone(); press(); }}>
            <span class="toolicon">${icon()}</span>
            <span class="toolbody">
                <span class="toollabel">${label}</span>
                ${(off || note) && html`<span class=${`toolnote${off ? " why" : ""}`}>${off || note}</span>`}
            </span>
        </button></li>
    `;
    return html`
        <div class="cmdsheet tools">
            <div class="shead cmdtitle"><span class="cmdhead toolname">${tabName(t)}</span></div>
            <ul class="mcplist toollist">
                ${row(Icon.monitor, "Open in a window", `on ${hostLabel()}: the same tmux, the same shell`,
                    () => acts.window(t), acts.can.console ? "" : acts.why.console)}
                ${row(Icon.pencil, "Rename tab", "", () => onLook("rename"), acts.can.rename ? "" : acts.why.rename)}
                ${row(Icon.plus, "New tab here", "", onNew, acts.can.start ? "" : acts.why.start)}
                ${row(Icon.close, "Close tab", "ends the shell and what runs in it", () => onLook("close"),
                    acts.can.close ? "" : acts.why.close, true)}
            </ul>
        </div>
    `;
}

// RenameTab takes the name a tab keeps instead of the command running in it.
export function RenameTab({ t, acts, onDone }) {
    const [value, setValue] = useState(t.name || "");
    const [busy, setBusy] = useState(false);
    const next = value.trim();
    const trouble = !next ? "Enter a name." : next.length > NAME_MAX ? `A name is at most ${NAME_MAX} characters.` : "";
    const ready = acts.can.rename && !busy && !trouble && next !== t.name;

    const save = async () => {
        if (!ready) return;
        setBusy(true);
        const ok = await acts.rename(t, next);
        setBusy(false);
        if (ok) onDone();
    };

    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle"><span class="cmdhead">Rename the tab</span></div>
            <p class="cmdnote">The tab keeps this name instead of the command running in it.</p>
            <input class="rnname" type="text" aria-label="the new name of the tab" value=${value}
                   maxlength=${NAME_MAX} autocomplete="off" autocapitalize="off" spellcheck="false"
                   onInput=${(e) => setValue(e.currentTarget.value)}
                   onKeyDown=${(e) => { if (e.key === "Enter") { e.preventDefault(); save(); } }} />
            ${value && trouble && html`<p class="cmdnote stpwarn">${trouble}</p>`}
            ${!acts.can.rename && html`<p class="cmdnote">${acts.why.rename}</p>`}
            <div class="btnrow">
                <button class="btn" type="button" onClick=${onDone}>Cancel</button>
                <button class="btn primary" type="button" disabled=${!ready} onClick=${save}>
                    ${busy ? "Saving…" : "Save"}
                </button>
            </div>
        </div>
    `;
}

// CloseTab asks before a tab is closed: the shell ends, and whatever runs in
// it ends with it, so the question names what runs.
export function CloseTab({ t, acts, onClosed, onDone }) {
    const [busy, setBusy] = useState(false);
    const what = runs(t) ? `${t.command} runs in it and stops too` : "nothing runs in it but the shell";
    const close = async () => {
        if (busy) return;
        setBusy(true);
        const ok = await acts.close(t);
        setBusy(false);
        onDone();
        if (ok) onClosed(t);
    };
    return html`
        <div class="tclose">
            <div class="shead">
                <span class="dot crit"></span>
                <div>
                    <div class="stitle">Close ${tabName(t)}?</div>
                    <div class="ssub">${what}</div>
                </div>
            </div>
            <div class="warnline">
                The shell of the tab ends, and whatever runs in it ends with it: a command half done stays half
                done. Nothing on the panel brings the tab back.
            </div>
            ${!acts.can.close && html`<p class="cmdnote">${acts.why.close}</p>`}
            <div class="btnrow">
                <button class="btn" type="button" onClick=${onDone}>Cancel</button>
                <button class="btn danger" type="button" disabled=${!acts.can.close || busy} onClick=${close}>
                    Close tab
                </button>
            </div>
        </div>
    `;
}
