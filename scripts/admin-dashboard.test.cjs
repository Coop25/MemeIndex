// Run with: node --test scripts/admin-dashboard.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");
const context = vm.createContext({});
// Exercise the actual browser helpers without bootstrapping the authenticated app.
const escapeStart = source.indexOf("function escapeHTML(");
vm.runInContext(source.slice(escapeStart, source.indexOf("\nfunction ", escapeStart + 1)), context);
const chartStart = source.indexOf("function adminChartTooltipAttributes(");
vm.runInContext(source.slice(chartStart, source.indexOf("function renderAdminDashboard(", chartStart)), context);

test("application JavaScript parses", () => {
  assert.doesNotThrow(() => new vm.Script(source));
});

test("daily hover targets cover the full graph and align with each point", () => {
  const points = Array.from({ length: 30 }, (_, index) => ({ date: `2026-08-${String(index + 1).padStart(2, "0")}`, uploads: index }));
  const html = context.buildAdminUploadChart(points);
  const targets = [...html.matchAll(/class="admin-chart-hit" x="([\d.]+)" y="0" width="([\d.]+)"/g)].map((match) => ({ x: +match[1], width: +match[2] }));
  assert.equal(targets.length, 30);
  assert.equal(targets[0].x, 0);
  assert.equal(targets[29].x + targets[29].width, 720);
  targets.forEach((target, index) => {
    const pointX = index / 29 * 720;
    assert.ok(target.x <= pointX + .01 && target.x + target.width >= pointX - .01);
    if (index) assert.ok(Math.abs(targets[index - 1].x + targets[index - 1].width - target.x) <= .02);
  });
  assert.match(html, /2026-08-01 \(UTC day\)\nUploads: 0/);
  assert.match(html, /2026-08-30 \(UTC day\)\nUploads: 29/);
  assert.equal((html.match(/tabindex="0"/g) || []).length, 30);
});

test("empty, flat, and single-day series stay valid and expose exact values", () => {
  assert.match(context.buildAdminUploadChart([]), /Upload history will appear here/);
  assert.equal(context.buildAdminMetricSparkline([], "memes", "Memes"), "");
  const point = { date: "2026-08-27", uploads: 0, storage_bytes: 1234567, users: 10 };
  assert.match(context.buildAdminUploadChart([point]), /cx="360.00"/);
  const flat = context.buildAdminMetricSparkline([point, point], "users", "Users");
  assert.doesNotMatch(flat, /NaN|Infinity/);
  assert.equal((flat.match(/class="admin-chart-dot"/g) || []).length, 2);
  const storage = context.buildAdminMetricSparkline([point], "storage_bytes", "Storage", (value) => `${value} bytes`);
  assert.match(storage, /Storage: 1234567 bytes/);
});

test("tag sections and legend expose counts, including the unlisted remainder", () => {
  const html = context.buildAdminTopTags([{ tag: "music", count: 49 }, { tag: "song", count: 37 }], 2271);
  assert.match(html, /music\n49 tag assignments \(2.2%\)/);
  assert.match(html, /Other tags\n2,185 tag assignments/);
  assert.match(html, /<strong>2,271<\/strong><span>Assignments/);
  assert.equal((html.match(/class="admin-tag-slice"/g) || []).length, 3);
  assert.match(html, /data-admin-tag="music"/);
});

test("empty and full-circle tag distributions render without phantom sections", () => {
  assert.match(context.buildAdminTopTags([], 0), /No tags have been used yet/);
  const html = context.buildAdminTopTags([{ tag: "only", count: 7 }], 7);
  assert.equal((html.match(/class="admin-tag-slice"/g) || []).length, 1);
  assert.doesNotMatch(html, /Other tags|NaN|Infinity/);
  assert.match(html, /7 tag assignments \(100.0%\)/);
  assert.equal((html.match(/A65,65/g) || []).length, 2);
});

test("tag names are escaped in both tooltip attributes and legend text", () => {
  const html = context.buildAdminTopTags([{ tag: '\"><img src=x onerror=alert(1)>', count: 1 }], 1);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&quot;&gt;&lt;img/);
});
