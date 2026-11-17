// One read carries the live state of all request cards of a conversation.
export async function list(session) {
    const response = await fetch(`/api/acts?session=${encodeURIComponent(session)}`, { cache: "no-store" });
    if (!response.ok) throw new Error((await response.text()).trim() || "The requests could not be read.");
    const data = await response.json();
    return (data.acts || []).filter((row) => row.kind === "do").map((row) => ({
        ...row, steps: row.body.steps, answered: row.state === "answered" || row.state === "declined" ? row.closed : null,
        withdrawn: row.state === "withdrawn" ? row.closed : null, answer: row.answer && row.answer.outcome,
        note: row.answer && row.answer.note || "",
    }));
}
