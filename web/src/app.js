// Application data and the choice of shell by screen width.
import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "./html.js";
import { loadHost, loadTree } from "./data.js";
import { setHostName } from "./actions/registry.js";
import { useCatchUp } from "./catchup.js";
import { useFaults } from "./faults.js";
import { useExec, useTreeStream } from "./exec.js";
import { useWide } from "./ui/wide.js";
import { openCount, useAlerts } from "./alerts.js";
import { MobileShell } from "./mobile/shell.js";
import { RouteSheet, asOf } from "./ui/route.js";
import { AsOf } from "./ui/asof.js";
import { DesktopShell } from "./desktop/shell.js";
import { failed, pick } from "./router.js";
import * as api from "./api.js";
import { tellEndpoints, watchOpen } from "./pwa.js";


const REFRESH_MS = 15000;

const HISTORY_POINTS = 240;

const PICK_MS = 5 * 60 * 1000;


function toLogin(err) {
    const params = new URLSearchParams({ next: location.pathname + location.search });
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

const THEME_KEY = "aacpanel.theme";

// wanted reads the session an address asks to open: a tap on a push about a
// session lands on /app?session=<name>.
function wanted(search) {
    const name = new URLSearchParams(search).get("session") || "";
    return name ? { name, id: null } : null;
}

export function App({ updateReady, updating, onApplyUpdate, installable, onInstall }) {
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
    const openAlerts = openCount(alerts);

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
        updateReady, updating, onApplyUpdate, jump, onJumped,
        route: { here, via, why: routeWhy, onRecheck, onOpen: openRoute },
    };

    const wide = useWide();
    const still = asOf({ conn, ageSec, snapshot });
    const sheet = html`
        <${RouteSheet}
            open=${routeOpen}
            onClose=${closeRoute}
            route=${shared.route}
            conn=${conn}
            ageSec=${ageSec}
            insecure=${!!(snapshot && snapshot.cookieInsecure)}
            onRetry=${refresh}
        />
    `;
    if (wide) {
        return html`
            <${AsOf.Provider} value=${still}>
                <${DesktopShell} ...${shared} />
            <//>
            ${sheet}
        `;
    }

    return html`
        <${AsOf.Provider} value=${still}>
            <${MobileShell}
                ...${shared}
                conn=${conn}
                installable=${installable}
                onInstall=${onInstall}
            />
        <//>
        ${sheet}
    `;
}
