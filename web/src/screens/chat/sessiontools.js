// The tools of a session. What is done to it answers three questions: where
// it lives (the stream or the console, the window on the host, the move
// between them with what stops on the way), Remote Control, and the session
// itself (its name, what it runs on, how to find it again, how it ends). The
// sections are one piece for both screens: on a phone they fill the sheet
// behind the header's one button, on the wide screen they drop from the
// session button beside the views. Every line says in words why it cannot be
// done, when it cannot.

import { useCallback, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { Popover } from "../../ui/popover.js";
import { useToast } from "../../ui/toasts.js";
import { useAction } from "../../actions/gate.js";
import { hostLabel } from "../../actions/registry.js";
import { knows, whyNot } from "../../exec.js";
import { share, tokens } from "../../format.js";
import { copyText } from "./copy.js";
import { shortPath } from "./head.js";
import { useRemote } from "./remote.js";
import { moveSession, stops } from "./switch.js";
import { windowOf } from "./window.js";

// MoreButton opens the tools of the session from the header on a phone.
export function MoreButton({ onOpen }) {
    return html`
        <button class="pmore" type="button" aria-label="tools of the session" onClick=${onOpen}>
            ${Icon.more()}
        </button>
    `;
}

// SessionTools fills the sheet on a phone: who the session is and where it
// works, what to look at it with, then the sections of what is done to it.
export function SessionTools(props) {
    const { name, live, archive, pct, cwd, view, sides, onRepo, onPick } = props;
    const toast = useToast();
    const where = cwd ? shortPath(cwd) : "";
    return html`
        <div class="cmdsheet tools">
            <div class="shead cmdtitle">
                <span class="cmdhead toolname">${name}</span>
            </div>
            ${where && html`
                <div class="toolpath">
                    <span class="toolwhere">${where}</span>
                    <button class="cmdcopy" type="button" aria-label="copy the directory of the session"
                            onClick=${() => copyText(cwd, toast, "Copied", "the directory")}>${Icon.copy()}</button>
                </div>
            `}
            <ul class="cmdrows mcpfacts">${facts(live, pct)}</ul>
            <ul class="mcplist toollist">
                ${onRepo && html`
                    <li><button type="button" class="mcprow toolrow" onClick=${onRepo}>
                        <span class="toolicon">${Icon.files()}</span>
                        <span class="toollabel">Files of the project</span>
                        <span class="crgo">${Icon.chevron()}</span>
                    </button></li>
                `}
                ${live && sides.pair && !sides.moves && html`<${Watch} view=${view} note=${sides.tip} onPick=${onPick} />`}
            </ul>
            ${live ? html`<${SessionSections} ...${props} />`
                : archive && html`<p class="cmdnote">The conversation is closed: it can be resumed from the history.</p>`}
        </div>
    `;
}

function facts(live, pct) {
    if (live) {
        const of = live.limit ? ` of ${tokens(live.limit)}` : "";
        return html`
            <li><span class="cmdname">Context</span>
                <span class="cmdtok">${pct == null ? "—" : share(pct)}${live.tokens ? ` · ${tokens(live.tokens)}${of}` : ""}</span></li>
        `;
    }
    return html`
        <li><span class="cmdname">Context peak</span><span class="cmdtok">${pct == null ? "—" : share(pct)}</span></li>
    `;
}

// Watch picks what this device watches the session with. Nothing moves: the
// session stays where it lives.
function Watch({ view, note, onPick }) {
    const one = (id, label, icon) => html`
        <button type="button" class=${`toolseg${view === id ? " on" : ""}`} aria-pressed=${view === id}
                onClick=${() => onPick(id)}>${icon()}<span>${label}</span></button>
    `;
    return html`
        <li class="toolline">
            <span class="toolicon">${Icon.feed()}</span>
            <span class="toolbody">
                <span class="toollabel">Watch with</span>
                ${note && html`<span class="toolnote">${note}</span>`}
            </span>
            <span class="toolsegs" role="group" aria-label="what to watch the session with">
                ${one("feed", "Feed", Icon.feed)}
                ${one("term", "Terminal", Icon.terminal)}
            </span>
        </li>
    `;
}

// SessionSections lists what is done to a live session. Every press that asks
// for a confirmation puts the list down first: the confirmation is a sheet of
// its own, and two layers over the run fight for the way back.
export function SessionSections({ name, live, exec, snapshot, cwd, sides, win, way, work, onDone, onWindow, onLook }) {
    const run = useAction();
    return html`
        <section class="toolsec">
            <div class="cmdsechead"><span>Where it lives</span></div>
            <ul class="mcplist toollist">
                <${Place} live=${live} win=${win} />
                <${WindowLine} name=${name} live=${live} work=${work} exec=${exec} win=${win} way=${way}
                               run=${run} onDone=${onDone} onChange=${onWindow} />
            </ul>
            ${sides.moves && html`<${Move} name=${name} live=${live} exec=${exec} sides=${sides} work=${work}
                                          win=${win} way=${way} run=${run} onDone=${onDone} />`}
        </section>
        <section class="toolsec">
            <div class="cmdsechead"><span>Remote Control</span></div>
            <ul class="mcplist toollist">
                <${RemoteLine} name=${name} live=${live} exec=${exec} snapshot=${snapshot} />
            </ul>
        </section>
        <section class="toolsec">
            <div class="cmdsechead"><span>Session</span></div>
            <ul class="mcplist toollist">
                <${SessionLines} name=${name} live=${live} exec=${exec} cwd=${cwd} onDone=${onDone} onLook=${onLook} />
            </ul>
        </section>
        <section class="toolsec toolend">
            <ul class="mcplist toollist">
                <${EndLine} name=${name} live=${live} exec=${exec} work=${work} run=${run} onDone=${onDone} />
            </ul>
        </section>
    `;
}

// Place says where the session lives now.
function Place({ live, win }) {
    const stream = live.transport === "stream";
    const note = stream
        ? `the panel's feed: no terminal and no window on ${hostLabel()} here`
        : `tmux on ${hostLabel()}${win.kind === "open" ? " · a window shows it" : ""}`;
    return html`
        <li class="toolline">
            <span class="toolicon">${Icon.pin()}</span>
            <span class="toolbody">
                <span class="toollabel">${stream ? "On the stream" : "In the console"}</span>
                <span class="toolnote">${note}</span>
            </span>
        </li>
    `;
}

// WindowLine opens and closes the window on the host. A session on the
// stream has no window of its own: that way goes through the move.
function WindowLine({ name, live, work, exec, win, way, run, onDone, onChange }) {
    const it = windowOf({ name, live, work, exec, win, way, run, onChange });
    if (it.moves || live.transport === "stream") return null;
    const state = win.kind === "open" ? "open" : win.kind === "unknown" ? "" : "closed";
    return html`
        <li class="toolline">
            <span class="toolicon">${Icon.monitor()}</span>
            <span class="toolbody">
                <span class="toollabel">Window on ${hostLabel()}</span>
                <span class=${`toolnote${it.why ? " why" : ""}`}>${it.why || state}</span>
            </span>
            <button type="button" class="btn toolbtn" disabled=${Boolean(it.why)}
                    onClick=${() => { onDone(); it.press(); }}>${it.open ? "Close" : "Open"}</button>
        </li>
    `;
}

// RemoteLine is the bridge to the Claude app and claude.ai. Where the contour
// signs in with an account of its own the bridge would open where the phone
// does not look, and there is nothing to press — the line says so instead.
export function RemoteLine({ name, live, exec, snapshot }) {
    const rc = useRemote({ name, live, exec, snapshot });
    const where = live.remote ? live.remote.replace(/^https:\/\//, "") : "";
    const note = rc.contour
        ? `not available: contour ${rc.contour} signs in with an account of its own`
        : rc.why || (rc.shown ? `on${where ? ` · ${where}` : ""}` : "off");
    return html`
        <li class="toolline">
            <span class="toolicon">${Icon.remote()}</span>
            <span class="toolbody">
                <span class="toollabel">Remote Control</span>
                <span class=${`toolnote${rc.why ? " why" : rc.shown ? " on" : ""}`}>${note}</span>
            </span>
            ${!rc.contour && html`
                <button type="button" class="btn toolbtn" disabled=${rc.off} aria-pressed=${rc.shown}
                        onClick=${rc.press}>${rc.shown ? "Turn off" : "Turn on"}</button>
            `}
        </li>
        ${rc.shown && live.remote && html`
            <li><a class="mcprow toolrow" href=${live.remote} target="_blank" rel="noopener">
                <span class="toolicon">${Icon.globe()}</span>
                <span class="toollabel">Open in claude.ai</span>
                <span class="crgo">${Icon.chevron()}</span>
            </a></li>
        `}
    `;
}

// SessionLines names the session and says how to find it again.
function SessionLines({ name, live, exec, cwd, onDone, onLook }) {
    const toast = useToast();
    const id = live.sessionId || "";
    const resume = id ? `${cwd ? `cd ${cwd} && ` : ""}claude --resume ${id}` : "";
    const renameWhy = live.transport !== "stream"
        ? "a session in the console is renamed on its own screen, /rename with keys"
        : knows(exec, "session.rename") ? "" : whyNot(exec, "session.rename");
    const row = (icon, label, note, press, aside = "", off = "") => html`
        <li><button type="button" class="mcprow toolrow" disabled=${Boolean(off)} onClick=${press}>
            <span class="toolicon">${icon()}</span>
            <span class="toolbody">
                <span class="toollabel">${label}</span>
                ${(off || note) && html`<span class=${`toolnote${off ? " why" : ""}`}>${off || note}</span>`}
            </span>
            ${aside && html`<span class="toolaside">${aside}</span>`}
        </button></li>
    `;
    return html`
        ${row(Icon.pencil, "Rename…", "", () => { onDone(); onLook("rename"); }, "", renameWhy)}
        ${row(Icon.info, "Session info", "the model, the context, the tokens in and out",
            () => { onDone(); onLook("status"); }, "/status")}
        ${id && row(Icon.copy, "Copy the session ID", "", () => copyText(id, toast, "Copied", "Session ID"), id.slice(0, 8))}
        ${resume && row(Icon.copy, "Copy the resume command", `claude --resume ${id.slice(0, 8)}…`,
            () => copyText(resume, toast, "Copied", "the resume command"))}
    `;
}

// EndLine ends the session: a session of its own home starts over, any other
// closes and leaves its conversation to the history.
function EndLine({ name, live, exec, work, run, onDone }) {
    const kind = live.home ? "session.restart" : "session.close";
    const why = knows(exec, kind) ? "" : whyNot(exec, kind);
    const lost = stops(work);
    const note = live.home
        ? "the conversation ends and a new one starts with an empty context"
        : "the process ends; the conversation stays in the history";
    return html`
        <li><button type="button" class="mcprow toolrow toolstop" disabled=${Boolean(why)}
                    onClick=${() => { onDone(); run(kind, name, {}); }}>
            <span class="toolicon">${live.home ? Icon.refresh() : Icon.stop()}</span>
            <span class="toolbody">
                <span class="toollabel">${live.home ? "Restart the session" : "Close the session"}</span>
                <span class=${`toolnote${why ? " why" : ""}`}>${why || (lost ? `stops ${lost} · ${note}` : note)}</span>
            </span>
        </button></li>
    `;
}

// Move takes the session to the other side: the process closes and the
// conversation is resumed there. What runs inside the process ends with it,
// and the line names it before the press, not after. A window on the host
// comes with a move to the console, or not at all: on the stream there is
// nothing for it to show.
function Move({ name, live, exec, sides, work, win, way, run, onDone }) {
    const [withWindow, setWithWindow] = useState(false);
    const to = sides.moves;
    const lost = stops(work);
    const off = Boolean(sides.why);
    const windowWhy = whyNot(exec, "window.open");
    const go = () => {
        onDone();
        if (to === "console" && withWindow) {
            windowOf({ name, live, work, exec, win, way, run }).press();
            return;
        }
        moveSession({ run, exec, name, to, work });
    };
    return html`
        <div class="toolmove">
            <button type="button" class="btn toolgo" disabled=${off} onClick=${go}>
                ${Icon.exit()}<span>${to === "console" ? "Move to the console" : "Move to the feed"}</span>
            </button>
            ${to === "console" && html`
                <label class=${`toolcheck${windowWhy ? " off" : ""}`} title=${windowWhy || undefined}>
                    <input type="checkbox" checked=${withWindow} disabled=${off || Boolean(windowWhy)}
                           onChange=${(e) => setWithWindow(e.target.checked)} />
                    <span>Open a window on ${hostLabel()} after the move</span>
                </label>
            `}
            ${off && html`<span class="toolnote why">Not now: ${sides.why}</span>`}
            ${lost && html`<span class="toolnote stops">Stops ${lost} — they do not come back after the move</span>`}
        </div>
    `;
}

// ViewTabs is what the wide screen looks at the conversation with: the feed,
// the terminal, the files of the project. A view never moves the session:
// where the other side is reached only by a move, its tab opens the session
// panel on the move instead, and is marked for it.
export function ViewTabs({ view, sides, canTerm, onView, onRepo, onMove }) {
    const moving = (id) => id !== "files" && Boolean(sides.moves) && id !== sides.view;
    const tab = (id, label, press, shown = true) => shown && html`
        <button type="button" class=${`dktab${view === id ? " on" : ""}${moving(id) ? " moves" : ""}`}
                aria-pressed=${view === id}
                data-tip=${moving(id)
                    ? (sides.moves === "console" ? "The session is on the stream: move it to the console first" : "Move the session to the feed first")
                    : (id !== "files" && id !== view && sides.tip) || undefined}
                onClick=${press}>${label}${moving(id) ? html`<span class="dktabgo" aria-hidden="true">⇢</span>` : ""}</button>
    `;
    const pick = (id) => () => (moving(id) ? onMove() : onView(id));
    return html`
        <span class="dktabs" role="group" aria-label="what to look at the conversation with">
            ${tab("feed", "Feed", pick("feed"))}
            ${tab("term", "Terminal", pick("term"), canTerm || sides.moves === "console")}
            ${onRepo && tab("files", "Files", onRepo)}
        </span>
    `;
}

// SessionButton is the wide screen's one control for the session: its face
// says where the session lives, whether a window shows it and whether Remote
// Control is on; the panel under it holds the sections.
export function SessionButton(props) {
    const { live, win, open, onOpen } = props;
    const close = useCallback(() => onOpen(false), [onOpen]);
    const stream = live.transport === "stream";
    return html`
        <span class="dksessctl">
            <button type="button" class=${`dkplace${open ? " on" : ""}`} aria-expanded=${open}
                    aria-label="the session: where it lives, Remote Control and what can be done to it"
                    onClick=${() => onOpen(!open)}>
                <span class="dkplaceicon">${Icon.pin()}</span>
                <span>${stream ? "stream" : "console"}</span>
                ${!stream && win.kind === "open" && html`<span class="dkplacewin">window</span>`}
                ${live.remote && html`<span class="dkrc">RC</span>`}
                <span class="dkplacechev">${Icon.chevron()}</span>
            </button>
            <${Popover} open=${open} onClose=${close} label="the session">
                <div class="cmdsheet tools dkpanel">
                    <${SessionSections} ...${props} onDone=${close} />
                </div>
            <//>
        </span>
    `;
}
