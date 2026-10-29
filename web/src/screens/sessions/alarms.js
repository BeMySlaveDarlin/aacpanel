// Alarm times come from the host; a pause exists only once its alarm does.
import { html } from "../../html.js";
import { alarmTime } from "../../format.js";
import { Icon } from "../../ui/icons.js";

export { alarmTime };

export function SessionAlarms({ session, paused, desktop = false }) {
    const pause = session.pauseAlarm;
    const next = session.nextAlarm;
    const cls = desktop ? "dkmark sessionalarm" : "pjalarm sessionalarm";
    return html`
        ${pause && !paused && html`<span class=${cls} title=${`Paused until ${alarmTime(pause.next, true)}`}>
            <${Icon.pause} />${alarmTime(pause.next)}</span>`}
        ${next && next.id !== pause?.id && html`<span class=${cls} title=${`${next.name} · ${alarmTime(next.next, true)}`}>
            <${Icon.clock} />${alarmTime(next.next)}</span>`}
    `;
}
