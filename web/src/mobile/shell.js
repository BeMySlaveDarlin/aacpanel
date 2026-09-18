// The phone shell: header, tabs and the bottom menu.
import { useCallback, useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { useBackClose } from "../ui/back.js";
import { Header } from "../ui/header.js";
import { Chips } from "../ui/chips.js";
import { Nav, PAGES, TABS } from "../ui/nav.js";
import { HomeButton } from "../ui/home.js";
import { LogBar } from "../ui/logbar.js";
import { Sheet } from "../ui/sheet.js";
import { useToastHide } from "../ui/toasts.js";
import { logout } from "../auth.js";
import { Alerts } from "../screens/alerts.js";
import { Containers, filterChips } from "../screens/containers.js";
import { Machine, machineStats } from "../screens/machine.js";
import { Sessions, sessionChips } from "../screens/sessions.js";
import { Devices } from "../screens/devices.js";
import { Usage } from "../screens/usage.js";
import { Profiles } from "../screens/profiles.js";
import { Journal } from "../screens/journal.js";
import { Briefs } from "../screens/briefs.js";
import { Settings } from "../screens/settings.js";

const TAB_KEY = "aacpanel.tab";

function lastTab() {
    try {
        const saved = window.localStorage.getItem(TAB_KEY);
        if (saved && TABS.some((s) => s.id === saved)) return saved;
    } catch (err) {
    }
    return "containers";
}

export function MobileShell({
    snapshot, tree, treeError, hostError, ageSec, history, faults, alerts, openAlerts,
    exec, onRefresh, wait, conn, theme, onTheme, updateReady, updating, onApplyUpdate, installable, onInstall,
    status, jump, onJumped,
}) {
    const [tab, setTab] = useState(lastTab);
    const [filters, setFilters] = useState({ containers: "all", sessions: "all" });
    const [query, setQuery] = useState("");
    const [open, setOpen] = useState(() => new Set());

    const [page, setPage] = useState(null);
    const [openBrief, setOpenBrief] = useState(null);
    const [menu, setMenu] = useState(false);

    const [logs, setLogs] = useState(null);
    const [logsBig, setLogsBig] = useState(true);

    const [layer, setLayer] = useState(false);

    useBackClose(Boolean(page), () => {
        if (page === "briefs" && openBrief) {
            setOpenBrief(null);
            return;
        }
        setPage(null);
    });

    // The note about an action belongs to the screen it was taken on.
    const hideToast = useToastHide();
    useEffect(() => { hideToast(); }, [tab, page, hideToast]);

    const toggleStack = useCallback((name) => {
        setOpen((prev) => {
            const next = new Set(prev);
            if (next.has(name)) next.delete(name);
            else next.add(name);
            return next;
        });
    }, []);

    const openLogs = useCallback((container) => {
        setLogs({ id: container.id, name: container.name });
        setLogsBig(true);
    }, []);

    const filter = filters[tab] || "all";
    const setFilter = useCallback((id) => setFilters((prev) => ({ ...prev, [tab]: id })), [tab]);


    const goTab = useCallback((id) => {
        setPage(null);
        setTab(id);
        try {
            window.localStorage.setItem(TAB_KEY, id);
        } catch (err) {
        }
    }, []);

    const goNav = useCallback((id) => {
        if (TABS.some((t) => t.id === id)) {
            goTab(id);
            return;
        }
        setPage(id);
    }, [goTab]);

    const section = PAGES.some((p) => p.id === page);

    const [want, setWant] = useState(null);
    const goHome = useCallback((name, id) => {
        setPage(null);
        goTab("sessions");
        setWant({ name, id });
    }, [goTab]);
    useEffect(() => {
        if (!jump) return;
        goHome(jump.name, jump.id);
        onJumped();
    }, [jump, goHome, onJumped]);

    const chips = useMemo(() => {
        if (layer) return null;
        if (tab === "containers") return filterChips(tree);
        if (tab === "sessions") return sessionChips(snapshot);
        return null;
    }, [tab, tree, snapshot, layer]);

    const machine = useMemo(
        () => (tab === "containers" && !layer && !page ? machineStats(snapshot) : null),
        [tab, layer, page, snapshot],
    );


    return html`
        <div class=${`shell${updateReady ? " has-update" : ""}${layer || page ? " no-nav" : ""}`}>
            <${Header}
                onMenu=${() => setMenu(true)}
                hostName=${(snapshot && snapshot.hostName) || "host"}
                insecure=${!!(snapshot && snapshot.cookieInsecure)}
                ageSec=${ageSec}
                machine=${machine}
                onMachine=${() => setPage("machine")}
                alerts=${openAlerts}
                onAlerts=${() => setPage("alerts")}
                query=${query}
                onQuery=${setQuery}
                status=${status}
                theme=${theme}
                onTheme=${onTheme}
            />

            ${!page && chips && html`<${Chips} items=${chips} current=${filter} onSelect=${setFilter} />`}

            <main class="body">
                ${page === "machine"
                    ? html`<${Machine} snapshot=${snapshot} error=${hostError} ageSec=${ageSec}
                        history=${history} faults=${faults} onBack=${() => setPage(null)} />`
                    : page === "devices"
                    ? html`<${Devices} onBack=${() => setPage(null)} />`
                    : page === "usage"
                    ? html`<${Usage} snapshot=${snapshot} exec=${exec} onBack=${() => setPage(null)} />`
                    : page === "settings"
                    ? html`<${Settings} onClose=${() => setPage(null)} />`
                    : page === "profiles"
                    ? html`<${Profiles} />`
                    : page === "journal"
                    ? html`<${Journal} onBack=${() => setPage(null)} />`
                    : page === "briefs"
                    ? html`<${Briefs} snapshot=${snapshot} exec=${exec}
                        open=${openBrief} onOpen=${setOpenBrief}
                        onSession=${(name) => { setOpenBrief(null); goHome(name, null); }} />`
                    : page === "alerts"
                    ? html`<${Alerts} alerts=${alerts} onAction=${alerts.reload} onBack=${() => setPage(null)} />`
                    : html`<${Screen}
                    tab=${tab}
                    tree=${tree}
                    snapshot=${snapshot}
                    filter=${filter}
                    query=${query}
                    open=${open}
                    onToggle=${toggleStack}
                    onLogs=${openLogs}
                    onDone=${onRefresh}
                    wait=${wait}
                    exec=${exec}
                    treeError=${treeError}
                    hostError=${hostError}
                    ageSec=${ageSec}
                    faults=${faults}
                    onLayer=${setLayer}
                    want=${want}
                    onWanted=${() => setWant(null)}
                onUsage=${() => setPage("usage")} />`}
            </main>

            <${Sheet} open=${menu} onClose=${() => setMenu(false)} label="menu">
                <div class="shead">
                    <div><div class="stitle">${(snapshot && snapshot.hostName) || "host"}</div><div class="ssub">monitoring panel</div></div>
                </div>
                <button class="item" type="button" onClick=${() => { setMenu(false); setPage("settings"); }}>
                    Settings
                </button>
                <button class="item" type="button" onClick=${() => { setMenu(false); setPage("devices"); }}>
                    Devices
                </button>
                <button class="item" type="button" onClick=${() => { setMenu(false); setPage("journal"); }}>
                    Journal
                </button>
                <button class="item" type="button" onClick=${() => { setMenu(false); setPage("usage"); }}>
                    Usage
                </button>
                <button class="item danger" type="button" onClick=${logout}>Sign out</button>
            <//>

            ${logs && html`
                <${LogBar}
                    target=${logs}
                    expanded=${logsBig}
                    onToggle=${() => setLogsBig((v) => !v)}
                    onClose=${() => setLogs(null)}
                />
            `}

            ${updateReady && html`
                <div class="update" role="status">
                    <span>${updating ? "Updating…" : "A new version is ready"}</span>
                    <button type="button" disabled=${updating} onClick=${onApplyUpdate}>update</button>
                </div>
            `}

            ${!layer && (!page || section) && html`
                <${Nav}
                    current=${page || tab}
                    onSelect=${goNav}
                    home=${html`
                        <${HomeButton}
                            snapshot=${snapshot}
                            exec=${exec}
                            onChat=${goHome}
                            onOpened=${() => { setPage(null); goTab("sessions"); }}
                        />
                    `}
                />
            `}
        </div>
    `;
}

function Screen({ tab, tree, snapshot, filter, query, open, onToggle, onLogs, onDone, wait, exec, treeError, hostError, ageSec, faults, onLayer, want, onWanted, onUsage }) {
    if (tab === "sessions") {
        return html`<${Sessions} snapshot=${snapshot} error=${hostError} ageSec=${ageSec} filter=${filter}
            exec=${exec} wait=${wait} faults=${faults} onLayer=${onLayer}
            want=${want} onWanted=${onWanted} onUsage=${onUsage} />`;
    }
    return html`<${Containers} tree=${tree} error=${treeError} filter=${filter} query=${query} open=${open}
        onToggle=${onToggle} onLogs=${onLogs} onDone=${onDone} exec=${exec} />`;
}
