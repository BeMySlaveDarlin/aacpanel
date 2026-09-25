// The composer paperclip with its target sheet.

import { html } from "../../html.js";
import { Sheet } from "../../ui/sheet.js";
import { knows, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { modeName } from "./picker.js";

const FILE_MAX = 32 * 1024 * 1024;

const PACK_MAX = 32 * 1024 * 1024;

export const FILES_MAX = 16;

const TARGETS = [
    { id: "camera", label: "Camera", icon: Icon.camera, accept: "image/*", capture: "environment", multiple: false },
    { id: "photo", label: "Photos", icon: Icon.photo, accept: "image/*", capture: undefined, multiple: true },
    { id: "files", label: "Files", icon: Icon.file, accept: undefined, capture: undefined, multiple: true },
];

// PickFile renders the paperclip in the composer.
export function PickFile({ exec, onAsk }) {
    const canSend = knows(exec, "session.file");
    const fileWhy = whyNot(exec, "session.file");
    return html`
        <button
            class=${`iconbtn pickfile${canSend ? "" : " off"}`}
            type="button"
            disabled=${!canSend}
            aria-label="attach files"
            title=${canSend ? "attach files" : fileWhy}
            onClick=${onAsk}
        >${Icon.clip()}</button>
    `;
}

// AttachSheet renders the sheet with the attachment targets.
export function AttachSheet({ open, onClose, exec, files, onFiles, live, onMode }) {
    const toast = useToast();

    const canSend = knows(exec, "session.file");
    const have = files || [];

    const take = async (event) => {
        const picked = Array.from(event.target.files || []);
        event.target.value = "";
        if (!picked.length) return;
        onClose();

        const got = await intake(picked, have, toast);
        if (got) onFiles(got);
    };

    if (!canSend) return null;

    return html`
        <${Sheet} open=${open} onClose=${onClose} label="attach" inner>
            <div class="shead"><span class="stitle">Attach</span></div>
            <div class="addctx">
                ${TARGETS.map((target) => html`
                    <label class="addctxtile" key=${target.id}>
                        <input
                            type="file"
                            multiple=${target.multiple}
                            accept=${target.accept}
                            capture=${target.capture}
                            onChange=${take}
                        />
                        <span class="addctxicon"><${target.icon} /></span>
                        <span class="addctxname">${target.label}</span>
                    </label>
                `)}
            </div>
            ${onMode && live && html`
                <div class="pklist addmode">
                    <button type="button" class="pkrow pkmore" onClick=${onMode}>
                        <span class="pkround">${Icon.bolt()}</span>
                        <span class="pkbody">
                            <span class="pkname">Permission</span>
                            <span class="pkdesc">${modeName(live.mode)}</span>
                        </span>
                        <span class="crgo">${Icon.chevron()}</span>
                    </button>
                </div>
            `}
        <//>
    `;
}

// intake returns the picked files ready for the protocol, or null if they cannot be taken.
export async function intake(picked, have, toast, rename) {
    if (have.length + picked.length > FILES_MAX) {
        toast("Too many files",
            `${have.length + picked.length} against a ceiling of ${FILES_MAX} at a time`, true);
        return null;
    }
    const big = picked.find((f) => f.size > FILE_MAX);
    if (big) {
        toast("The file is too large", `${big.name} — ${mb(big.size)} against a ceiling of ${mb(FILE_MAX)}`, true);
        return null;
    }
    const total = have.reduce((sum, f) => sum + f.size, 0) + picked.reduce((sum, f) => sum + f.size, 0);
    if (total > PACK_MAX) {
        toast("They will not fit together", `${mb(total)} against a ceiling of ${mb(PACK_MAX)} per message`, true);
        return null;
    }

    try {
        return await Promise.all(picked.map((f, i) => read(f, rename && rename(f, i))));
    } catch (err) {
        toast("The file was not read", `${err.message}: the browser did not give up the contents`, true);
        return null;
    }
}

// clipName gives a name to a picture pasted from the clipboard.
export function clipName(file, at) {
    const own = String((file && file.name) || "");
    if (own && !/^image\.[a-z0-9]+$/i.test(own)) return undefined;
    const ext = own.includes(".") ? own.split(".").pop() : String((file && file.type) || "").split("/").pop();
    const d = at instanceof Date ? at : new Date();
    const two = (n) => String(n).padStart(2, "0");
    const stamp = `${d.getFullYear()}${two(d.getMonth() + 1)}${two(d.getDate())}`
        + `-${two(d.getHours())}${two(d.getMinutes())}${two(d.getSeconds())}`
        + `-${String(d.getMilliseconds()).padStart(3, "0")}`;
    return ext ? `paste-${stamp}.${ext}` : `paste-${stamp}`;
}

function read(file, name) {
    return new Promise((ok, no) => {
        const reader = new FileReader();
        reader.onerror = () => no(new Error(file.name));
        reader.onload = () => {
            const at = String(reader.result).indexOf(",");
            if (at < 0) {
                no(new Error(file.name));
                return;
            }
            ok({ name: tidy(name || file.name), size: file.size, data: String(reader.result).slice(at + 1) });
        };
        reader.readAsDataURL(file);
    });
}

// tidy trims a file name down to what the protocol accepts.
export function tidy(name) {
    const base = String(name || "").split(/[\\/]/).pop().slice(-96);
    const safe = base
        .replace(/[^\p{L}\p{N}._-]/gu, "_")
        .replace(/\.{2,}/g, ".")
        .replace(/^[.-]+/, "");
    return safe || "file";
}

// mb renders a size in human figures for the refusal toast.
export function mb(bytes) {
    if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${Math.round(bytes / 1024)} KB`;
}
