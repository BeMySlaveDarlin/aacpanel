// Since when what the screens show has stood still. The header chip speaks for
// the link; a line that shows a state of its own asks here, so as not to say
// "answering" about a snapshot nobody has refreshed since.
import { createContext } from "preact";
import { useContext } from "preact/hooks";

// The value is null while the data is live, and the time of the snapshot on
// the screen otherwise — empty where that time is unknown. A screen outside
// the provider is live.
export const AsOf = createContext(null);

export function useAsOf() {
    return useContext(AsOf);
}
