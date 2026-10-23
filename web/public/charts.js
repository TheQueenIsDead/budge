// Chart colours, shared by every canvas in the app.
//
// Chart.js needs literal colours while the interface is themed with CSS custom
// properties, so the two have to agree. Rather than repeat the hexes in each
// template, they are read from the stylesheet: the tokens in styles.css stay the
// single source, and a palette change does not need chasing through templates.
//
// Lookups are lazy and memoised. Resolving at load would depend on this script
// being parsed after the stylesheet link, which is a silent failure if the head
// is ever reordered: getPropertyValue returns an empty string, every accent
// falls back to the same colour, and a stacked chart renders as one flat block.
(function () {
    // Token names are explicit: "muted" is --b-muted-accent, not --b-muted,
    // which is the colour body text falls back to.
    const ACCENT_TOKENS = {
        sage: "--b-sage",
        clay: "--b-clay",
        plum: "--b-plum",
        slate: "--b-slate",
        brick: "--b-brick",
        muted: "--b-muted-accent"
    };

    const FALLBACK = "#3d6b8f";
    const cache = {};

    function token(name, fallback) {
        if (cache[name] === undefined) {
            const value = getComputedStyle(document.documentElement)
                .getPropertyValue(name)
                .trim();
            // Only cache a real answer, so a call made before the stylesheet is
            // ready does not poison every later lookup.
            if (!value) {
                return fallback;
            }
            cache[name] = value;
        }
        return cache[name];
    }

    const budge = {
        get ink() {
            return token("--b-muted", "#78858f");
        },
        get grid() {
            return token("--b-surface-3", "#e6ecf0");
        },
        get surface() {
            return token("--b-surface", "#ffffff");
        },
        get primary() {
            return token("--b-primary", FALLBACK);
        },
        get primaryInk() {
            return token("--b-primary-ink", "#315774");
        }
    };

    // colour resolves an accent by token name, falling back to the primary so an
    // unrecognised name renders as a colour rather than as nothing.
    budge.colour = function (name) {
        return token(ACCENT_TOKENS[name] || "--b-primary", FALLBACK);
    };

    budge.colourSoft = function (name) {
        return token((ACCENT_TOKENS[name] || "--b-primary") + "-soft", "#e6eef4");
    };

    // mix blends an accent towards white. The soft tokens are tuned to sit
    // behind text and are too pale to read as a chart bar, so a chart wanting a
    // lighter shade asks for one rather than hard coding it.
    budge.mix = function (name, amount) {
        const hex = budge.colour(name).replace("#", "");
        if (hex.length !== 6) {
            return budge.colour(name);
        }
        const channel = function (offset) {
            const value = parseInt(hex.substring(offset, offset + 2), 16);
            return Math.round(value + (255 - value) * (1 - amount));
        };
        return "rgb(" + channel(0) + "," + channel(2) + "," + channel(4) + ")";
    };

    // series is an ordered palette for charts that colour by an arbitrary
    // category rather than by a tracked spending group. The first five are the
    // tracked accents, so a category that happens to be one of them keeps its
    // familiar colour; the rest extend the set without repeating a hue.
    Object.defineProperty(budge, "series", {
        get: function () {
            return [
                budge.colour("sage"),
                budge.colour("clay"),
                budge.colour("plum"),
                budge.colour("slate"),
                budge.colour("brick"),
                "#5f8a8a",
                "#8a7b9e",
                "#9e8a5f",
                "#6d8a4f",
                "#a86b7b"
            ];
        }
    });

    window.budge = budge;
})();
