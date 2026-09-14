import { render } from "preact";
import { useEffect, useState } from "preact/hooks";

import { App } from "./app.js";
import { Login } from "./screens/login.js";
import { GateHost } from "./actions/gate.js";
import { ToastHost } from "./ui/toasts.js";
import { html } from "./html.js";
import * as pwa from "./pwa.js";
import * as api from "./api.js";

import "../vendor/uplot.css";
import "../vendor/xterm.css";
import "./app.css";

function Root() {
    if (location.pathname === "/login") {
        const params = new URLSearchParams(location.search);
        return html`<${Login}
            next=${safeNext(params.get("next"))}
            reason=${params.get("reason") || ""}
            afterSec=${Number(params.get("after")) || 0}
        />`;
    }
    return html`<${Shell} />`;
}

function safeNext(next) {
    if (!next || !next.startsWith("/") || next.startsWith("//") || next.includes("\\")) return "/app";
    if (next === "/") return "/app";
    return next;
}

function Shell() {
    const [updateReady, setUpdateReady] = useState(false);
    const [updating, setUpdating] = useState(false);
    const [installable, setInstallable] = useState(false);

    useEffect(() => {
        pwa.watchInstall(setInstallable);
        pwa.register(() => setUpdateReady(true));
    }, []);

    const applyUpdate = () => {
        setUpdating(true);
        pwa.apply();
    };

    return html`
        <${ToastHost}>
        <${GateHost}>
            <${App}
                updateReady=${updateReady}
                updating=${updating}
                onApplyUpdate=${applyUpdate}
                installable=${installable}
                onInstall=${pwa.install}
            />
        <//>
        <//>
    `;
}

api.install();

const root = document.getElementById("app");
if (root) {
    render(html`<${Root} />`, root);
} else {
    console.error("aacpanel: the page has no #app — nowhere to render");
}
