// What a session says about itself, the way the client's /status card does:
// what it runs, where, since when and under which id; its MCP servers in a
// line; and on the stream the account it works as. The panel knows most of it
// from the snapshot of the host, and asks the session for the rest.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { ago, plural, share, tokens } from "../../format.js";
import { copyText } from "./copy.js";
import { modelTitle } from "./head.js";
import { effortName, modeName } from "./picker.js";

export const STATUS_TITLE = "Session info";

function useAnswer(url, on) {
    const [answer, setAnswer] = useState(null);
    useEffect(() => {
        if (!on) return undefined;
        let alive = true;
        fetch(url, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => { if (alive) setAnswer(body); })
            .catch((err) => { if (alive) setAnswer({ state: "unknown", reason: String(err.message || err) }); });
        return () => { alive = false; };
    }, [url, on]);
    return answer;
}

// mcpLine sums the servers up the way a line has room for.
export function mcpLine(servers) {
    const list = servers || [];
    if (!list.length) return "None";
    const count = (status) => list.filter((s) => s.status === status).length;
    const parts = [];
    const on = count("connected");
    if (on) parts.push(`${on} connected`);
    const auth = count("needs-auth");
    if (auth) parts.push(`${auth} need${auth === 1 ? "s" : ""} authentication`);
    const failed = count("failed");
    if (failed) parts.push(`${failed} failed`);
    return parts.length ? parts.join(" · ") : `${list.length} ${plural(list.length, "server", "servers")}`;
}

// planName names a plan the way it is sold.
export function planName(plan) {
    const known = { max: "Max", pro: "Pro", team: "Team", enterprise: "Enterprise", free: "Free" };
    return known[plan] || plan || "";
}

// StatusSheet is what /status opens.
export function StatusSheet({ name, live }) {
    const toast = useToast();
    const stream = live.transport === "stream";
    const status = useAnswer(`/api/session/status?name=${encodeURIComponent(name)}`, true);
    const mcp = useAnswer(`/api/session/mcp?name=${encodeURIComponent(name)}`, stream);
    const version = (status && status.version) || "";
    const account = status && status.account;
    const model = [modelTitle(live.model || ""), live.effort && effortName(live.effort)].filter(Boolean).join(" · ");
    const rows = [
        ["Claude Code", version || "—"],
        ["Runs in", stream ? "the feed" : "a console"],
        ["Model", model || "—"],
        ["Permissions", live.mode ? modeName(live.mode) : "—"],
        ["Started", live.startedAt ? ago(live.startedAt) : "—"],
        ["Context", live.pct == null ? "—"
            : `${share(live.pct)}${live.tokens ? ` · ${tokens(live.tokens)}${live.limit ? ` of ${tokens(live.limit)}` : ""}` : ""}`],
        ["Tokens", live.tokensIn > 0 ? `${tokens(live.tokensIn)} in · ${tokens(live.tokensOut || 0)} out` : "—"],
        ["Messages", String(live.messages || 0)],
    ];
    const servers = !stream ? "on its own screen, /mcp with keys"
        : !mcp ? "asking…"
        : mcp.state === "ok" ? mcpLine(mcp.servers) : (mcp.reason || "unknown");
    const who = account ? [account.email, planName(account.plan)].filter(Boolean).join(" · ") : "";
    const text = () => [
        STATUS_TITLE,
        ...rows.map(([k, v]) => `${k}: ${v}`),
        `Session ID: ${live.sessionId || "—"}`,
        `MCP servers: ${servers}`,
        ...(who ? [`Signed in as: ${who}`] : []),
        ...(account && account.organization ? [`Organization: ${account.organization}`] : []),
    ].join("\n");

    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle">
                <span class="cmdhead">${STATUS_TITLE}</span>
                <button class="cmdcopy" type="button" aria-label="copy the card"
                        onClick=${() => copyText(text(), toast, "Copied", STATUS_TITLE)}>${Icon.copy()}</button>
            </div>
            <section class="cmdsec">
                <div class="cmdsechead"><span>Session</span></div>
                <ul class="cmdrows mcpfacts">
                    ${rows.map(([k, v]) => html`<li key=${k}><span class="cmdname">${k}</span><span class="cmdtok">${v}</span></li>`)}
                    <li><span class="cmdname">Session ID</span>
                        <span class="cmdtok mcpwhere">${live.sessionId || "—"}</span>
                        ${live.sessionId && html`
                            <button class="cmdcopy statcopy" type="button" aria-label="copy the session id"
                                    onClick=${() => copyText(live.sessionId, toast, "Copied", "Session ID")}>${Icon.copy()}</button>`}
                    </li>
                </ul>
            </section>
            <section class="cmdsec">
                <div class="cmdsechead"><span>Environment</span></div>
                <ul class="cmdrows mcpfacts">
                    <li><span class="cmdname">MCP servers</span><span class="cmdtok">${servers}</span></li>
                </ul>
            </section>
            ${account && html`
                <section class="cmdsec">
                    <div class="cmdsechead"><span>Account</span></div>
                    <ul class="cmdrows mcpfacts">
                        ${who && html`<li><span class="cmdname">Signed in as</span><span class="cmdtok">${who}</span></li>`}
                        ${account.organization && html`
                            <li><span class="cmdname">Organization</span><span class="cmdtok">${account.organization}</span></li>`}
                    </ul>
                </section>
            `}
            ${status && status.state !== "ok" && html`<p class="cmdnote">${status.reason}</p>`}
        </div>
    `;
}
