// Chart colours, shared by every canvas in the app.
//
// Chart.js needs literal colours while the interface is themed with CSS custom
// properties, so the two have to agree. Every colour is read from the tokens in
// colours.css, the only place a colour is written down; nothing here or in a
// template restates one.
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

    const cache = {};

    function token(name) {
        if (cache[name] === undefined) {
            const value = getComputedStyle(document.documentElement)
                .getPropertyValue(name)
                .trim();
            // Only cache a real answer, so a call made before the stylesheet is
            // ready does not poison every later lookup. An empty colour leaves
            // Chart.js to its default rather than drawing nothing.
            if (!value) {
                return value;
            }
            cache[name] = value;
        }
        return cache[name];
    }

    const budge = {
        get ink() {
            return token("--b-muted");
        },
        get grid() {
            return token("--b-surface-3");
        },
        get surface() {
            return token("--b-surface");
        },
        get primary() {
            return token("--b-primary");
        },
        get primaryInk() {
            return token("--b-primary-ink");
        },
        get primarySoft() {
            return token("--b-primary-soft");
        }
    };

    // colour resolves an accent by token name, falling back to the primary so an
    // unrecognised name renders as a colour rather than as nothing.
    budge.colour = function (name) {
        return token(ACCENT_TOKENS[name] || "--b-primary");
    };

    budge.colourSoft = function (name) {
        return token((ACCENT_TOKENS[name] || "--b-primary") + "-soft");
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
                token("--b-series-6"),
                token("--b-series-7"),
                token("--b-series-8"),
                token("--b-series-9"),
                token("--b-series-10")
            ];
        }
    });

    window.budge = budge;
})();
