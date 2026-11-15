const assert = require("node:assert/strict");
const { test } = require("node:test");
const vm = require("node:vm");
const { readAppSource, extractFunction } = require("./app-source.cjs");
const source = readAppSource();

const extract = (name) => extractFunction(source, name);

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
