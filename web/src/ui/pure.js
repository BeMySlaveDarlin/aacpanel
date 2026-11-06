// Components drawn again only when what they are given changes.
import { Component, h } from "preact";
import { useCallback, useRef } from "preact/hooks";

const plain = (v) => Array.isArray(v)
    || (v !== null && typeof v === "object" && Object.getPrototypeOf(v) === Object.prototype);

// holds reports whether a and b are two plain objects or two arrays whose
// entries are alike.
function holds(a, b, alike) {
    if (!plain(a) || !plain(b) || Array.isArray(a) !== Array.isArray(b)) return false;
    const keys = Object.keys(a);
    if (keys.length !== Object.keys(b).length) return false;
    return keys.every((k) => Object.prototype.hasOwnProperty.call(b, k) && alike(a[k], b[k]));
}

// How deep same looks into plain data. A row of the feed is folded at most
// six deep — a run, its groups, a group, its calls, a call, the end of its
// task — and data that runs deeper, or round in a circle as a drawn node and
// its parent do, is taken for changed.
const DEPTH = 8;

// same says two values draw the same: the same value, or plain objects or
// arrays holding values that are the same; anything else is the same only as
// itself. The feed folds its rows again on every change of it — a run wrapped
// into a row, its calls welded, a thought marked as the first of its run, a
// message marked as queued — and a row folded again around the same parts is
// the same row. Looking stops at the first difference and at every value that
// is itself, so the fresh wrappers are all it walks.
export function same(a, b, depth = DEPTH) {
    return Object.is(a, b) || (depth > 0 && holds(a, b, (x, y) => same(x, y, depth - 1)));
}

// pure returns draw as a component its parent's renders pass by while every
// prop stays the same — memo of preact/compat, which this tree does not carry.
// A function prop is the same only as itself, so what a pure component is
// handed to call keeps its identity (see useSteady). Its own state and the
// contexts it reads still draw it.
export function pure(draw) {
    class Pure extends Component {
        shouldComponentUpdate(next) {
            return !holds(this.props, next, same);
        }

        render(props) {
            return h(draw, props);
        }
    }
    Pure.displayName = `pure(${draw.name || "component"})`;
    return Pure;
}

// useSteady returns a function of one identity for the life of the component
// that runs fn as the latest render made it: a handler handed to a pure
// component that would otherwise come new with every render and draw it again.
export function useSteady(fn) {
    const latest = useRef(fn);
    latest.current = fn;
    return useCallback((...args) => latest.current(...args), []);
}
