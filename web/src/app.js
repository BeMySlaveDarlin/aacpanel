// Application data and the choice of shell by screen width.
import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "./html.js";
import { loadHost, loadTree } from "./data.js";
import { setHostName } from "./actions/registry.js";
import { useCatchUp } from "./catchup.js";
import { useFaults } from "./faults.js";
import { useExec, useTreeStream } from "./exec.js";
import { useWide } from "./ui/wide.js";
import { unread, useAlerts } from "./alerts.js";
import { MobileShell } from "./mobile/shell.js";
import { RouteSheet, routeChip } from "./ui/route.js";
import { DesktopShell } from "./desktop/shell.js";
import { failed, pick } from "./router.js";
import * as api from "./api.js";
import { tellEndpoints, watchOpen } from "./pwa.js";


const REFRESH_MS = 15000;

const HISTORY_POINTS = 240;

const PICK_MS = 5 * 60 * 1000;


function toLogin(err) {
    const params = new URLSearchParams({ next: location.pathname });
    if (err.reason) params.set("reason", err.reason);
    if (err.afterSec) params.set("after", String(err.afterSec));
    location.href = `/login?${params}`;
}

function point(snapshot) {
    const host = snapshot.host || {};
    const net = (host.net || []).reduce(
        (sum, n) => ({ rx: sum.rx + (n.rxRate || 0), tx: sum.tx + (n.txRate || 0) }),
        { rx: 0, tx: 0 },
    );
    return {
        t: snapshot.at || Math.floor(Date.now() / 1000),
        cpu: host.cpuPct || 0,
        mem: (host.mem && host.mem.pct) || 0,
        rx: net.rx,
        tx: net.tx,
    };
}

function trim(points) {
    return points.length > HISTORY_POINTS ? points.slice(-HISTORY_POINTS) : points;
}

function clock(date) {
    return date.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}

const THEME_KEY = "aacpanel.theme";

// wanted reads the session an address asks to open: a tap on a push about a
// session lands on /app?session=<name>.
function wanted(search) {
    const name = new URLSearchParams(search).get("session") || "";
    return name ? { name, id: null } : null;
}

export function App({ updateReady, onApplyUpdate, installable, onInstall }) {
    const [tree, setTree] = useState(null);
    const [snapshot, setSnapshot] = useState(null);
    const [treeError, setTreeError] = useState(null);
    const [hostError, setHostError] = useState(null);
    const [history, setHistory] = useState([]);
    const [ageSec, setAgeSec] = useState(null);
    const [conn, setConn] = useState({ kind: "loading" });
    const faults = useFaults();

    const routed = useRef(false);
    const picking = useRef(false);
    const [here, setHere] = useState("");
    const [via, setVia] = useState("");
    const [routeWhy, setRouteWhy] = useState("");
    const repick = useCallback(async (avoid = "") => {
        if (picking.current) return false;
        picking.current = true;
        try {
            const was = api.current().base;
            const r = await pick({ fetch: api.direct, token: api.fetchToken, avoid });
            api.use({ base: r.base, via: r.via, token: r.token });
            tellEndpoints(r.endpoints.map((e) => e.url));
            setHere(r.here);
            setVia(r.via);
            setRouteWhy(r.why);
            return r.base !== was;
        } finally {
            picking.current = false;
        }
    }, []);
    const onRecheck = useCallback(() => { repick(); }, [repick]);
    const [routeOpen, setRouteOpen] = useState(false);
    const openRoute = useCallback(() => setRouteOpen(true), []);
    const closeRoute = useCallback(() => setRouteOpen(false), []);

    const refresh = useCallback(async () => {
        const [treeResult, hostResult] = await Promise.allSettled([loadTree(), loadHost()]);

        if (treeResult.status === "fulfilled") {
            setTree(treeResult.value.tree);
            setTreeError(null);
            setConn({ kind: treeResult.value.stale ? "stale" : "live", at: treeResult.value.at });
            if (!routed.current) {
                routed.current = true;
                repick().then((changed) => changed && refresh());
            }
        } else {
            const err = treeResult.reason;
            if (err && err.unauthorized) {
                toLogin(err);
                return;
            }
            setTreeError(err ? err.message : "the tree is unavailable");
            setConn((prev) => ({ ...prev, kind: "offline" }));
            if (!routed.current && err && !err.status) {
                routed.current = true;
                repick().then((changed) => changed && refresh());
            }
        }

        if (hostResult.status === "fulfilled") {
            const { snapshot: fresh, ageSec: age } = hostResult.value;
            setSnapshot(fresh);
            setAgeSec(age);
            setHistory((prev) => trim([...prev, point(fresh)]));
            setHostError(null);
            setHostName(fresh.hostName);
            if (fresh.hostName) document.title = fresh.hostName;
        } else {
            setAgeSec(null);
            setHostError(hostResult.reason ? hostResult.reason.message : "the agent snapshot is unavailable");
        }
    }, []);

    const asking = useRef("");
    useEffect(() => {
        api.watchFailures(async (dead) => {
            if (asking.current === dead) return;
            asking.current = dead;
            try {
                if (!(await failed(dead, { fetch: api.direct }))) return;
                api.use({});
                setVia("");
                repick(dead);
            } finally {
                asking.current = "";
            }
        });
        return () => api.watchFailures(null);
    }, [repick]);
    useEffect(() => {
        const timer = setInterval(() => {
            if (document.visibilityState === "visible" && routed.current) repick();
        }, PICK_MS);
        return () => clearInterval(timer);
    }, [repick]);

    useEffect(() => {
        refresh();
        const timer = setInterval(() => {
            if (document.visibilityState === "visible") refresh();
        }, REFRESH_MS);
        const onVisible = () => document.visibilityState === "visible" && refresh();
        document.addEventListener("visibilitychange", onVisible);
        window.addEventListener("online", refresh);
        return () => {
            clearInterval(timer);
            document.removeEventListener("visibilitychange", onVisible);
            window.removeEventListener("online", refresh);
        };
    }, [refresh]);

    const onTree = useCallback((fresh) => {
        setTree(fresh);
        setTreeError(null);
        setConn({ kind: "live", at: new Date() });
    }, []);
    useTreeStream(onTree);

    const exec = useExec();

    const wait = useCatchUp(snapshot, refresh);


    const alerts = useAlerts();
    const openAlerts = unread(alerts.alerts).length;

    const [theme, setTheme] = useState(() => {
        try {
            return localStorage.getItem(THEME_KEY) === "sky" ? "sky" : "space";
        } catch {
            return "space";
        }
    });
    useEffect(() => {
        document.documentElement.classList.toggle("sky", theme === "sky");
        try {
            localStorage.setItem(THEME_KEY, theme);
        } catch {
        }
    }, [theme]);
    const onTheme = useCallback(() => setTheme((t) => (t === "sky" ? "space" : "sky")), []);

    const [jump, setJump] = useState(() => wanted(location.search));
    useEffect(() => {
        watchOpen((url) => setJump(wanted(new URL(url, location.href).search)));
    }, []);
    const onJumped = useCallback(() => {
        setJump(null);
        if (location.search) history.replaceState(history.state, "", location.pathname);
    }, []);

    const shared = {
        snapshot, tree, treeError, hostError, ageSec, history, faults,
        alerts, openAlerts, exec, onRefresh: refresh, wait, theme, onTheme,
        updateReady, onApplyUpdate, jump, onJumped,
        route: { here, via, why: routeWhy, onRecheck, onOpen: openRoute },
    };

    const wide = useWide();
    const sheet = html`<${RouteSheet} open=${routeOpen} onClose=${closeRoute} route=${shared.route} />`;
    if (wide) {
        return html`
            <${DesktopShell} ...${shared} />
            ${sheet}
        `;
    }

    return html`
        <${MobileShell}
            ...${shared}
            conn=${conn}
            installable=${installable}
            onInstall=${onInstall}
            status=${html`<${Status} conn=${conn} installable=${installable} onInstall=${onInstall} route=${shared.route} />`}
        />
        ${sheet}
    `;
}

function Status({ conn, installable, onInstall, route }) {
    const text = () => {
        if (conn.kind === "loading") return html`<span>Loading…</span>`;
        if (conn.kind === "unauthorized") return html`<span>The session has ended — <a href="/login">sign in again</a></span>`;
        if (conn.kind === "offline") {
            return conn.at
                ? html`<span>No connection — data from ${clock(conn.at)}</span>`
                : html`<span>No connection and no saved snapshot</span>`;
        }
        if (conn.kind === "stale") return html`<span>No connection — snapshot from ${clock(conn.at)}</span>`;
        return null;
    };

    const kind = { live: "ok", stale: "warn", offline: "warn", unauthorized: "bad", loading: "" }[conn.kind];
    const chip = routeChip(route);

    return html`
        <span class="status ${kind}">
            ${text()}
            ${installable && html`<button class="ghost accent" type="button" onClick=${onInstall}>install</button>`}
            ${route.here && html`
                <button
                    class=${`route${chip.near ? " on" : ""}`}
                    type="button"
                    title=${chip.tip}
                    onClick=${route.onOpen}
                >${chip.text}</button>
            `}
        </span>
    `;
}
