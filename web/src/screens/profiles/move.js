// moveIds returns the ids of one level of the map with two neighbours swapped.
export function moveIds(items, index, dir) {
    const j = index + (dir === "up" ? -1 : 1);
    if (j < 0 || j >= items.length) return null;
    const ids = items.map((it) => it.id);
    [ids[index], ids[j]] = [ids[j], ids[index]];
    return ids;
}
