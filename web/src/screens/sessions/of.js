import { ownName } from "../../catchup.js";

// sessionsOf returns the sessions of one project. The service says which
// project a live session belongs to — by its directory, a worktree of the
// project included — and null when none; the name is the guess only for a
// snapshot the service could not place.
export function sessionsOf(project, sessions) {
    return sessions.filter((s) => (s.project === undefined
        ? ownName(s.session, project.session)
        : Boolean(s.project) && s.project.id === project.id));
}

// inOrder lays the live sessions of a project out as the list shows them: the
// session named after the project first — the project's own conversation, by
// the rule that tells it apart — whatever it is doing, then the rest by when
// they last asked the model, the latest on top.
export function inOrder(project, list) {
    const name = project && project.session;
    const lead = (s) => (name && ownName(s.session, name) ? 0 : 1);
    const at = (s) => Date.parse(s.lastRequestAt || s.startedAt || "") || 0;
    return [...list].sort((a, b) => lead(a) - lead(b) || at(b) - at(a) || a.session.localeCompare(b.session));
}
