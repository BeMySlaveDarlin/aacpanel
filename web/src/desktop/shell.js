// The desktop shell: the section decides what stands left, centre and right.
import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { terminalOnScreen, typing } from "../ui/focus.js";
import { attachTips } from "./tip.js";
import { logout } from "../auth.js";
import { Chat } from "../screens/chat.js";
import { follow, liveOf } from "../catchup.js";
import { ChatEmpty } from "../screens/chat/empty.js";
import { Alerts } from "../screens/alerts.js";
import { HeadLoad } from "./load.js";
import { Devices } from "../screens/devices.js";
import { Settings } from "../screens/settings.js";
import { SessionColumn } from "./sessions.js";
import { TermsDesk } from "./terms.js";
import { useTermAvailable } from "../screens/chat/term.js";
import { StackColumn, ContainersCenter } from "./containers.js";
import { MachineCats, MachineCenter } from "./machine.js";
import { routeChip } from "../ui/route.js";
import { useToastHide } from "../ui/toasts.js";
import { Home } from "./home.js";
import { PANELS, RightPanel } from "./panels.js";
import { MapSettings } from "./settings.js";
import { MAX, MIN, useScale } from "./scale.js";

export const SECTIONS = [
    { id: "sessions", label: "Sessions", icon: Icon.sessions },
    { id: "terminals", label: "Terminals", icon: Icon.prompt },
    { id: "containers", label: "Containers", icon: Icon.containers },
    { id: "machine", label: "Machine", icon: Icon.cpu },
    { id: "devices", label: "Devices", icon: Icon.skill },
];

// The pages stand over a section rather than beside it: each has a way back,
// and it leads to the section the page was opened over.
const PAGES = ["devices", "alerts"];

// Every section the shell can stand on. A kept section that is not among them
// opens home.
const PLACES = ["home", ...SECTIONS.map((s) => s.id), "alerts"];

const PLACE_KEY = "aacpanel.desktop.place";

// talkOf keeps what names a conversation and drops what it was drawn from: an
// archive row is a copy of a list as it stood then, and the conversation opens
// by its id without one.
function talkOf(chat) {
    if (!chat || typeof chat.name !== "string" || !chat.name) return null;
    const talk = { name: chat.name, id: typeof chat.id === "string" && chat.id ? chat.id : null };
    if (chat.follow === true) talk.follow = true;
    if (chat.archived === true) talk.archived = true;
    return talk;
}

// lastPlace reads where the shell stood when the page was left, so a reload
// comes back to the same section and the same conversation instead of home.
// With nothing kept — the first visit — it is home.
function lastPlace() {
    const first = { section: "home", from: "", chat: null };
    try {
        const kept = JSON.parse(localStorage.getItem(PLACE_KEY) || "null");
        if (!kept || typeof kept !== "object") return first;
        const section = PLACES.includes(kept.section) ? kept.section : "home";
        const from = PAGES.includes(section) && PLACES.includes(kept.from) && !PAGES.includes(kept.from) ? kept.from : "";
        return { section, from, chat: talkOf(kept.chat) };
    } catch {
        return first;
    }
}

function keepPlace(section, from, chat) {
    try {
        localStorage.setItem(PLACE_KEY, JSON.stringify({ section, from, chat: talkOf(chat) }));
    } catch {
    }
}

function useJSON(url) {
    const [state, setState] = useState({ data: null, error: "" });
    useEffect(() => {
        let alive = true;
        fetch(url, { credentials: "same-origin" })
            .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
            .then((d) => alive && setState({ data: d, error: "" }))
            .catch((e) => alive && setState({ data: null, error: String(e.message || e) }));
        return () => { alive = false; };
    }, [url]);
    return state;
}

// Zoom is the pair of buttons that sets how large the shell is drawn, with
// the scale in force between them so a press has something to read against.
function Zoom({ scale, onSmaller, onBigger }) {
    return html`
        <div class="dkzoom">
            <button
                class="dkzoombtn"
                type="button"
                aria-label="Draw the interface smaller"
                disabled=${scale <= MIN}
                onClick=${onSmaller}
            >−</button>
            <span class="dkzoomnum">${Math.round(scale * 100)}%</span>
            <button
                class="dkzoombtn"
                type="button"
                aria-label="Draw the interface larger"
                disabled=${scale >= MAX}
                onClick=${onBigger}
            >+</button>
        </div>
    `;
}

// A button of the header is its icon and nothing else: the name of what it
// opens reaches a screen reader through aria-label, and a pointer reads the
// section that lights up after the press.
function IconButton({ item, active, onClick }) {
    return html`
        <button
            class=${`dkib${active ? " on" : ""}`}
            type="button"
            aria-label=${item.label}
            onClick=${onClick}
        ><${item.icon} /></button>
    `;
}

// The alerts bell stays in the header even with nothing open, so there is a
// way to the screen before anything breaks — the count on it is only how it
// says something is waiting, not the only door in.
function AlertsButton({ count, active, onClick }) {
    return html`
        <button
            class=${`dkib${active ? " on" : ""}`}
            type="button"
            aria-label=${count > 0 ? `Alerts, ${count} unread` : "Alerts"}
            onClick=${onClick}
        >
            <${Icon.alerts} />
            ${count > 0 && html`<span class="dkibnum">${count}</span>`}
        </button>
    `;
}

export function DesktopShell({
    snapshot, tree, treeError, hostError, ageSec, history, faults, alerts, openAlerts,
    exec, onRefresh, wait, theme, onTheme, updateReady, updating, onApplyUpdate, route, jump, onJumped,
}) {
    // Read once, as the shell starts: afterwards the state is the truth and
    // storage only follows it.
    const [start] = useState(lastPlace);
    // from is the section an open page goes back to, empty on a section.
    const [place, setPlace] = useState({ section: start.section, from: start.from });
    const { section, from } = place;
    const [chat, setChat] = useState(start.chat);
    useEffect(() => { keepPlace(section, from, chat); }, [section, from, chat]);
    const [stack, setStack] = useState(null);
    const [cont, setCont] = useState(null);
    const [cat, setCat] = useState("cpu");
    const [panel, setPanel] = useState(null);

    // The note about an action belongs to the section and the conversation it was taken in.
    const hideToast = useToastHide();
    useEffect(() => { hideToast(); }, [section, chat, hideToast]);
    // The open conversation follows its session: to a new name when it is
    // renamed, to a new conversation when it is restarted.
    useEffect(() => {
        if (!chat) return;
        const next = follow((snapshot && snapshot.sessions) || [], chat);
        if (next !== chat) setChat(next);
    }, [snapshot, chat]);
    const [picks, setPicks] = useState([]);
    const [names, setNames] = useState([]);
    // The archive filters by contour on its own: the panel stands beside the
    // column, and a pick made in one list that quietly moved the other read as
    // the archive having no filter at all.
    const [archPicks, setArchPicks] = useState([]);
    const [settings, setSettings] = useState(false);
    // The settings of the map open over the shell and come back where they
    // were left, for as long as the page lives.
    const [mapOpen, setMapOpen] = useState(false);
    const [mapAt, setMapAt] = useState(null);
    const [order, setOrder] = useState([]);

    const zoom = useScale();

    const shellRef = useRef(null);
    const tipRef = useRef(null);
    useEffect(() => attachTips(shellRef.current, tipRef.current), []);

    const profilesReq = useJSON("/api/profiles");
    const probesReq = useJSON("/api/probes");
    const topCpu = useJSON("/api/history/top?period=1h&metric=cpu&limit=12").data;
    const topMem = useJSON("/api/history/top?period=1h&metric=mem&limit=12").data;
    const profiles = profilesReq.data;
    const probes = (probesReq.data && probesReq.data.probes) || null;

    const stacks = (tree && tree.stacks) || [];
    const current = stacks.find((s) => s.name === stack) || stacks[0] || null;
    const container = current ? (current.containers || []).find((c) => c.id === cont) || null : null;

    // A page opened from another page goes back to the section under both:
    // two pages of the header replace each other, and neither is where the
    // person came from.
    const goSection = useCallback((id) => {
        setPlace((cur) => ({
            section: id,
            from: PAGES.includes(id) ? (PAGES.includes(cur.section) ? cur.from : cur.section) : "",
        }));
        setPanel((cur) => ((PANELS[id] || []).some((p) => p.id === cur) ? cur : null));
    }, []);
    const goBack = useCallback(() => goSection(from || "home"), [goSection, from]);

    useEffect(() => {
        const onKey = (e) => {
            if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
            if (typing()) return;
            if (settings || mapOpen) return;
            if (terminalOnScreen()) return;

            if (e.key === "Escape") {
                if (panel) {
                    setPanel(null);
                    e.preventDefault();
                }
                return;
            }

            if (section !== "sessions" || order.length === 0) return;

            if (e.key >= "1" && e.key <= "9") {
                const target = order[Number(e.key) - 1];
                if (target) {
                    setChat(target);
                    e.preventDefault();
                }
                return;
            }

            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                const at = order.findIndex((t) => chat && t.name === chat.name);
                const step = e.key === "ArrowDown" ? 1 : -1;
                const next = at < 0
                    ? (step > 0 ? 0 : order.length - 1)
                    : Math.min(order.length - 1, Math.max(0, at + step));
                setChat(order[next]);
                e.preventDefault();
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [section, order, chat, panel, settings, mapOpen]);

    const openChat = useCallback((target) => {
        setChat(target);
        goSection("sessions");
    }, [goSection]);

    // The terminals are a section where the listener has the terminal route:
    // elsewhere the section has no button, and a kept one opens home.
    const term = useTermAvailable();
    const [termOpen, setTermOpen] = useState(null);
    const openTerm = useCallback((target) => {
        setTermOpen(target);
        goSection("terminals");
    }, [goSection]);
    useEffect(() => {
        if (section === "terminals" && term.known && !term.route) goSection("home");
    }, [section, term.known, term.route, goSection]);
    useEffect(() => {
        if (!jump) return;
        openChat({ name: jump.name, id: jump.id });
        onJumped();
    }, [jump, openChat, onJumped]);

    const left = () => {
        if (section === "containers") {
            return html`<${StackColumn} tree=${tree} current=${stack} onPick=${setStack} exec=${exec} onDone=${onRefresh} />`;
        }
        if (section === "machine") {
            return html`<${MachineCats} snapshot=${snapshot} probes=${probes} current=${cat} onPick=${setCat} />`;
        }
        return html`<${SessionColumn}
            snapshot=${snapshot}
            profiles=${profiles}
            limits=${snapshot && snapshot.limits}
            current=${chat && chat.name}
            currentId=${chat && chat.id}
            onPick=${setChat}
            picks=${picks}
            setPicks=${setPicks}
            onNames=${setNames}
            onOrder=${setOrder}
            onArchive=${() => setPanel("archive")}
            exec=${exec}
            wait=${wait}
        />`;
    };

    const center = () => {
        if (section === "home") return html`<${Home} snapshot=${snapshot} tree=${tree} onSection=${goSection} />`;
        if (section === "containers") {
            return html`<${ContainersCenter}
                tree=${tree}
                stack=${stack}
                current=${cont}
                onPick=${setCont}
                exec=${exec}
                onDone=${onRefresh}
                onLogs=${(c) => { setCont(c.id); setPanel("logs"); }}
            />`;
        }
        if (section === "machine") {
            return html`<${MachineCenter}
                snapshot=${snapshot}
                probes=${probes}
                history=${history}
                topCpu=${topCpu}
                topMem=${topMem}
                cat=${cat}
            />`;
        }
        if (section === "terminals") {
            return term.route
                ? html`<${TermsDesk} snapshot=${snapshot} exec=${exec} open=${termOpen} onOpen=${setTermOpen} />`
                : null;
        }
        if (section === "devices") return html`<div class="dkpage"><${Devices} onBack=${goBack} /></div>`;
        if (section === "alerts") {
            return html`<div class="dkpage"><${Alerts} alerts=${alerts} onAction=${alerts.reload} onBack=${goBack} /></div>`;
        }
        if (!chat) return html`<${ChatEmpty} />`;
        const live = liveOf((snapshot && snapshot.sessions) || [], chat);
        return html`<section class="dkcenter dkchat">
            <${Chat}
                name=${chat.name}
                id=${chat.id}
                live=${live}
                snapshot=${snapshot}
                exec=${exec}
                wait=${wait}
                archive=${chat.archived ? chat.row : null}
                onBack=${() => setChat(null)}
                onUsage=${() => { setChat(null); goSection("home"); }}
                onTerm=${term.route ? openTerm : null}
                onOpenChat=${(talk) => openChat(talk.live
                    ? { name: talk.name, id: null }
                    : { name: talk.name, id: talk.id, archived: true, row: null })}
            />
        </section>`;
    };

    const troubles = [
        hostError && `the agent is silent: ${hostError}`,
        treeError && `docker is silent: ${treeError}`,
        profilesReq.error && `the profile map is unavailable: ${profilesReq.error}`,
        faults && faults.blocks && faults.blocks.length > 0 && `the collector is not writing: ${faults.blocks.join(", ")}`,
        ageSec !== null && ageSec > 120 && `the agent snapshot is ${Math.round(ageSec / 60)} min old`,
    ].filter(Boolean);

    const chip = routeChip(route);
    const panels = PANELS[section] || [];
    const panelTitle = (panels.find((p) => p.id === panel) || {}).label || "";
    const wide = section === "home" || section === "devices" || section === "alerts" || section === "terminals";

    return html`
        <div class="deskshell" ref=${shellRef}>
            <div class="dktip" ref=${tipRef} role="tooltip"></div>
            <header class="dktop">
                <button
                    class="dkbrand"
                    type="button"
                    aria-label="Settings"
                    onClick=${() => setSettings(true)}
                >${(snapshot && snapshot.hostName) || "host"}<span class="caret">▾</span></button>
                <button
                    class=${`dklogo${section === "home" ? " on" : ""}`}
                    type="button"
                    aria-label="Home"
                    onClick=${() => goSection("home")}
                ><${Icon.orbit} /></button>
                <nav class="dkibs">
                    ${SECTIONS.filter((it) => it.id !== "terminals" || term.route).map((it) => html`
                        <${IconButton} key=${it.id} item=${it} active=${section === it.id} onClick=${() => goSection(it.id)} />
                    `)}
                </nav>
                ${panels.length > 0 && html`<span class="dksep"></span>`}
                <nav class="dkibs">
                    ${panels.map((it) => html`
                        <${IconButton}
                            key=${it.id}
                            item=${it}
                            active=${it.layer ? mapOpen : panel === it.id}
                            onClick=${() => (it.layer ? setMapOpen(true) : setPanel((cur) => (cur === it.id ? null : it.id)))}
                        />
                    `)}
                </nav>
                <${HeadLoad} snapshot=${snapshot} onOpen=${() => goSection("machine")} />
                <div class="dktopright">
                    ${snapshot && snapshot.cookieInsecure && html`
                        <span class="dkalert" data-tip="AACP_SECURE=0 in .env while the panel is reached over https: the session cookie has no Secure flag and travels over plain http as well" data-tipside="left">
                            cookie without Secure
                        </span>
                    `}
                    ${troubles.length > 0 && html`
                        <span class="dkalert" data-tip=${troubles.join(" · ")} data-tipside="left">
                            silent <b>${troubles.length}</b>
                        </span>
                    `}
                    ${route && route.here && html`
                        <button
                            class=${`dkroute${chip.near ? " on" : ""}`}
                            type="button"
                            data-tip=${chip.tip}
                            data-tipside="left"
                            onClick=${route.onOpen}
                        >${chip.text}</button>
                    `}
                    <${Zoom} scale=${zoom.scale} onSmaller=${zoom.smaller} onBigger=${zoom.bigger} />
                    <${AlertsButton} count=${openAlerts} active=${section === "alerts"} onClick=${() => goSection("alerts")} />
                    <${IconButton}
                        item=${{ label: theme === "sky" ? "Dark theme" : "Light theme", icon: theme === "sky" ? Icon.moon : Icon.sun }}
                        active=${false}
                        onClick=${onTheme}
                    />
                    <${IconButton} item=${{ label: "Sign out", icon: Icon.exit }} active=${false} onClick=${logout} />
                </div>
            </header>

            <div class=${`dkbody${panel ? " withpanel" : ""}${wide ? " full" : ""}`}>
                ${!wide && left()}
                ${center()}
                ${panel && !wide && html`
                    <${RightPanel}
                        tab=${panel}
                        title=${panelTitle}
                        profiles=${profiles}
                        names=${names}
                        archPicks=${archPicks}
                        setArchPicks=${setArchPicks}
                        container=${container}
                        exec=${exec}
                        onOpen=${openChat}
                        onClose=${() => setPanel(null)}
                    />
                `}
            </div>

            ${settings && html`<${Settings} onClose=${() => setSettings(false)} exec=${exec} />`}

            ${mapOpen && html`
                <${MapSettings}
                    picks=${picks}
                    last=${mapAt}
                    exec=${exec}
                    sessions=${snapshot && snapshot.sessions}
                    onPick=${setMapAt}
                    onClose=${() => setMapOpen(false)}
                />
            `}

            ${updateReady && html`
                <div class="update" role="status">
                    <span>${updating ? "Updating…" : "A new version is ready"}</span>
                    <button type="button" disabled=${updating} onClick=${onApplyUpdate}>update</button>
                </div>
            `}
        </div>
    `;
}
