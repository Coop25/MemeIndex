// Run with: node --test scripts/app-scripts.test.cjs
// The app is split into area scripts under static/js/ plus app.js, all
// classic scripts sharing one global scope. index.html loads the area scripts
// first, so they must only declare things: if one ran code at load, it could
// reach state app.js has not set up yet.
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { basename } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");
const { appScriptPaths, readAppSource, extractFunction, extractDeclaration } = require("./app-source.cjs");

const paths = appScriptPaths();
const areaPaths = paths.filter((path) => basename(path) !== "app.js");

test("index.html loads every area script before app.js", () => {
  assert.equal(basename(paths[paths.length - 1]), "app.js");
  assert.ok(areaPaths.length > 0);
});

test("each app script parses on its own", () => {
  for (const path of paths) {
    assert.doesNotThrow(() => new vm.Script(readFileSync(path, "utf8"), { filename: path }), path);
  }
});

test("area scripts run no code at load: they read nothing but a few built-ins", () => {
  // The global the scripts see holds only the built-ins a plain starting value
  // may use. Any other name they read while loading - document, window, state
  // from app.js, even a guarded `typeof window` - is recorded and fails the
  // test. Function bodies are not run, so what they use does not count.
  const builtins = ["Map", "Set", "WeakMap", "WeakSet", "Object", "Symbol", "Math", "Number", "String", "Array", "JSON", "Boolean", "Infinity", "NaN", "undefined"];
  const target = Object.fromEntries(builtins.map((name) => [name, globalThis[name]]));
  const reads = [];
  const note = (key, target_) => {
    if (typeof key === "string" && !(key in target_)) reads.push(key);
  };
  const global = new Proxy(target, {
    get(target_, key, receiver) { note(key, target_); return Reflect.get(target_, key, receiver); },
    has(target_, key) { note(key, target_); return Reflect.has(target_, key); },
  });
  const context = vm.createContext(global);
  for (const path of areaPaths) {
    reads.length = 0;
    assert.doesNotThrow(() => new vm.Script(readFileSync(path, "utf8"), { filename: path }).runInContext(context), path);
    assert.deepEqual([...new Set(reads)], [], `${basename(path)} reads these at load`);
  }
});

test("app.js declares no functions: they belong in an area script", () => {
  const appJs = paths.find((path) => basename(path) === "app.js");
  const functions = [...readFileSync(appJs, "utf8").matchAll(/^(?:async )?function ([\w$]+)/gm)].map((m) => m[1]);
  assert.deepEqual(functions, [], "move these into the static/js/ file for their area");
});

test("each top-level declaration names one plain identifier", () => {
  // Keeps the duplicate check below complete: it reads one name per line.
  for (const path of paths) {
    const source = readFileSync(path, "utf8");
    // Destructuring, or a name list like `let a, b` or `let a = 1, b = 2;`.
    // Only a simple value may sit before the comma, so commas inside a call
    // or an array (`Object.freeze(["a", "b"])`) do not count.
    const simpleValue = String.raw`(?:-?[\w$.]+|"[^"\n]*"|'[^'\n]*'|` + "`[^`\\n]*`" + String.raw`|\[\]|\{\})`;
    const multi = source.match(new RegExp(
      String.raw`^(?:let|const|var)\s+(?:[[{]|[\w$]+(?:\s*=\s*` + simpleValue + String.raw`)?[ \t]*,[ \t]*(?:[\w$]+\s*(?:=|,|;)|$))`,
      "m",
    ));
    assert.equal(multi, null, `${basename(path)}: declare one name per statement: ${multi?.[0]}`);
  }
});

test("the test helpers extract every top-level function and declaration whole", () => {
  // Too short and it will not parse; too long and it takes in the next
  // top-level declaration, which would quietly load code a test never asked for.
  const nextDeclaration = /\n(?:async function|function|const|let|var|class) /;
  const source = readAppSource();
  for (const [, name] of source.matchAll(/^(?:async )?function ([\w$]+)\(/gm)) {
    const text = extractFunction(source, name);
    assert.doesNotThrow(() => new vm.Script(text), `extractFunction(${name}) returned a partial function`);
    assert.doesNotMatch(text, nextDeclaration, `extractFunction(${name}) ran into the next declaration`);
  }
  for (const [, name] of source.matchAll(/^(?:const|let) ([\w$]+)/gm)) {
    const text = extractDeclaration(source, name);
    assert.doesNotThrow(() => new vm.Script(text), `extractDeclaration(${name}) returned a partial declaration`);
    assert.doesNotMatch(text, nextDeclaration, `extractDeclaration(${name}) ran into the next declaration`);
  }
});

test("no top-level name is declared in two scripts", () => {
  const seen = new Map();
  for (const path of paths) {
    const source = readFileSync(path, "utf8");
    for (const match of source.matchAll(/^(?:async function|function|const|let|var|class) ([A-Za-z_$][\w$]*)/gm)) {
      const name = match[1];
      assert.ok(!seen.has(name), `${name} is declared in ${seen.get(name)} and ${basename(path)}`);
      seen.set(name, basename(path));
    }
  }
});
