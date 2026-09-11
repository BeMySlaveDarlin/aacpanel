// The voice of a watch: whether a background task has shown signs of life, and when.

// TASK_HUSH_MS is how long a watch may stay silent and still count as ordinary.
export const TASK_HUSH_MS = 2 * 60 * 60 * 1000;

// taskVoice returns what to say about the voice of a task, or null to say nothing.
export function taskVoice(task, now = Date.now()) {
    if (!task || task.kind !== "aacpanel") return null;
    if (!task.event) return { silent: true, hush: true };
    const at = Date.parse(task.event);
    if (Number.isNaN(at)) return null;
    return { silent: false, hush: now - at > TASK_HUSH_MS };
}
