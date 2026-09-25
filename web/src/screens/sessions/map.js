// Profiles, groups and projects: whose project it is, where it lies and what is
// alive in it.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { plural } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { ownName } from "../../catchup.js";
import { Ghost, LiveRow } from "./card.js";

// sessionsOf returns the sessions of one project. The service says which
// project a live session belongs to — by its directory, a worktree of the
// project included — and null when none; the name is the guess only for a
// snapshot the service could not place.
export function sessionsOf(project, sessions) {
    return sessions.filter((s) => (s.project === undefined
        ? ownName(s.session, project.session)
        : Boolean(s.project) && s.project.id === project.id));
}

// contoursOf returns which profile each session lives in.
export function contoursOf(profiles, sessions) {
    const byPath = new Map();
    const out = new Map();
    for (const profile of profiles) {
        for (const group of profile.groups) {
            for (const project of group.projects) {
                if (project.path) byPath.set(project.path, profile.profile);
                for (const s of sessionsOf(project, sessions)) out.set(s.session, profile.profile);
            }
        }
    }
    for (const s of sessions) {
        if (out.has(s.session)) continue;
        const known = s.cwd && byPath.get(s.cwd);
        if (known) out.set(s.session, known);
    }
    const byID = new Map((profiles || []).map((p) => [p.id, p.profile]));
    for (const s of sessions) {
        const named = s.contour && byID.get(s.contour);
        if (named) {
            out.set(s.session, named);
            continue;
        }
        if (s.profile) out.set(s.session, s.profile);
    }
    return out;
}

// pagesOf returns the sessions laid out across the profile pages.
export function pagesOf(profiles, sessions, names) {
    const where = contoursOf(profiles, sessions);
    const pages = names && names.length ? names : profiles.map((p) => p.profile);
    const out = new Map(pages.map((name) => [name, []]));
    const fallback = pages.length > 0 ? pages[0] : null;
    for (const s of sessions) {
        const page = out.get(where.get(s.session)) || (fallback !== null ? out.get(fallback) : null);
        if (page) page.push(s);
    }
    return out;
}

// placesOf returns where each session lives.
export function placesOf(profiles, sessions) {
    const places = new Map();
    for (const profile of profiles) {
        for (const group of profile.groups) {
            for (const project of group.projects) {
                for (const s of sessionsOf(project, sessions)) {
                    places.set(s.session, { profile: profile.profile, group: group.name });
                }
            }
        }
    }
    return places;
}

export function Groups({ profile, sessions, onOpen, exec }) {
    const [open, setOpen] = useState(() => new Set());
    const run = useAction();
    const ready = knows(exec, "session.open");
    const why = whyNot(exec, "session.open");

    const groups = profile.groups
        .map((group) => ({ ...group, shown: group.projects }))
        .filter((group) => group.shown.length > 0);

    if (groups.length === 0) return null;

    const toggle = (name) => setOpen((prev) => {
        const next = new Set(prev);
        if (next.has(name)) next.delete(name);
        else next.add(name);
        return next;
    });

    return html`
        <div class="grouphead">project groups</div>
        ${groups.map((group) => {
            const live = group.shown.filter((p) => sessionsOf(p, sessions).length > 0).length;
            const expanded = open.has(group.name);
            return html`
                <section class="stack" key=${group.name} data-open=${expanded ? "1" : "0"}>
                    <div class="stackhead">
                        <button class="stacktoggle" type="button" onClick=${() => toggle(group.name)}>
                            <span class="dot ${live > 0 ? "ok" : "off"}"></span>
                            <span class="stackname">${group.name}</span>
                            <span class="count">${live > 0
                                ? `${live} live`
                                : `${group.shown.length} ${plural(group.shown.length, "project", "projects")}`}</span>
                            <span class="chev">${Icon.chevron()}</span>
                        </button>
                    </div>
                    <div class="rows">
                        ${group.shown.map((p) => {
                            const own = sessionsOf(p, sessions);
                            return html`
                                <div class="prow ${own.length > 0 ? "live" : ""}" key=${p.path}>
                                    <button class="pmain" type="button" onClick=${() => onOpen({ ...p, profile: profile.profile, group: group.name })}>
                                        <div class="r1">
                                            <span class="nm">${p.name}</span>
                                            <span class="dot ${own.length > 0 ? "ok" : "off"}"></span>
                                        </div>
                                        <div class="meta">
                                            <div class="mline"><span class="path">${p.path}</span></div>
                                            ${own.length > 0 && html`
                                                <div class="mline">
                                                    <span>${own.length} ${plural(own.length, "live session", "live sessions")}</span>
                                                </div>
                                            `}
                                        </div>
                                        <span class="chev">${Icon.chevron()}</span>
                                    </button>
                                    <button
                                        class="iconbtn accent"
                                        type="button"
                                        aria-label=${own.length > 0 ? `open one more console ${p.name}` : `open console ${p.name}`}
                                        disabled=${!ready}
                                        title=${ready ? "open the console" : why}
                                        onClick=${async () => {
                                            await run("session.open", p.session, { project: p.id });
                                        }}
                                    >${Icon.plus()}</button>
                                </div>
                            `;
                        })}
                    </div>
                </section>
            `;
        })}
    `;
}

// Project renders the project screen.
export function Project({ project, sessions, notes, exec, wait, onBack, onChat }) {
    useBackClose(true, onBack);

    const run = useAction();
    const own = sessionsOf(project, sessions);
    const ready = knows(exec, "session.open");
    const why = whyNot(exec, "session.open");
    const opening = wait ? wait.of("open", project.session) : null;
    return html`
        <${BackHead} onBack=${onBack} label="to the projects">
            <h2>${project.name}</h2>
            <span class="path">${project.path}</span>
            <span class="where">${project.profile} · ${project.group}</span>
        <//>

        <div class="btnrow">
            <button
                class="btn primary"
                type="button"
                disabled=${!ready}
                onClick=${async () => {
                    await run("session.open", project.session, { project: project.id });
                }}
            >${own.length > 0 ? "Open one more console" : "Open the console"}</button>
        </div>
        ${!ready && html`<p class="hint warn">${why}</p>`}

        ${opening && html`<${Ghost} task=${opening} />`}

        ${own.length === 0
            ? !opening && html`<p class="empty">There are no sessions of this project right now.</p>`
            : own.map((s) => html`
                <${LiveRow} key=${s.session} session=${s} notes=${notes && notes.get(s.session)}
                    exec=${exec} wait=${wait} onOpen=${onChat} />
            `)}
    `;
}
