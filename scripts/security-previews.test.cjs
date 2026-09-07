const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");
const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");

function extract(name) {
  const start = source.indexOf(`function ${name}(`);
  assert.ok(start >= 0);
  const end = source.indexOf("\nfunction ", start + 1);
  return source.slice(start, end);
}

for (const name of ["buildPreview", "buildModalPreview", "buildRandomReelPreview", "renderUploadPreview"]) {
  test(`${name} renders hostile file metadata as text`, () => {
    const children = [];
    const context = vm.createContext({
      document: { createElement: () => ({ innerHTML: "" }) },
      URL: { createObjectURL: () => "blob:test" },
      uploadPreview: { appendChild: node => children.push(node) },
      clearUploadPreview() {},
    });
    vm.runInContext([extract("escapeHTML"), extract("pickIcon"), extract(name)].join("\n"), context);
    const payload = '<img src=x onerror="globalThis.compromised=true">';
    const node = name === "renderUploadPreview"
      ? (context[name]({ type: "application/octet-stream", name: payload }), children[0])
      : context[name]({ contentType: `application/octet-stream;${payload}`, originalName: payload });
    assert.ok(node.innerHTML.includes("&lt;img"), node.innerHTML);
    assert.ok(!node.innerHTML.includes("<img"), node.innerHTML);
  });
}
