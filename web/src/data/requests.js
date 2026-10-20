// One read carries the live state of all request cards of a conversation.
export async function list(session) {
    const response = await fetch(`/api/requests?session=${encodeURIComponent(session)}`, { cache: "no-store" });
    if (!response.ok) throw new Error((await response.text()).trim() || "The requests could not be read.");
    const data = await response.json();
    return data.requests || [];
}
