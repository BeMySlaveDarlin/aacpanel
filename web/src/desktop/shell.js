// The desktop shell: the section decides what stands left, centre and right.
import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { terminalOnScreen, typing } from "../ui/focus.js";
import { attachTips } from "./tip.js";
import { logout } from "../auth.js";
import { Chat } from "../screens/chat.js";
import { ChatEmpty } from "../screens/chat/empty.js";
import { Devices } from "../screens/devices.js";
import { Settings } from "../screens/settings.js";
import { SessionColumn } from "./sessions.js";
import { StackColumn, ContainersCenter } from "./containers.js";
import { MachineCats, MachineCenter } from "./machine.js";
import { routeChip } from "../ui/route.js";
import { useToastHide } from "../ui/toasts.js";
import { Home } from "./home.js";
import { PANELS, RightPanel } from "./panels.js";

export const SECTIONS = [
    { id: "sessions", label: "Sessions", icon: Icon.sessions },
    { id: "containers", label: "Containers", icon: Icon.containers },
    { id: "machine", label: "Machine", icon: Icon.cpu },
    { id: "devices", label: "Devices", icon: Icon.skill },
];

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

function IconButton({ item, active, onClick }) {
    return html`
        <button
            class=${`dkib${active ? " on" : ""}`}
            type="button"
            data-tip=${item.label}
            onClick=${onClick}
        ><${item.icon} /></button>
    `;
}

export function DesktopShell({
    snapshot, tree, treeError, hostError, ageSec, history, faults, alerts, openAlerts,
    exec, onRefresh, wait, theme, onTheme, updateReady, updating, onApplyUpdate, route, jump, onJumped,
}) {
    const [section, setSection] = useState("home");
    const [chat, setChat] = useState(null);
    const [stack, setStack] = useState(null);
    const [cont, setCont] = useState(null);
    const [cat, setCat] = useState("cpu");
    const [panel, setPanel] = useState(null);

    // The note about an action belongs to the section and the conversation it was taken in.
    const hideToast = useToastHide();
    useEffect(() => { hideToast(); }, [section, chat, hideToast]);
    const [picks, setPicks] = useState([]);
    const [settings, setSettings] = useState(false);
    const [order, setOrder] = useState([]);

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

    const goSection = useCallback((id) => {
        setSection(id);
        setPanel((cur) => ((PANELS[id] || []).some((p) => p.id === cur) ? cur : null));
    }, []);

    useEffect(() => {
        const onKey = (e) => {
            if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
            if (typing()) return;
            if (settings) return;
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
    }, [section, order, chat, panel, settings]);

    const openChat = useCallback((target) => {
        setChat(target);
        setSection("sessions");
    }, []);
    useEffect(() => {
        if (!jump) return;
        openChat({ name: jump.name, id: jump.id });
        onJumped();
    }, [jump, openChat, onJumped]);

    const onOpened = useCallback(() => setPanel(null), []);

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
            onPick=${setChat}
            picks=${picks}
            setPicks=${setPicks}
            onOrder=${setOrder}
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
        if (section === "devices") return html`<div class="dkpage"><${Devices} onBack=${() => goSection("sessions")} /></div>`;
        if (!chat) return html`<${ChatEmpty} />`;
        const live = chat.archived
            ? null
            : ((snapshot && snapshot.sessions) || []).find((x) => x.session === chat.name) || null;
        return html`<section class="dkcenter dkchat">
            <${Chat}
                name=${chat.name}
                id=${chat.id}
                live=${live}
                exec=${exec}
                archive=${chat.archived ? chat.row : null}
                onBack=${() => setChat(null)}
                onUsage=${() => { setChat(null); goSection("home"); }}
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
    const wide = section === "home" || section === "devices";

    return html`
        <div class="deskshell" ref=${shellRef}>
            <div class="dktip" ref=${tipRef} role="tooltip"></div>
            <header class="dktop">
                <button
                    class="dkbrand"
                    type="button"
                    data-tip="Settings"
                    onClick=${() => setSettings(true)}
                >${(snapshot && snapshot.hostName) || "host"}<span class="caret">▾</span></button>
                <button
                    class=${`dklogo${section === "home" ? " on" : ""}`}
                    type="button"
                    data-tip="Home"
                    onClick=${() => goSection("home")}
                ><${Icon.orbit} /></button>
                <nav class="dkibs">
                    ${SECTIONS.map((it) => html`
                        <${IconButton} key=${it.id} item=${it} active=${section === it.id} onClick=${() => goSection(it.id)} />
                    `)}
                </nav>
                ${panels.length > 0 && html`<span class="dksep"></span>`}
                <nav class="dkibs">
                    ${panels.map((it) => html`
                        <${IconButton}
                            key=${it.id}
                            item=${it}
                            active=${panel === it.id}
                            onClick=${() => setPanel((cur) => (cur === it.id ? null : it.id))}
                        />
                    `)}
                </nav>
                <div class="dktopright">
                    ${snapshot && snapshot.cookieInsecure && html`
                        <span class="dkalert" data-tip="AACP_SECURE=0 in .env while the panel is reached over https: the session cookie has no Secure flag and travels over plain http as well" data-tipside="left">
                            cookie without Secure
                        </span>
                    `}
                    ${openAlerts > 0 && html`
                        <button class="dkalert" type="button" onClick=${() => goSection("machine")}>
                            alerts <b>${openAlerts}</b>
                        </button>
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
                        picks=${picks}
                        container=${container}
                        exec=${exec}
                        onOpen=${openChat}
                        onClose=${() => setPanel(null)}
                        onOpened=${onOpened}
                    />
                `}
            </div>

            ${settings && html`<${Settings} onClose=${() => setSettings(false)} />`}

            ${updateReady && html`
                <div class="update" role="status">
                    <span>${updating ? "Updating…" : "A new version is ready"}</span>
                    <button type="button" disabled=${updating} onClick=${onApplyUpdate}>update</button>
                </div>
            `}
        </div>
    `;
}
