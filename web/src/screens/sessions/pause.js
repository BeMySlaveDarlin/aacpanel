// A pause is one service action. The recipient list previews the same map
// scope; the service resolves it again when the person presses the button.
import { knows, whyNot } from "../../exec.js";
import { openedBy } from "./kin.js";
import { sessionsOf } from "./of.js";

export function groupOf(project, profile) {
    return (profile?.groups || []).find((g) => (g.projects || []).some((p) => p.id === project.id));
}

// pauseRows lists the sessions a pause reaches: those of the project,
// of its whole group, or the one session it is given, and one step down the
// sessions any of them had the panel open, wherever those run, as the
// service takes them. Alone excludes the sessions the given session opened.
export function pauseRows(project, profile, sessions = [], group = false, only = null, alone = false) {
    const reached = (s) => !s.outside && s.session && s.sessionId;
    const projects = group ? groupOf(project, profile)?.projects || [project] : [project];
    const scope = only ? [only].filter(reached) : sessions.filter((s) => reached(s) && projects.some((p) => sessionsOf(p, [s]).length));
    if (alone) return scope;
    const names = new Set(scope.map((s) => s.session));
    return [...scope, ...sessions.filter((s) => reached(s) && !names.has(s.session) && names.has(openedBy(s)))];
}

export function pauseWhy(project, profile, sessions, exec) {
    return (knows(exec, "project.pause") ? "" : whyNot(exec, "project.pause")) || (pauseRows(project, profile, sessions, true).length ? "" : "No sessions in this project or group");
}

// sessionPauseWhy says why one session cannot be paused or continued now: a
// pause is addressed through the project of the map that holds it, and
// reaches a conversation, not a process.
export function sessionPauseWhy(s, project, exec) {
    if (!project) return "No project of the map holds this session";
    return (knows(exec, "project.pause") ? "" : whyNot(exec, "project.pause")) || (s.sessionId ? "" : "The session has no conversation yet");
}

// continueProject lifts the pause of every session of a project now.
export function continueProject(run, project) {
    return run("project.pause", project.session || project.name, { project: project.id, group: false, resume: true });
}

function localInput(date) {
    const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
    return local.toISOString().slice(0, 16);
}

// Carry the selected date's offset, so the instruction names the person's
// local clock even when the service runs in another timezone.
export function pauseUntil(value) {
    const date = new Date(value);
    if (!Number.isFinite(date.getTime())) return "";
    const offset = -date.getTimezoneOffset();
    const n = Math.abs(offset);
    return `${localInput(date)}:00${offset < 0 ? "-" : "+"}${String(Math.floor(n / 60)).padStart(2, "0")}:${String(n % 60).padStart(2, "0")}`;
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
export const pauseHour = (date) => `${date.getHours()}:${String(date.getMinutes()).padStart(2, "0")}`;
export const pauseDay = (date) => WEEKDAYS[date.getDay()];

export function pauseAway(value, now = Date.now()) {
    const min = Math.max(0, Math.round((new Date(value).getTime() - now) / 60000));
    const days = Math.floor(min / 1440);
    const hours = Math.floor((min % 1440) / 60);
    const minutes = min % 60;
    if (days) return `${days} d ${hours} h`;
    return hours ? `${hours} h${minutes ? ` ${minutes} min` : ""}` : `${minutes} min`;
}

// The card and button keep the weekday short; the dial, conversation and
// letter name the date too. Every surface omits the day for today's end.
export function pauseEnd(value, { full = false, distance = false, now = Date.now() } = {}) {
    if (!value) return "until lifted";
    const date = new Date(value);
    const today = date.toDateString() === new Date(now).toDateString();
    const day = today ? "" : full ? `${pauseDay(date)} ${date.getDate()} ${MONTHS[date.getMonth()]}, ` : `${pauseDay(date)} `;
    return `until ${day}${pauseHour(date)}${distance ? ` · in ${pauseAway(date, now)}` : ""}`;
}
