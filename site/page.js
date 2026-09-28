// The page's moving parts: which stage is live, and a tile taken onto the stage.
// Scenes are cut from the panel and bring its markup along, so every lookup
// here walks the page's own frame by child steps and never searches inside one.
(() => {
    const calm = matchMedia("(prefers-reduced-motion: reduce)");
    const screenOf = (frame) => frame.querySelector(":scope > .bezel > .crop > .screen");

    // One scene moves at a time: the stage most on screen. The rest show the
    // end of their loop and cost nothing. "seen" is what the scenes' own
    // loops wait for; "still" is what stops everything else.
    const slots = [...document.querySelectorAll(".lane > .stage, .hf[data-live-slot]")];
    const seen = new Map();
    let live = null;
    const setLive = (screen) => {
        if (screen === live) return;
        if (live) { live.classList.remove("seen"); live.classList.add("still"); }
        live = screen;
        if (live) { live.classList.remove("still"); live.classList.add("seen"); }
    };
    const pick = () => {
        let best = null, most = 0.35;
        for (const [slot, ratio] of seen) if (ratio > most) { best = slot; most = ratio; }
        setLive(best ? screenOf(best) : null);
    };
    const eye = new IntersectionObserver((rows) => {
        for (const row of rows) seen.set(row.target, row.isIntersecting ? row.intersectionRatio : 0);
        pick();
    }, { threshold: [0, .2, .35, .5, .65, .8, 1] });
    slots.forEach((s) => eye.observe(s));

    // A shot's tile is a crop cut ahead; on the stage it shows the whole frame,
    // which is fetched only now: most readers take few tiles, and the frames
    // are the heaviest files of the page.
    const frame = (screen, where) => {
        const img = screen.querySelector(":scope > img");
        const spec = img && img.dataset[where];
        if (!spec) return;
        const [src, w, h] = spec.split(" ");
        if (img.getAttribute("src") === src) return;
        img.width = +w;
        img.height = +h;
        img.src = src;
    };

    // A tile goes onto the stage; what stood there goes back to its own tile.
    // The stage heads the lane of its device, so a desk takes it across.
    const take = (tile) => {
        const board = tile.closest(".board");
        const stage = board.querySelector(":scope > .lanes > .lane > .stage");
        const incoming = tile.querySelector(":scope > .crop > .screen");
        const outgoing = screenOf(stage);
        if (!incoming || !outgoing) return;
        const home = board.querySelector(`:scope > .lanes > .lane > .thumbs > .thumb[data-home="${outgoing.dataset.asset}"]`);
        if (live === outgoing) setLive(null);
        frame(outgoing, "tile");
        home.querySelector(":scope > .crop").prepend(outgoing);
        home.hidden = false;
        frame(incoming, "full");
        stage.querySelector(":scope > .bezel > .crop").prepend(incoming);
        if (!calm.matches) {
            // the scene's own loops end too, and their animationend bubbles up here
            const settle = (e) => {
                if (e.target !== incoming) return;
                incoming.classList.remove("arriving");
                incoming.removeEventListener("animationend", settle);
            };
            incoming.classList.add("arriving");
            incoming.addEventListener("animationend", settle);
        }
        tile.hidden = true;
        const dev = incoming.dataset.dev;
        const lane = board.querySelector(`:scope > .lanes > .lane[data-dev="${dev}"]`);
        if (stage.dataset.dev !== dev) {
            stage.dataset.dev = dev;
            board.dataset.stage = dev;
            lane.insertBefore(stage, lane.querySelector(":scope > .thumbs"));
        }
        const cap = stage.querySelector(":scope > figcaption");
        cap.querySelector(":scope > .cap").textContent = incoming.dataset.cap;
        cap.querySelector(":scope > .dev").innerHTML = lane.querySelector(":scope > .lanehead > svg").outerHTML;
        const top = stage.getBoundingClientRect().top;
        if (top < 0 || top > innerHeight * .6) stage.scrollIntoView({ behavior: calm.matches ? "auto" : "smooth", block: "start" });
        pick();
    };
    document.addEventListener("click", (e) => {
        const tile = e.target.closest(".thumb[data-home]");
        if (tile) take(tile);
    });
    document.addEventListener("keydown", (e) => {
        if (e.key !== "Enter" && e.key !== " ") return;
        const tile = e.target.closest && e.target.closest(".thumb[data-home]");
        if (!tile) return;
        e.preventDefault();
        take(tile);
    });

    // The install types its three lines once, when it comes on screen.
    const install = document.getElementById("install");
    if (install) {
        const once = new IntersectionObserver((rows) => {
            if (rows.some((r) => r.isIntersecting)) { install.classList.add("typing"); once.disconnect(); }
        }, { threshold: .4 });
        once.observe(install);
    }
})();
