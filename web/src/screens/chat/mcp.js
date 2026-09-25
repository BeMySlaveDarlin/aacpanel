// The MCP servers of a session on the stream, as its claude reports them: the
// list by where each server comes from, and one server with what can be done
// to it. The panel asks the session each time the screen opens, so what it
// shows is the session's word now, not a copy from earlier.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { plural } from "../../format.js";

export const MCP_TITLE = "MCP servers";

// Where a server comes from, in the order the client lists them.
const GROUPS = [
    { id: "user", title: "User" },
    { id: "project", title: "Project" },
    { id: "local", title: "Local" },
    { id: "claudeai", title: "claude.ai" },
    { id: "plugin", title: "Plugins and built-in" },
];

// statusOf says how a server stands, in the words and the tone of the client.
export function statusOf(server) {
    const tools = (server.tools || []).length;
    switch (server.status) {
    case "connected":
        return { tone: "ok", word: "connected", note: `${tools} ${plural(tools, "tool", "tools")}` };
    case "needs-auth":
        return { tone: "warn", word: "needs authentication", note: "needs authentication" };
    case "pending":
        return { tone: "off", word: "connecting", note: "connecting" };
    case "disabled":
        return { tone: "off", word: "disabled", note: "disabled" };
    case "failed":
        // A server with no address was never set up, and the client says so
        // rather than calling it broken.
        if (/^No URL configured/i.test(server.error || "")) {
            return { tone: "off", word: "not configured", note: "not configured" };
        }
        return { tone: "crit", word: "failed", note: server.error ? "" : "failed" };
    default:
        return { tone: "off", word: server.status || "unknown", note: server.status || "" };
    }
}

// groupsOf lays the servers out by where they come from; a source the list
// does not know goes last, under its own name.
export function groupsOf(servers) {
    const known = new Map(GROUPS.map((g) => [g.id, { ...g, servers: [] }]));
    const other = new Map();
    for (const s of servers || []) {
        const id = s.source || s.scope || "other";
        const group = known.get(id) || other.get(id) || { id, title: id, servers: [] };
        if (!known.has(id)) other.set(id, group);
        group.servers.push(s);
    }
    return [...known.values(), ...other.values()].filter((g) => g.servers.length);
}

function useMcp(name, tick) {
    const [answer, setAnswer] = useState(null);
    useEffect(() => {
        let alive = true;
        setAnswer(null);
        fetch(`/api/session/mcp?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => { if (alive) setAnswer(body); })
            .catch((err) => { if (alive) setAnswer({ state: "unknown", reason: String(err.message || err) }); });
        return () => { alive = false; };
    }, [name, tick]);
    return answer;
}

// McpSheet is what /mcp opens in a session on the stream.
export function McpSheet({ name, exec }) {
    const [tick, setTick] = useState(0);
    const [open, setOpen] = useState("");
    const data = useMcp(name, tick);
    const again = () => setTick((n) => n + 1);

    const head = html`
        <div class="shead cmdtitle">
            <span class="cmdhead">${MCP_TITLE}</span>
            <button class="cmdcopy" type="button" aria-label="ask the session again" onClick=${again}>
                ${Icon.refresh()}
            </button>
        </div>
    `;
    if (!data) return html`<div class="cmdsheet">${head}<p class="cmdnote">Asking the session…</p></div>`;
    if (data.state !== "ok") {
        return html`<div class="cmdsheet">${head}<p class="cmdnote">${data.reason || "The session did not answer."}</p></div>`;
    }
    if (data.transport !== "stream") {
        return html`
            <div class="cmdsheet">${head}
                <p class="cmdnote">This session runs in a console: its servers are on its own screen, /mcp with keys.
                    The panel lists them for a session in the feed.</p>
            </div>
        `;
    }
    const servers = data.servers || [];
    const chosen = open && servers.find((s) => s.name === open);
    if (chosen) {
        return html`<${McpServer} name=${name} exec=${exec} server=${chosen}
                                  onBack=${() => setOpen("")} onDone=${again} />`;
    }
    return html`
        <div class="cmdsheet">
            ${head}
            <p class="cmdsum">${servers.length} ${plural(servers.length, "server", "servers")}</p>
            ${groupsOf(servers).map((g) => html`
                <section key=${g.id} class="cmdsec">
                    <div class="cmdsechead"><span>${g.title}</span></div>
                    <ul class="mcplist">
                        ${g.servers.map((s) => {
                            const st = statusOf(s);
                            return html`
                                <li key=${s.name}>
                                    <button type="button" class="mcprow" onClick=${() => setOpen(s.name)}>
                                        <i class=${`mcpdot ${st.tone}`} aria-hidden="true"></i>
                                        <span class="cmdname">${s.name}</span>
                                        <span class="cmdrownote">${st.note}</span>
                                        <span class="crgo">${Icon.chevron()}</span>
                                    </button>
                                </li>
                            `;
                        })}
                    </ul>
                </section>
            `)}
        </div>
    `;
}

// McpServer is one server: how it stands, where it lives, and what can be
// done to it. Authentication goes through a browser that comes back to the
// host, and a phone is not that browser — it is left to the host.
function McpServer({ name, exec, server, onBack, onDone }) {
    const run = useAction();
    const [busy, setBusy] = useState("");
    const st = statusOf(server);
    const can = knows(exec, "session.mcp");
    const why = can ? "" : whyNot(exec, "session.mcp");
    const off = server.status === "disabled";

    // The list is asked again whatever the answer: claude refuses to turn on a
    // server it cannot reach, and has turned it on all the same.
    const act = async (what) => {
        if (!can || busy) return;
        setBusy(what);
        const result = await run("session.mcp", name, { server: server.name, do: what });
        setBusy("");
        if (!result || !result.cancelled) onDone();
    };
    const where = server.url || server.command || "";
    const tools = server.tools || [];

    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle">
                <button class="cmdcopy mcpback" type="button" aria-label="back to the list" onClick=${onBack}>
                    ${Icon.chevron()}
                </button>
                <span class="cmdhead">${server.name}</span>
            </div>
            <ul class="cmdrows mcpfacts">
                <li><span class="cmdname">Status</span>
                    <span class="cmdtok"><i class=${`mcpdot ${st.tone}`} aria-hidden="true"></i>${st.word}</span></li>
                ${server.error && html`<li><span class="cmdname">Issue</span><span class="cmdtok mcpissue">${server.error}</span></li>`}
                ${where && html`<li><span class="cmdname">${server.url ? "URL" : "Command"}</span><span class="cmdtok mcpwhere">${where}</span></li>`}
                ${server.type && html`<li><span class="cmdname">Transport</span><span class="cmdtok">${server.type}</span></li>`}
                ${server.title && html`<li><span class="cmdname">Server</span>
                    <span class="cmdtok">${server.title}${server.version ? ` ${server.version}` : ""}</span></li>`}
                <li><span class="cmdname">Tools</span><span class="cmdtok">${tools.length}</span></li>
            </ul>
            ${server.description && html`<p class="cmdnote">${server.description}</p>`}
            ${server.status === "needs-auth" && html`
                <p class="cmdnote">Authentication opens a browser and comes back to the host: do it in the console on the host.</p>`}
            <div class="mcpacts">
                ${!off && html`
                    <button type="button" class="btn" disabled=${!can || Boolean(busy)} title=${why || undefined}
                            onClick=${() => act("reconnect")}>${busy === "reconnect" ? "Reconnecting…" : "Reconnect"}</button>`}
                <button type="button" class="btn" disabled=${!can || Boolean(busy)} title=${why || undefined}
                        onClick=${() => act(off ? "enable" : "disable")}>${off ? "Enable" : "Disable"}</button>
            </div>
            ${tools.length > 0 && html`
                <section class="cmdsec">
                    <div class="cmdsechead"><span>Tools</span><span class="cmdaside">${tools.length}</span></div>
                    <ul class="cmdrows">${tools.map((t) => html`<li key=${t}><span class="cmdname">${t}</span></li>`)}</ul>
                </section>
            `}
        </div>
    `;
}
