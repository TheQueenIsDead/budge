// Copies the library files budge's pages load out of node_modules into
// public/vendor, where Go embeds them with budge's own static files. pnpm runs
// it after every install; the copies are not committed, so a Renovate update
// to package.json and the lockfile is the whole change.
//
// A stylesheet brings along the files it loads with url(), such as fonts, at
// the same relative path. A file already up to date is left alone, so Air,
// which rebuilds when a file under web changes, does not rebuild forever.

import { mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "node_modules");
const vendor = join(here, "public", "vendor");

// Where each file is served under /static/vendor, and where it comes from.
const files = {
  "bootstrap/bootstrap.min.css": "bootstrap/dist/css/bootstrap.min.css",
  "bootstrap/bootstrap.bundle.min.js": "bootstrap/dist/js/bootstrap.bundle.min.js",
  "bootstrap-icons/bootstrap-icons.min.css": "bootstrap-icons/font/bootstrap-icons.min.css",
  "inter/inter.css": "@fontsource-variable/inter/wght.css",
  "chart.js/chart.umd.min.js": "chart.js/dist/chart.umd.min.js",
  "htmx/htmx.min.js": "htmx.org/dist/htmx.min.js",
  "alpinejs/alpine.min.js": "alpinejs/dist/cdn.min.js",
  "alpinejs/morph.min.js": "@alpinejs/morph/dist/cdn.min.js",
};

const wanted = new Map();
for (const [to, from] of Object.entries(files)) {
  wanted.set(to, join(modules, from));
  if (from.endsWith(".css")) {
    const css = readFileSync(join(modules, from), "utf8");
    for (const [, url] of css.matchAll(/url\(\s*["']?([^"')]+)["']?\s*\)/g)) {
      if (/^(data:|[a-z]+:\/\/|\/)/.test(url)) continue;
      const path = url.replace(/[?#].*$/, "");
      wanted.set(join(dirname(to), path), join(modules, dirname(from), path));
    }
  }
}

let changed = 0;
for (const [to, from] of wanted) {
  const target = join(vendor, to);
  const content = readFileSync(from);
  let current;
  try {
    current = readFileSync(target);
  } catch {
    // not copied yet
  }
  if (current && current.equals(content)) continue;
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, content);
  changed++;
}

// Drop what is no longer wanted, so a removed library stops being served.
const walk = (dir) =>
  readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
let removed = 0;
for (const path of walk(vendor)) {
  if (!wanted.has(relative(vendor, path))) {
    rmSync(path);
    removed++;
  }
}

console.log(`public/vendor: ${wanted.size} files, ${changed} updated, ${removed} removed`);
