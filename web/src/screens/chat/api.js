// The address of a conversation in chat requests: one for every handle of the screen.

// idParam returns the conversation identifier as a query parameter.
export function idParam(id) {
    return id ? `&id=${encodeURIComponent(id)}` : "";
}
