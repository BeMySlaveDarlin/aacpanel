// The "Host" tab: the containers, the machine and the journal of the host,
// paged by swipe the way the sessions page their contours.
import { html } from "../html.js";
import { Chips } from "../ui/chips.js";
import { Containers, filterChips } from "./containers.js";
import { Journal } from "./journal.js";
import { Machine } from "./machine.js";
import { Pages, useProfilePage } from "./sessions/pages.js";

const PAGE_KEY = "aacpanel.host.page";

const PAGES = ["containers", "machine", "journal"];

const LABEL = { containers: "Containers", machine: "Machine", journal: "Journal" };

// Host renders the tab. The row of pages stands first under the header on
// every page; the filters belong to the containers and turn with their page.
// The figures of the machine are its own page, not a strip over the
// containers. The machine and the journal stand here without the way back
// they have when the host menu opens them over the screen.
export function Host({
    tree, treeError, filter, onFilter, query, open, onToggle, onLogs, onDone, exec,
    snapshot, hostError, ageSec, history, faults,
}) {
    const [current, pick] = useProfilePage(PAGES, PAGE_KEY);

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
                        history=${history} faults=${faults} exec=${exec} onDone=${onDone} />`;
                }
                if (name === "journal") return html`<${Journal} />`;
                return html`
                    <${Chips} items=${filterChips(tree)} current=${filter} onSelect=${onFilter} />
                    <${Containers} tree=${tree} error=${treeError} filter=${filter} query=${query}
                        open=${open} onToggle=${onToggle} onLogs=${onLogs} onDone=${onDone} exec=${exec} />
                `;
            }}
        />
    `;
}
