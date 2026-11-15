// Shared helpers for the frontend tests: the app's script source, and pulling
// one top-level function out of it to run in a vm context.
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");

const staticDir = resolve(__dirname, "../static");

// appScriptPaths lists the app's scripts in the order index.html loads them.
function appScriptPaths() {
  const html = readFileSync(resolve(staticDir, "index.html"), "utf8");
  const paths = [...html.matchAll(/<script src="\/static\/([^"?]+\.js)"/g)]
    .map((match) => match[1])
    .filter((path) => path !== "admin-pages.js");
  if (paths.length === 0) throw new Error("index.html loads no app scripts");
  return paths.map((path) => resolve(staticDir, path));
}

// readAppSource returns every app script joined in load order, so a test can
// search all of the app's code as one text.
function readAppSource() {
  return appScriptPaths().map((path) => readFileSync(path, "utf8")).join("\n");
}

// escapeRegExp makes a name safe to put in a RegExp (names may contain `$`).
const escapeRegExp = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

// extractFunction returns the source of one top-level function declaration,
// from its `function name(` line to the first line after it that is exactly
// `}`: top-level functions close there and their bodies are indented. (A line
// that only starts with `}`, such as `}) {` closing destructured parameters,
// is not the end.)
function extractFunction(source, name) {
  const match = new RegExp(`^(async )?function ${escapeRegExp(name)}\\(`, "m").exec(source);
  if (!match) throw new Error(`function ${name} not found`);
  const closing = /\n\}(?=\r?\n|$)/g;
  closing.lastIndex = match.index;
  const end = closing.exec(source);
  if (!end) throw new Error(`function ${name} has no closing brace`);
  return source.slice(match.index, end.index + 2);
}

// extractDeclaration returns one top-level `const` or `let` declaration: a
// single line ending in `;` (optionally followed by a `// comment`), such as
// `let audiobookPlayPending = null;`, or a multi-line one running to the first
// unindented line that ends in `;`, such as the `]);` closing
// `const ROWS = Object.freeze([ ... ]);`.
function extractDeclaration(source, name) {
  const match = new RegExp(`^(?:const|let) ${escapeRegExp(name)}(?![\\w$])`, "m").exec(source);
  if (!match) throw new Error(`declaration of ${name} not found`);
  const firstLineEnd = source.indexOf("\n", match.index);
  const firstLine = source.slice(match.index, firstLineEnd === -1 ? source.length : firstLineEnd).trimEnd();
  if (/;\s*(?:\/\/.*)?$/.test(firstLine)) return firstLine;
  const closing = /\n[^\s][^\n]*;[ \t]*(?:\/\/[^\n]*)?(?=\r?\n|$)/g;
  closing.lastIndex = match.index;
  const end = closing.exec(source);
  if (!end) throw new Error(`declaration of ${name} has no closing line`);
  return source.slice(match.index, end.index + end[0].length);
}

// extractAll joins the named top-level functions and single-line declarations
// in the order given, ready to run in a vm context.
function extractAll(source, names) {
  return names.map((name) => {
    try {
      return extractFunction(source, name);
    } catch {
      return extractDeclaration(source, name);
    }
  }).join("\n");
}

module.exports = { appScriptPaths, readAppSource, extractFunction, extractDeclaration, extractAll };
