// uPlot wrapper for preact.
import { useEffect, useRef } from "preact/hooks";
import uPlot from "uplot";

import { html } from "./html.js";

// spanRange builds a range from the actual spread rather than from zero.
export function spanRange(minWindow) {
    return (plot, dataMin, dataMax) => {
        if (dataMin == null || dataMax == null) return [0, 1];

        const span = dataMax - dataMin;
        if (span >= minWindow) {
            const pad = span * 0.15;
            return [dataMin - pad, dataMax + pad];
        }

        const middle = (dataMin + dataMax) / 2;
        return [middle - minWindow / 2, middle + minWindow / 2];
    };
}

// stroke returns the line colour taken from the palette on every repaint.
export function stroke(name, fallback) {
    return () => theme(name, fallback);
}

// tint returns a translucent palette colour for bands between series.
export function tint(name, fallback, alpha = 0.14) {
    return () => withAlpha(theme(name, fallback), alpha);
}

// area builds a series filled under its line.
export function area(name, fallback, { opacity = 0.5, scale } = {}) {
    return {
        ...(scale ? { scale } : {}),
        stroke: stroke(name, fallback),
        width: 2,
        points: { show: false },
        fill: (u, seriesIdx) => {
            const line = theme(name, fallback);
            const bottom = u.bbox.top + u.bbox.height;
            const grad = u.ctx.createLinearGradient(0, peakPos(u, seriesIdx), 0, bottom);
            grad.addColorStop(0, withAlpha(line, opacity));
            grad.addColorStop(1, withAlpha(line, 0));
            return grad;
        },
    };
}

// bars builds a series drawn as bars, for distributions over slots.
export function bars(name, fallback, { opacity = 0.5, scale, width = 0.62, max = 44 } = {}) {
    return {
        ...(scale ? { scale } : {}),
        stroke: stroke(name, fallback),
        width: 1,
        points: { show: false },
        fill: () => withAlpha(theme(name, fallback), opacity),
        paths: uPlot.paths.bars({ size: [width, max], align: 0, radius: 0.15 }),
    };
}

function peakPos(u, seriesIdx) {
    const top = u.bbox.top;
    const values = u.data[seriesIdx];
    if (!values || !values.length) return top;
    let max = null;
    for (const v of values) {
        if (v == null || Number.isNaN(v)) continue;
        if (max == null || v > max) max = v;
    }
    if (max == null) return top;
    const scale = u.series[seriesIdx].scale || "y";
    const pos = u.valToPos(max, scale, true);
    return Number.isFinite(pos) ? pos : top;
}

function withAlpha(color, alpha) {
    const hex = color.trim();
    if (hex.startsWith("#")) {
        const short = hex.length === 4;
        const part = (i) => parseInt(short ? hex[1 + i].repeat(2) : hex.slice(1 + i * 2, 3 + i * 2), 16);
        return `rgba(${part(0)}, ${part(1)}, ${part(2)}, ${alpha})`;
    }
    const nums = hex.match(/-?\d*\.?\d+/g);
    if (nums && nums.length >= 3) {
        return `rgba(${nums[0]}, ${nums[1]}, ${nums[2]}, ${alpha})`;
    }
    return "rgba(0, 0, 0, 0)";
}

function theme(name, fallback) {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return value || fallback;
}

function heightFor(el, fallback) {
    if (!el) return fallback;
    const raw = getComputedStyle(el).getPropertyValue("--chart-h").trim();
    const value = parseFloat(raw);
    return Number.isFinite(value) && value > 0 ? value : fallback;
}

function axisStyle() {
    return {
        stroke: theme("--ink-dim", "#bcc4e8"),
        font: "12px system-ui, sans-serif",
        grid: { stroke: theme("--line", "rgba(150,170,255,.14)"), width: 1 },
        ticks: { stroke: theme("--line", "rgba(150,170,255,.14)"), width: 1, size: 4 },
    };
}

function axisSize(plot, values, axisIdx, cycleNum) {
    const axis = plot.axes[axisIdx];

    let size = axis.ticks.size + axis.gap;
    const longest = (values || []).reduce((wide, v) => (v.length > wide.length ? v : wide), "");
    if (longest !== "") {
        plot.ctx.font = axis.font[0];
        size += plot.ctx.measureText(longest).width / devicePixelRatio;
    }
    size = Math.ceil(size);

    return cycleNum > 1 ? Math.max(size, axis._size) : size;
}

function pickOnClick(onPick) {
    return (plot) => {
        plot.over.addEventListener("click", () => {
            const idx = plot.cursor.idx;
            if (idx != null) onPick(idx);
        });
    };
}

const slotIncrs = [1, 2, 5, 10, 25, 50, 100, 250, 500, 1000];

function timeLabels(plot, ticks) {
    let previous = null;
    return ticks.map((sec) => {
        const at = new Date(sec * 1000);
        const time = at.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
        const day = at.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" });
        const label = day === previous ? time : `${time}\n${day}`;
        previous = day;
        return label;
    });
}

function slotRange(plot, min, max) {
    const xs = plot.data[0];
    let step = Infinity;
    for (let i = 1; xs && i < xs.length; i += 1) {
        const delta = Math.abs(xs[i] - xs[i - 1]);
        if (delta > 0 && delta < step) step = delta;
    }
    if (!Number.isFinite(step)) step = 1;
    return [min - step / 2, max + step / 2];
}

// chartOptions builds the uPlot options from the component props.
export function chartOptions({ series, width, height, compact = false, range, bands, yFormat, y2, xFormat, onPick }) {
    const options = {
        width,
        height,
        series,
        legend: { show: false },
        cursor: { y: false },
    };

    const scales = {};
    if (range) scales.y = { range };
    if (y2 && y2.range) scales.y2 = { range: y2.range };
    if (xFormat) scales.x = { time: false, range: slotRange };
    if (Object.keys(scales).length > 0) options.scales = scales;

    if (bands) options.bands = bands;

    if (onPick) options.hooks = { ready: [pickOnClick(onPick)] };

    if (compact) {
        options.axes = [{ show: false }, { show: false }];
        options.cursor = onPick ? { show: true, x: false, y: false, points: { show: false } } : { show: false };
        options.padding = [2, 0, 2, 0];
        return options;
    }

    const style = axisStyle();
    options.axes = [
        xFormat
            ? { ...style, incrs: slotIncrs, values: (plot, ticks) => ticks.map(xFormat) }
            : { ...style, values: timeLabels },
        { ...style, size: axisSize, ...(yFormat ? { values: (plot, ticks) => ticks.map(yFormat) } : {}) },
    ];
    if (y2) {
        options.axes.push({
            ...style,
            size: axisSize,
            scale: "y2",
            side: 1,
            grid: { show: false },
            ...(y2.format ? { values: (plot, ticks) => ticks.map(y2.format) } : {}),
        });
    }
    return options;
}

// Chart renders prepared uPlot data into a canvas.
export function Chart({ data, series, height = 200, compact = false, range, bands, yFormat, y2, xFormat, onPick }) {
    const outer = useRef(null);
    const inner = useRef(null);
    const plot = useRef(null);

    const pick = useRef(onPick);
    useEffect(() => {
        pick.current = onPick;
    });
    const picks = Boolean(onPick);

    useEffect(() => {
        const options = chartOptions({
            series,
            width: outer.current.clientWidth || 600,
            height: heightFor(outer.current, height),
            compact,
            range,
            bands,
            yFormat,
            y2,
            xFormat,
            onPick: picks ? (idx) => pick.current(idx) : undefined,
        });

        plot.current = new uPlot(options, data, inner.current);

        return () => {
            plot.current.destroy();
            plot.current = null;
        };
    }, [series, height, compact, range, bands, yFormat, y2, xFormat, picks]);

    useEffect(() => {
        if (plot.current) plot.current.setData(data);
    }, [data]);

    useEffect(() => {
        let last = 0;
        let lastHeight = 0;
        const observer = new ResizeObserver(() => {
            const width = outer.current.clientWidth;
            const h = heightFor(outer.current, height);
            if (!plot.current || width === 0) return;
            if (width === last && h === lastHeight) return;
            last = width;
            lastHeight = h;
            plot.current.setSize({ width, height: h });
        });
        observer.observe(outer.current);
        return () => observer.disconnect();
    }, [height]);

    return html`<div class="chart ${compact ? "spark" : ""}" ref=${outer}><div ref=${inner}></div></div>`;
}
