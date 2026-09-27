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
