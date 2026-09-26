// The tools of a session on a phone. The header keeps who the session is and
// how it stands; what is done to it lives here, each under a word of its own
// and saying why it cannot be done, when it cannot. Moving the session to the
// other side is a line of its own with what stops on the way: it closes the
// process and resumes the conversation elsewhere, which a switch of what to
// watch never does.

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { useAction } from "../../actions/gate.js";
import { hostLabel } from "../../actions/registry.js";
import { share, tokens } from "../../format.js";
import { copyText } from "./copy.js";
import { shortPath } from "./head.js";
import { useRemote } from "./remote.js";
import { moveSession, stops } from "./switch.js";
import { windowOf } from "./window.js";

// MoreButton opens the tools of the session from the header.
export function MoreButton({ onOpen }) {
    return html`
        <button class="pmore" type="button" aria-label="tools of the session" onClick=${onOpen}>
            ${Icon.more()}
        </button>
    `;
}

// SessionTools lists what can be done to the conversation. Every press that
// asks for a confirmation puts the list down first: the confirmation is a
// sheet of its own, and two sheets over the run fight for the way back.
export function SessionTools({ name, live, archive, pct, exec, snapshot, cwd, view, sides, win, way, work,
    onRepo, onPick, onDone, onWindow }) {
    const toast = useToast();
    const run = useAction();
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
            <ul class="cmdrows mcpfacts">${facts(live, archive, pct)}</ul>
            <ul class="mcplist toollist">
                ${onRepo && html`
                    <li><button type="button" class="mcprow toolrow" onClick=${onRepo}>
                        <span class="toolicon">${Icon.files()}</span>
                        <span class="toollabel">Files of the project</span>
                        <span class="crgo">${Icon.chevron()}</span>
                    </button></li>
                `}
                ${live && sides.pair && !sides.moves && html`<${Watch} view=${view} note=${sides.tip} onPick=${onPick} />`}
                ${live && html`<${WindowLine} name=${name} live=${live} work=${work} exec=${exec} win=${win} way=${way}
                                              run=${run} onDone=${onDone} onChange=${onWindow} />`}
                ${live && html`<${RemoteLine} name=${name} live=${live} exec=${exec} snapshot=${snapshot} />`}
            </ul>
            ${live && sides.moves && html`<${Move} name=${name} live=${live} exec=${exec} sides=${sides} work=${work}
                                                  win=${win} way=${way} run=${run} onDone=${onDone} />`}
        </div>
    `;
}

function facts(live, archive, pct) {
    if (live) {
        const of = live.limit ? ` of ${tokens(live.limit)}` : "";
        return html`
            <li><span class="cmdname">Context</span>
                <span class="cmdtok">${pct == null ? "—" : share(pct)}${live.tokens ? ` · ${tokens(live.tokens)}${of}` : ""}</span></li>
        `;
    }
    return html`
        <li><span class="cmdname">Context peak</span><span class="cmdtok">${pct == null ? "—" : share(pct)}</span></li>
        <li><span class="cmdname">State</span><span class="cmdtok">the conversation is closed</span></li>
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

// WindowLine opens and closes the window on the host. A session in the feed
// has no window of its own: that way goes through the move, below.
function WindowLine({ name, live, work, exec, win, way, run, onDone, onChange }) {
    const it = windowOf({ name, live, work, exec, win, way, run, onChange });
    if (it.moves) return null;
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
function RemoteLine({ name, live, exec, snapshot }) {
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
    `;
}

// Move takes the session to the other side: the process closes and the
// conversation is resumed there. What runs inside the process ends with it,
// and the line names it before the press, not after.
function Move({ name, live, exec, sides, work, win, way, run, onDone }) {
    const to = sides.moves;
    const lost = stops(work);
    const go = (withWindow) => {
        onDone();
        if (withWindow) {
            windowOf({ name, live, work, exec, win, way, run }).press();
            return;
        }
        moveSession({ run, exec, name, to, work });
    };
    const off = Boolean(sides.why);
    return html`
        <section class="toolmove">
            <div class="toolline">
                <span class="toolicon">${Icon.exit()}</span>
                <span class="toolbody">
                    <span class="toollabel">${to === "console" ? "Move to the console" : "Move to the feed"}</span>
                    ${off && html`<span class="toolnote why">Not now: ${sides.why}</span>`}
                    ${lost && html`<span class="toolnote stops">Stops: ${lost}</span>`}
                </span>
            </div>
            <div class="toolacts">
                <button type="button" class="btn" disabled=${off} onClick=${() => go(false)}>Move</button>
                ${to === "console" && html`
                    <button type="button" class="btn" disabled=${off} onClick=${() => go(true)}
                            aria-label=${`Move and open a window on ${hostLabel()}`}>Move with a window</button>
                `}
            </div>
        </section>
    `;
}
