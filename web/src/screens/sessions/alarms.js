// Alarm times come from the host; a pause exists only once its alarm does.
import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";

export function alarmTime(at, date = false) {
    const value = new Date(at);
    if (!Number.isFinite(value.getTime())) return "";
    const time = value.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
    return date ? `${time}, ${value.toLocaleDateString("en-GB", { day: "numeric", month: "short" })}` : time;
}

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
