// The desktop shell: the section decides what stands left, centre and right.
import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { terminalOnScreen, typing } from "../ui/focus.js";
import { MenuItem, useMenu } from "../ui/menu.js";
import { useColumnWidth } from "../ui/resize.js";
import { attachTips } from "./tip.js";
import { logout } from "../auth.js";
import { Chat } from "../screens/chat.js";
import { follow, liveOf } from "../catchup.js";
import { ChatEmpty } from "../screens/chat/empty.js";
import { Alerts } from "../screens/alerts.js";
import { HeadLoad } from "./load.js";
import { Devices } from "../screens/devices.js";
import { Settings } from "../screens/settings.js";
import { Tasks } from "../screens/tasks.js";
import { SessionColumn } from "./sessions.js";
import { TermsDesk } from "./terms.js";
import { useTermAvailable } from "../screens/chat/term.js";
import { StackColumn, ContainersCenter } from "./containers.js";
import { MachineCats, MachineCenter } from "./machine.js";
import { routeChip } from "../ui/route.js";
import { useToastHide } from "../ui/toasts.js";
import { Home } from "./home.js";
import { JOURNAL, PANELS, RightPanel } from "./panels.js";
import { MapSettings } from "./settings.js";
import { MenuHost } from "./menu.js";
import { CardPieHost } from "../screens/sessions/cardpie.js";
import { MAX, MIN, useScale } from "./scale.js";

export const SECTIONS = [
    { id: "home", label: "Home", icon: Icon.orbit },
    { id: "sessions", label: "Sessions", icon: Icon.sessions },
    { id: "terminals", label: "Terminals", icon: Icon.prompt },
    { id: "containers", label: "Containers", icon: Icon.containers },
    { id: "machine", label: "Machine", icon: Icon.cpu },
];

// The pages stand over a section rather than beside it: each has a way back,
// and it leads to the section the page was opened over.
const PAGES = ["devices", "alerts"];

// Every section the shell can stand on. A kept section that is not among them
// opens home.
const PLACES = [...SECTIONS.map((s) => s.id), ...PAGES];

const PLACE_KEY = "aacpanel.desktop.place";
const LABELS_KEY = "aacpanel.desktop.labels";

function readLabels() {
    try {
        return localStorage.getItem(LABELS_KEY) !== "false";
    } catch {
        return true;
    }
}

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

function NavButton({ item, active, onClick, stripe = false, count = 0 }) {
    return html`
        <button
            class=${`${stripe ? "dkpaneltab" : "dkrailbtn"}${active ? " on" : ""}`}
            type="button"
            aria-label=${count > 0 ? `${item.label}, ${count} unread` : item.label}
            aria-pressed=${Boolean(active)}
            data-tip=${item.label}
            data-tipside=${stripe ? "left" : undefined}
            onClick=${onClick}
        ><${item.icon} /><span class="dknavlabel">${item.name || item.label}</span>
            ${count > 0 && html`<span class="dkibnum">${count}</span>`}
        </button>
    `;
}

function YouMenu({ onDevices, theme, onTheme }) {
    return html`
        <${MenuItem} label="Devices" onPick=${onDevices} />
        <${MenuItem} label=${theme === "sky" ? "Dark theme" : "Light theme"} onPick=${onTheme} />
        <${MenuItem} label="Sign out" onPick=${logout} />
    `;
}

function AlertsButton({ count, active, onClick }) {
    return html`<${NavButton} item=${{ label: "Alerts", icon: Icon.alerts }} count=${count}
        active=${active} onClick=${onClick} />`;
}

function YouButton({ onDevices, theme, onTheme }) {
    const menuOf = useMenu();
    return html`<${NavButton} item=${{ label: "You", icon: Icon.settings }}
        onClick=${menuOf && menuOf(YouMenu, { onDevices, theme, onTheme }, "You")} />`;
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
    // The task registry of a contour, by its entry of the map, stands in the
    // centre over the conversation, and the conversation is there again once
    // it closes. A conversation picked while it is open takes its place.
    const [tasks, setTasks] = useState(null);
    const showChat = useCallback((target) => {
        setTasks(null);
        setChat(target);
    }, []);
    // A conversation opened from outside the sessions column, held until the
    // column has moved its switch to the conversation's contour.
    const [reveal, setReveal] = useState(null);
    const revealed = useCallback(() => setReveal(null), []);
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
    const [names, setNames] = useState([]);
    // The contour the sessions column stands on: the settings of the map open
    // on it.
    const [shown, setShown] = useState("");
    // The archive filters by contour on its own: the panel stands beside the
    // column, and a pick made in one list that quietly moved the other read as
    // the archive having no filter at all.
    const [archPicks, setArchPicks] = useState([]);
    const [settings, setSettings] = useState(false);
    // The settings of the map open over the shell and come back where they
    // were left, for as long as the page lives.
    const [mapOpen, setMapOpen] = useState(false);
    const [mapAt, setMapAt] = useState(null);
    // The menu of a project opens them on its page.
    const openMapAt = useCallback((at) => {
        setMapAt(at);
        setMapOpen(true);
    }, []);
    const [order, setOrder] = useState([]);

    const zoom = useScale();
    const [labels, setLabels] = useState(readLabels);
    useEffect(() => {
        try { localStorage.setItem(LABELS_KEY, String(labels)); } catch {
        }
    }, [labels]);
    const column = useColumnWidth({ key: "aacpanel.desktop.column", min: 16, max: 32, initial: 19.5, label: "Resize section column" });

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
    // two pages replace each other, and neither is where the
    // person came from.
    const goSection = useCallback((id) => {
        setPlace((cur) => ({
            section: id,
            from: PAGES.includes(id) ? (PAGES.includes(cur.section) ? cur.from : cur.section) : "",
        }));
        setPanel((cur) => (cur === "journal" || (PANELS[id] || []).some((p) => p.id === cur) ? cur : null));
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

            // Over the registry the arrow keys scroll it: the conversations
            // they walk are under it.
            if (section !== "sessions" || order.length === 0 || tasks) return;

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
    }, [section, order, chat, panel, settings, mapOpen, tasks]);

    const openChat = useCallback((target) => {
        showChat(target);
        setReveal(target);
        goSection("sessions");
    }, [goSection, showChat]);

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
        openChat({ name: jump.name, id: jump.id, toAct: jump.toAct });
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
            limits=${snapshot && snapshot.limits}
            current=${chat && chat.name}
            currentId=${chat && chat.id}
            onPick=${showChat}
            onTerm=${term.route ? openTerm : null}
            onSettings=${openMapAt}
            onTasks=${setTasks}
            onNames=${setNames}
            onShown=${setShown}
            reveal=${reveal}
            onRevealed=${revealed}
            onOrder=${setOrder}
            onArchive=${() => setPanel("archive")}
            exec=${exec}
            wait=${wait}
        />`;
    };

    const center = () => {
        if (section === "home") {
            return html`<${Home}
                snapshot=${snapshot}
                tree=${tree}
                exec=${exec}
                wait=${wait}
                theme=${theme}
                onSection=${goSection}
                onSession=${openChat}
                onTerm=${term.route ? openTerm : null}
            />`;
        }
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
                exec=${exec} onDone=${onRefresh}
                probes=${probes}
                history=${history}
                topCpu=${topCpu}
                topMem=${topMem}
                cat=${cat}
            />`;
        }
        if (section === "terminals") {
            return term.route
                ? html`<${TermsDesk} snapshot=${snapshot} exec=${exec} open=${termOpen} onOpen=${setTermOpen} onSession=${openChat} />`
                : null;
        }
        if (section === "devices") return html`<div class="dkpage"><${Devices} onBack=${goBack} /></div>`;
        if (section === "alerts") {
            return html`<div class="dkpage"><${Alerts} alerts=${alerts} onAction=${alerts.reload} onBack=${goBack} /></div>`;
        }
        // The registry draws its sheets beside its page: one box under the
        // page takes the width the page gives its children, and the sheets
        // inside it keep the whole window.
        if (tasks) {
            return html`<div class="dkpage"><div>
                <${Tasks} key=${tasks.id} contour=${tasks.id} name=${tasks.profile} onClose=${() => setTasks(null)} />
            </div></div>`;
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
                toAct=${chat.toAct || null}
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
    const hostName = (snapshot && snapshot.hostName) || "host";
    const panels = PANELS[section] || [];
    const panelTitle = ([...panels, JOURNAL].find((p) => p.id === panel) || {}).label || "";
    const wide = section === "home" || section === "devices" || section === "alerts" || section === "terminals";

    // The menu of the right click stands inside the shell, where the tips of
    // its items are read.
    return html`
        <div class=${`deskshell${labels ? "" : " compact"}`} ref=${shellRef}>
            <${MenuHost}>
                <div class="dktip" ref=${tipRef} role="tooltip"></div>
                <div class="dkworkspace">
                    <nav class="dkrail" aria-label="Sections">
                        <button class="dkhost" type="button" aria-label=${labels ? "Settings" : `${hostName} settings`}
                                data-tip=${labels ? "Settings" : `${hostName} · Settings`}
                                onClick=${() => setSettings(true)}>
                            <span>${labels ? hostName : Array.from(hostName)[0].toUpperCase()}</span>
                            ${labels && html`<span class="caret">▾</span>`}
                        </button>
                        <div class="dksections">
                            ${SECTIONS.filter((it) => it.id !== "terminals" || term.route).map((it) => html`
                                <${NavButton} key=${it.id} item=${it} active=${section === it.id} onClick=${() => goSection(it.id)} />
                            `)}
                        </div>
                        <div class="dkrailbottom">
                            <button class="dkrailbtn dkcollapse" type="button" aria-label=${labels ? "Hide navigation labels" : "Show navigation labels"}
                                    data-tip=${labels ? "Hide navigation labels" : "Show navigation labels"} aria-pressed=${!labels}
                                    onClick=${() => setLabels((v) => !v)}><${Icon.chevron} /></button>
                            <${AlertsButton} count=${openAlerts} active=${section === "alerts"} onClick=${() => goSection("alerts")} />
                            <${YouButton} onDevices=${() => goSection("devices")} theme=${theme} onTheme=${onTheme} />
                        </div>
                    </nav>

                <${CardPieHost} snapshot=${snapshot} exec=${exec} wait=${wait} onTerm=${term.route ? openTerm : null}>
                <div class=${`dkbody${panel ? " withpanel" : ""}${wide ? " full" : ""}`} style=${`--column-width:${column.width}rem`}>
                    ${!wide && html`<div class="dkcolumn" ref=${column.ref}>
                        ${left()}${column.edge}
                    </div>`}
                    ${center()}
                    ${panel && html`
                        <${RightPanel}
                            snapshot=${snapshot}
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
                <//>
                    <nav class="dkstripe" aria-label="Panels">
                        <div class="dksectionpanels">
                            ${panels.map((it) => html`
                                <${NavButton} key=${it.id} item=${it} stripe=${true}
                                    active=${it.layer ? mapOpen : panel === it.id}
                                    onClick=${() => (it.layer ? setMapOpen(true) : setPanel((cur) => (cur === it.id ? null : it.id)))} />
                            `)}
                        </div>
                        <${NavButton} item=${JOURNAL} stripe=${true} active=${panel === "journal"}
                            onClick=${() => setPanel((cur) => cur === "journal" ? null : "journal")} />
                    </nav>
                </div>
                <footer class="dkstatus">
                    <span class="dkstatushost"><i class=${`dkconnection${hostError || ageSec > 120 ? " silent" : ""}`}></i>${(snapshot && snapshot.hostName) || "host"}</span>
                    ${route && route.here && html`
                        <button class=${`dkroute${chip.near ? " on" : ""}`} type="button" data-tip=${chip.tip}
                                onClick=${route.onOpen}>${chip.text}</button>
                    `}
                    <${HeadLoad} snapshot=${snapshot} onOpen=${() => goSection("machine")} />
                    ${snapshot && snapshot.cookieInsecure && html`
                        <span class="dkalert" data-tip="AACP_SECURE=0 in .env while the panel is reached over https: the session cookie has no Secure flag and travels over plain http as well">
                            cookie without Secure
                        </span>
                    `}
                    ${troubles.length > 0 && html`
                        <span class="dkalert" data-tip=${troubles.join(" · ")}>silent <b>${troubles.length}</b></span>
                    `}
                    <${Zoom} scale=${zoom.scale} onSmaller=${zoom.smaller} onBigger=${zoom.bigger} />
                </footer>


                ${settings && html`<${Settings} onClose=${() => setSettings(false)} exec=${exec} />`}

                ${mapOpen && html`
                    <${MapSettings}
                        shown=${shown}
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
            <//>
        </div>
    `;
}
