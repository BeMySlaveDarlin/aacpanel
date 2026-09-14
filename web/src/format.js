const UNITS = ["B", "KB", "MB", "GB", "TB"];

export function bytes(value) {
    if (!value) return "0 B";
    let n = value;
    let unit = 0;
    while (n >= 1024 && unit < UNITS.length - 1) {
        n /= 1024;
        unit += 1;
    }
    return `${n < 10 && unit > 0 ? n.toFixed(1) : Math.round(n)} ${UNITS[unit]}`;
}

export function rate(bytesPerSec) {
    return `${bytes(bytesPerSec)}/s`;
}

const COUNT = [
    [1e12, "tn"],
    [1e9, "bn"],
    [1e6, "m"],
    [1e3, "k"],
];

export function tokens(value) {
    const n = value || 0;
    for (const [step, word] of COUNT) {
        if (Math.abs(n) >= step) {
            const scaled = n / step;
            return `${Math.abs(scaled) < 100 ? Math.round(scaled * 10) / 10 : Math.round(scaled)}${word}`;
        }
    }
    return `${Math.round(n)}`;
}

export function pct(value) {
    return `${Math.round(value)}%`;
}

export function share(value) {
    return `${Math.round((value || 0) * 10) / 10}%`;
}

export function duration(sec) {
    if (sec < 60) return `${Math.round(sec)} s`;
    if (sec < 3600) return `${Math.round(sec / 60)} min`;

    const hours = Math.floor(sec / 3600);
    if (hours < 24) return `${hours} h ${Math.floor((sec % 3600) / 60)} m`;
    return `${Math.floor(hours / 24)} d ${hours % 24} h`;
}

export function level(percent) {
    if (percent >= 90) return "crit";
    if (percent >= 70) return "warn";
    return "ok";
}

export function fill(percent) {
    if (percent >= 70) return "s4";
    if (percent >= 50) return "s3";
    if (percent >= 30) return "s2";
    return "s1";
}

export function ago(iso) {
    if (!iso) return "—";
    const sec = (Date.now() - new Date(iso).getTime()) / 1000;
    if (sec < 0) return "just now";
    if (sec < 60) return `${Math.round(sec)} s ago`;
    if (sec < 3600) return `${Math.round(sec / 60)} min ago`;
    if (sec < 86400) return `${Math.floor(sec / 3600)} h ago`;
    return `${Math.floor(sec / 86400)} d ago`;
}

export function until(iso) {
    if (!iso) return "—";
    const sec = (new Date(iso).getTime() - Date.now()) / 1000;
    if (sec <= 0) return "now";
    if (sec < 60) return `in ${Math.round(sec)} s`;
    if (sec < 3600) return `in ${Math.round(sec / 60)} min`;
    const h = Math.floor(sec / 3600);
    const m = Math.round((sec % 3600) / 60);
    return m ? `in ${h} h ${m} min` : `in ${h} h`;
}

export function since(iso) {
    if (!iso) return "—";
    return duration(Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000));
}

export function plural(n, one, many) {
    return n === 1 ? one : many;
}

// uptime says how long the machine has been up, in the two largest units
// that matter for the scale: days and hours, or hours and minutes.
export function uptime(sec) {
    if (!sec || sec <= 0) return "";
    const days = Math.floor(sec / 86400);
    const hours = Math.floor((sec % 86400) / 3600);
    const minutes = Math.floor((sec % 3600) / 60);
    if (days > 0) return `up ${days} ${plural(days, "day", "days")}${hours > 0 ? ` ${hours} h` : ""}`;
    if (hours > 0) return `up ${hours} h${minutes > 0 ? ` ${minutes} min` : ""}`;
    return `up ${minutes} min`;
}

export function degrees(value) {
    return `${Math.round(value)} °C`;
}

// withDegrees joins a temperature to a line only when there is a sensor
// behind it: a machine without one gets neither a zero nor a dash, the line
// simply says less.
export function withDegrees(text, temp) {
    return temp == null ? text : `${text} · ${degrees(temp)}`;
}

export function span(sec) {
    if (!sec || sec <= 0) return "";
    if (sec < 3600) {
        const minutes = Math.round(sec / 60);
        return `${minutes} ${plural(minutes, "minute", "minutes")}`;
    }
    const hours = Math.round(sec / 3600);
    return `${hours} ${plural(hours, "hour", "hours")}`;
}
