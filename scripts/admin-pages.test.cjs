// Run with: node --test scripts/admin-pages.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const appSource = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");
const pagesSource = readFileSync(resolve(__dirname, "../static/admin-pages.js"), "utf8");
const context = vm.createContext({});
// Slice the shared helpers out of app.js, as admin-dashboard.test.cjs does.
const escapeStart = appSource.indexOf("function escapeHTML(");
vm.runInContext(appSource.slice(escapeStart, appSource.indexOf("\nfunction ", escapeStart + 1)), context);
const sizeStart = appSource.indexOf("function formatSize(");
vm.runInContext(appSource.slice(sizeStart, appSource.indexOf("\n}", sizeStart) + 2), context);
const relStart = appSource.indexOf("function formatRelativeTime(");
vm.runInContext(appSource.slice(relStart, appSource.indexOf("\n}", relStart) + 2), context);
vm.runInContext("function formatDateTime(d) { return new Date(d).toISOString(); }", context);
vm.runInContext(pagesSource, context);

test("admin pages script parses", () => {
  assert.doesNotThrow(() => new vm.Script(pagesSource));
});

test("storage dirs sort by size with other last", () => {
  const html = context.buildStorageDirs([
    { name: "other", path: "/d/other", bytes: 9000, files: 1 },
    { name: "thumbs", path: "/d/thumbs", bytes: 100, files: 2 },
    { name: "uploads", path: "/d/uploads", bytes: 5000, files: 3 },
  ], null);
  const order = ["uploads", "thumbs", "other"].map((name) => html.indexOf(`>${name}<`));
  assert.ok(order.every((i) => i >= 0), "all dir names rendered");
  assert.ok(order[0] < order[1] && order[1] < order[2]);
});

test("orphan footer totals selected bytes", () => {
  const scan = { orphans: [
    { path: "uploads/a.png", dir: "uploads", bytes: 1024, modified_at: "2026-01-01T00:00:00Z" },
    { path: "uploads/b.png", dir: "uploads", bytes: 2048, modified_at: "2026-01-01T00:00:00Z" },
  ], missing: [] };
  const html = context.buildStorageOrphans(scan, new Set(["uploads/a.png", "uploads/b.png"]));
  assert.ok(html.includes(context.formatSize(3072)));
});

test("orphan list escapes file names", () => {
  const scan = { orphans: [{ path: "uploads/<img onerror=x>.png", dir: "uploads", bytes: 1, modified_at: "" }],
    missing: [{ kind: "meme", id: "1", title: "<b>x</b>", path: "<i>p</i>" }] };
  const html = context.buildStorageOrphans(scan, new Set());
  assert.ok(!html.includes("<img"));
  assert.ok(!html.includes("<b>x"));
  assert.ok(!html.includes("<i>p"));
});

test("upload rows expose a discard button with the id", () => {
  const html = context.buildStorageUploads([{ id: "u1", title: "Book", display_name: "Ann", received_bytes: 5, total_bytes: 10, updated_at: "2026-01-01T00:00:00Z" }]);
  assert.match(html, /data-discard-upload="u1"/);
});

test("index.html has one admin-pages script tag and one storage panel", () => {
  const html = readFileSync(resolve(__dirname, "../static/index.html"), "utf8");
  assert.equal((html.match(/<script[^>]+admin-pages.js/g) || []).length, 1);
  assert.equal((html.match(/id="admin-storage-panel"/g) || []).length, 1);
});
