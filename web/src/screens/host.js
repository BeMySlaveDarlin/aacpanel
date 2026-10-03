// The "Host" tab: the containers, the machine and the journal of the host,
// paged by swipe the way the sessions page their contours.
import { html } from "../html.js";
import { Chips } from "../ui/chips.js";
import { Icon } from "../ui/icons.js";
import { level, pct, rate } from "../format.js";
import { Containers, filterChips } from "./containers.js";
import { Journal } from "./journal.js";
import { Machine, machineStats } from "./machine.js";
import { Pages, useProfilePage } from "./sessions/pages.js";

const PAGE_KEY = "aacpanel.host.page";

const PAGES = ["containers", "machine", "journal"];

const LABEL = { containers: "Containers", machine: "Machine", journal: "Journal" };

// Host renders the tab. The row of pages stands first under the header on
// every page; the strip of the machine and the filters belong to the
// containers and turn with their page. The machine and the journal stand here
// without the way back they have when the host menu opens them over the
// screen.
export function Host({
    tree, treeError, filter, onFilter, query, open, onToggle, onLogs, onDone, exec,
    snapshot, hostError, ageSec, history, faults,
}) {
    const [current, pick] = useProfilePage(PAGES, PAGE_KEY);
    const machine = machineStats(snapshot);

    return html`
        <${Pages}
            names=${PAGES}
            current=${current}
            onPick=${pick}
            label=${(name) => LABEL[name]}
            what="pages"
            page=${(name) => {
                if (name === "machine") {
                    return html`<${Machine} snapshot=${snapshot} error=${hostError} ageSec=${ageSec}
                        history=${history} faults=${faults} />`;
                }
                if (name === "journal") return html`<${Journal} />`;
                return html`
                    ${machine && html`<${HostBar} machine=${machine} onMachine=${() => pick("machine")} />`}
                    <${Chips} items=${filterChips(tree)} current=${filter} onSelect=${onFilter} />
                    <${Containers} tree=${tree} error=${treeError} filter=${filter} query=${query}
                        open=${open} onToggle=${onToggle} onLogs=${onLogs} onDone=${onDone} exec=${exec} />
                `;
            }}
        />
    `;
}

// HostBar is the strip of the machine over the containers: three figures of
// the host and the way to its page.
function HostBar({ machine, onMachine }) {
    return html`
        <div class="hostbar">
            <div class="hstats">
                <${Stat} title="cpu" value=${pct(machine.cpuPct)} fill=${machine.cpuPct} />
                <${Stat} title="memory" value=${pct(machine.memPct)} fill=${machine.memPct} />
                <${Stat} title="network" value=${rate(machine.net)} fill=${null} />
            </div>
            <button class="hdetails" type="button" onClick=${onMachine}>
                details
                <span class="chev">${Icon.chevron()}</span>
            </button>
        </div>
    `;
}

function Stat({ title, value, fill }) {
    return html`
        <div class="hstat">
            <span class="hstat-title">${title}</span>
            <span class="hstat-value">${value}</span>
            <div class=${`bar${fill == null ? " blank" : ""}`}>
                ${fill != null && html`
                    <i class=${level(fill)} style=${`width:${Math.min(100, fill)}%`}></i>
                `}
            </div>
        </div>
    `;
}
