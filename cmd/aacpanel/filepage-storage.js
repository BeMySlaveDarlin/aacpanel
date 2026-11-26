(() => {
    for (const name of ["localStorage", "sessionStorage"]) {
        try {
            window[name];
            continue;
        } catch {
            // Opaque origins deny storage. Each document gets its own memory,
            // without granting the file access to the panel's origin.
        }
        const values = new Map();
        const methods = {
            getItem(key) { return values.get(String(key)) ?? null; },
            setItem(key, value) { values.set(String(key), String(value)); },
            removeItem(key) { values.delete(String(key)); },
            clear() { values.clear(); },
            key(index) { return [...values.keys()][index >>> 0] ?? null; },
            get length() { return values.size; }
        };
        const storage = new Proxy(methods, {
            get(target, key, receiver) {
                return Reflect.has(target, key) ? Reflect.get(target, key, receiver) : values.get(String(key));
            },
            set(target, key, value) { values.set(String(key), String(value)); return true; },
            deleteProperty(target, key) { values.delete(String(key)); return true; },
            ownKeys() { return [...values.keys()]; },
            getOwnPropertyDescriptor(target, key) {
                if (values.has(String(key))) return { value: values.get(String(key)), writable: true, enumerable: true, configurable: true };
            }
        });
        Object.defineProperty(window, name, { value: storage, configurable: true });
    }
})();
