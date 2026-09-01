// Run with: node --test scripts/share-links.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");
const functionStart = source.indexOf("async function copyShareText(");
const functionEnd = source.indexOf("\nfunction showShareCopiedFeedback(", functionStart);
const copyShareTextSource = source.slice(functionStart, functionEnd);

function clipboardContext(writeText) {
  const events = [];
  const input = {
    style: {},
    value: "",
    setAttribute() {},
    focus() { events.push("focus"); },
    select() { events.push("select"); },
    setSelectionRange(start, end) { events.push(`range:${start}-${end}`); },
    remove() { events.push("remove"); },
  };
  const context = vm.createContext({
    console: { warn() {} },
    navigator: { clipboard: writeText ? { writeText } : undefined },
    document: {
      body: { appendChild() { events.push("append"); } },
      createElement() { return input; },
      execCommand(command) { events.push(command); return true; },
    },
  });
  vm.runInContext(copyShareTextSource, context);
  return { context, events, input };
}

test("application JavaScript parses", () => {
  assert.doesNotThrow(() => new vm.Script(source));
});

test("share copying uses the Clipboard API when it succeeds", async () => {
  const writes = [];
  const { context, events } = clipboardContext(async (value) => writes.push(value));
  await context.copyShareText("  https://memes.example/m/123?share=token  ");
  assert.deepEqual(writes, ["https://memes.example/m/123?share=token"]);
  assert.deepEqual(events, []);
});

test("share copying falls back when a browser rejects the Clipboard API", async () => {
  const { context, events, input } = clipboardContext(async () => {
    throw new Error("clipboard permission denied");
  });
  await context.copyShareText("https://memes.example/m/123?share=token");
  assert.equal(input.value, "https://memes.example/m/123?share=token");
  assert.deepEqual(events, ["append", "focus", "select", `range:0-${input.value.length}`, "copy", "remove"]);
});

test("share copying rejects an empty URL", async () => {
  const { context, events } = clipboardContext();
  await assert.rejects(() => context.copyShareText("  "), /share URL missing/);
  assert.deepEqual(events, []);
});
