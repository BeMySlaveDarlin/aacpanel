// The schema of the launch parameters, as the service describes them: the
// pages draw their rows from it. It is built into the service and does not
// change while a page is open, so it is asked once.
import { useEffect, useState } from "preact/hooks";

let cached = null;
let asking = null;

async function ask() {
    const response = await fetch("/api/profiles/schema", { credentials: "same-origin" });
    if (!response.ok) throw new Error(`the schema did not come: ${response.status}`);
    return response.json();
}

export function useSchema() {
    const [schema, setSchema] = useState(cached);
    const [error, setError] = useState("");
    useEffect(() => {
        if (cached) return undefined;
        let live = true;
        asking = asking || ask();
        asking.then(
            (got) => {
                cached = got;
                if (live) setSchema(got);
            },
            (err) => {
                asking = null;
                if (live) setError(err.message);
            },
        );
        return () => {
            live = false;
        };
    }, []);
    return { schema, error };
}

// paramOf returns the parameter of a key from the schema.
export function paramOf(schema, key) {
    return ((schema && schema.params) || []).find((p) => p.key === key) || null;
}
