// Mod boundary: herdr-app loads a mod's modules only from inside its own folder,
// and agent-usage-tabs must share agent-usage's single model.js through a link.
// Run with: node --experimental-vm-modules --test 'app/**/*.test.js'
import { test } from "node:test";
import assert from "node:assert/strict";
import { lstatSync, readdirSync, readFileSync } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const modsDir = fileURLToPath(new URL("./", import.meta.url));
// Matches `from "./x"`, `import "./x"` and `import("./x")` with a relative specifier.
const RELATIVE_IMPORT = /(?:\bfrom|\bimport)\s*\(?\s*["'](\.{1,2}\/[^"'\n]*)["']/g;

function modNames() {
  return readdirSync(modsDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();
}

// Non-test .js files under dir. Symlinked directories are not followed.
function moduleFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...moduleFiles(path));
    } else if (entry.name.endsWith(".js") && !entry.name.endsWith(".test.js")) {
      out.push(path);
    }
  }
  return out;
}

function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

function relativeImports(source) {
  return [...stripComments(source).matchAll(RELATIVE_IMPORT)].map((match) => match[1]);
}

// Lexical resolution, as the loader sees it: the specifier is resolved from the
// importing file's folder and must stay inside the mod folder. Symlinks are not followed.
function escapesMod(modPath, fromDir, specifier) {
  const rel = relative(modPath, resolve(fromDir, specifier));
  return rel === ".." || rel.startsWith(`..${sep}`) || isAbsolute(rel);
}

test("every relative import in a mod stays inside that mod's folder", () => {
  const mods = modNames();
  assert.ok(mods.length > 0, `no mods found under ${modsDir}`);
  const violations = [];
  for (const mod of mods) {
    const modPath = join(modsDir, mod);
    for (const file of moduleFiles(modPath)) {
      const source = readFileSync(file, "utf8");
      for (const specifier of relativeImports(source)) {
        if (escapesMod(modPath, dirname(file), specifier)) {
          violations.push(`${mod}/${relative(modPath, file)}: import "${specifier}" resolves outside ${mod}/`);
        }
      }
    }
  }
  assert.deepEqual(violations, [], `mod imports escape their own folder:\n${violations.join("\n")}`);
});

test("the boundary check flags a sibling-mod import and accepts a folder-local one", () => {
  const modPath = join(modsDir, "agent-usage-tabs");
  const source = 'import { openPanes } from "../agent-usage/model.js";';
  assert.deepEqual(relativeImports(source), ["../agent-usage/model.js"]);
  assert.equal(escapesMod(modPath, modPath, "../agent-usage/model.js"), true);
  assert.equal(escapesMod(modPath, modPath, "./model.js"), false);
});

test("agent-usage-tabs/model.js is a symlink to the single agent-usage/model.js", () => {
  const link = join(modsDir, "agent-usage-tabs", "model.js");
  const real = join(modsDir, "agent-usage", "model.js");
  assert.ok(lstatSync(link).isSymbolicLink(), `${link} must be a symbolic link to ${real}`);
  assert.equal(
    readFileSync(link, "utf8"),
    readFileSync(real, "utf8"),
    `${link} must have the same content as ${real}`,
  );
});
