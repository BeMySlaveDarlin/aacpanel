// Acts carry their state and their form even outside the loaded feed window.
export async function list(session) {
    const response = await fetch(`/api/acts?session=${encodeURIComponent(session)}`, { cache: "no-store" });
    if (!response.ok) throw new Error((await response.text()).trim() || "The acts could not be read.");
    const data = await response.json();
    return (data.acts || []).map((row) => ({
        ...row, ...row.body, answered: ["answered", "declined"].includes(row.state) ? row.closed : null,
        ...(row.kind === "claim" ? { due: row.due } : {}),
        withdrawn: row.state === "withdrawn" ? row.closed : null,
        outcome: row.answer && row.answer.outcome, note: row.answer && row.answer.note || "",
    }));
}

export function overdueMinutes(claim) {
    const due = Date.parse(claim?.due || "");
    return Number.isFinite(due) && due <= Date.now() ? Math.floor((Date.now() - due) / 60000) : null;
}
