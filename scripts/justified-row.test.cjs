// Run with: node --test scripts/justified-row.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");

const context = vm.createContext({});
const start = source.indexOf("function pickJustifiedRow(");
const end = source.indexOf("const DASHBOARD_RECENT_MOBILE_COUNT", start);
assert.notEqual(start, -1);
assert.notEqual(end, -1);
vm.runInContext(source.slice(start, end), context);

const rowWidth = (ratios, row, gap) =>
  ratios.slice(0, row.count).reduce((sum, ratio) => sum + ratio * row.height, 0) + gap * (row.count - 1);

test("a row with plenty of items spans the width exactly, near the target height", () => {
  const ratios = Array.from({ length: 24 }, (_, index) => [0.5625, 1, 1.78, 0.75][index % 4]);
  for (const width of [700, 1075, 1750, 2560, 3600]) {
    const row = context.pickJustifiedRow(ratios, width, 14, 250, 375);
    assert.ok(row.count > 0 && row.count < ratios.length, `count at ${width}`);
    assert.ok(Math.abs(rowWidth(ratios, row, 14) - width) < 0.001, `fills ${width}`);
    assert.ok(row.height > 250 * 0.7 && row.height <= 375, `height ${row.height} at ${width}`);
  }
});

test("wider windows show more items", () => {
  const ratios = Array.from({ length: 24 }, () => 0.5625);
  const narrow = context.pickJustifiedRow(ratios, 1000, 14, 250, 375);
  const wide = context.pickJustifiedRow(ratios, 3000, 14, 250, 375);
  assert.ok(wide.count > narrow.count);
});

test("too few items show them all and stop growing at the maximum height", () => {
  const row = context.pickJustifiedRow([0.5625, 0.5625], 2000, 14, 250, 375);
  assert.equal(row.count, 2);
  assert.equal(row.height, 375);
});

test("an empty row is safe", () => {
  const row = context.pickJustifiedRow([], 1000, 14, 250, 375);
  assert.equal(row.count, 0);
  assert.ok(Number.isFinite(row.height));
});

test("a single very wide item still fits the row", () => {
  const row = context.pickJustifiedRow([2.2, 1], 400, 14, 250, 375);
  assert.equal(row.count, 1);
  assert.ok(Math.abs(row.height * 2.2 - 400) < 0.001);
});
