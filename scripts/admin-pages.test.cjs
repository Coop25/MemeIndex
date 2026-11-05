// Run with: node --test scripts/admin-pages.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const appSource = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");
const pagesSource = readFileSync(resolve(__dirname, "../static/admin-pages.js"), "utf8");
const context = vm.createContext({ URLSearchParams });
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

/* ---------- Audiobook Library ---------- */

const libraryRow = (over) => ({
  id: "b1", title: "Dune", author: "Frank Herbert", format: "M4B", size_bytes: 2048,
  duration_seconds: 7200, uploader_name: "Ann", created_at: "2026-01-01T00:00:00Z",
  has_cover: true, has_chapters: true, has_series: true, active_listeners: 2, seconds_listened: 3600, ...over,
});

test("library header marks the active sort", () => {
  const html = context.buildAudiobookLibraryTable([libraryRow()], { sort: "size", dir: "desc" });
  assert.match(html, /<th[^>]*aria-sort="descending"[^>]*>\s*<button[^>]*data-library-sort="size"/);
  assert.match(html, /<th[^>]*aria-sort="none"[^>]*>\s*<button[^>]*data-library-sort="title"/);
  const asc = context.buildAudiobookLibraryTable([libraryRow()], { sort: "title", dir: "asc" });
  assert.match(asc, /aria-sort="ascending"[^>]*>\s*<button[^>]*data-library-sort="title"/);
});

test("library rows show metadata gaps", () => {
  const html = context.buildAudiobookLibraryTable([libraryRow({ has_cover: false })], { sort: "created_at", dir: "desc" });
  assert.match(html, /<td[^>]*data-col="cover"[^>]*>[^<]*<span class="admin-library-gap"[^>]*>—<\/span>/);
  assert.match(html, /<td[^>]*data-col="chapters"[^>]*>[^<]*<span class="admin-library-ok"[^>]*>✓<\/span>/);
  assert.match(html, /data-admin-audiobook-id="b1"/);
});

test("library escapes titles", () => {
  const html = context.buildAudiobookLibraryTable([libraryRow({ id: '"><x', title: "<img src=x onerror=y>", author: "<b>a</b>", uploader_name: "<i>u</i>" })], { sort: "title", dir: "asc" });
  assert.ok(!html.includes("<img"));
  assert.ok(!html.includes("<b>a"));
  assert.ok(!html.includes("<i>u"));
  assert.ok(!html.includes('"><x'));
});

test("library empty list and pager label", () => {
  assert.match(context.buildAudiobookLibraryTable([], { sort: "title", dir: "asc" }), /No audiobooks match/);
  assert.equal(context.audiobookLibraryPageInfo({ page: 2, page_size: 50, total: 120 }).label, "Page 2 of 3");
  assert.equal(context.audiobookLibraryPageInfo({ page: 3, page_size: 50, total: 120 }).hasNext, false);
  assert.equal(context.audiobookLibraryPageInfo({ page: 1, page_size: 50, total: 0 }).label, "Page 1 of 1");
});

test("library query string carries sort, filter and paging", () => {
  const qs = context.audiobookLibraryQuery({ sort: "size", dir: "desc", filter: "no-cover", page: 2 });
  assert.match(qs, /sort=size/);
  assert.match(qs, /dir=desc/);
  assert.match(qs, /filter=no-cover/);
  assert.match(qs, /page=2/);
  assert.ok(!context.audiobookLibraryQuery({ sort: "size", dir: "desc", filter: "", page: 1 }).includes("filter="));
});

test("index.html has one library panel", () => {
  const html = readFileSync(resolve(__dirname, "../static/index.html"), "utf8");
  assert.equal((html.match(/id="admin-audiobook-library-panel"/g) || []).length, 1);
  assert.equal((html.match(/id="admin-library-filter"/g) || []).length, 1);
});

test("dashboard preset resets page and data but keeps sort", () => {
  const next = context.adminLibraryPresetState({ filter: "", sort: "size", dir: "asc", page: 3, data: { items: [1] }, busy: true, status: "x" }, "no-cover");
  assert.equal(next.filter, "no-cover");
  assert.equal(next.page, 1);
  assert.equal(next.data, null);
  assert.equal(next.sort, "size");
  assert.equal(next.dir, "asc");
  assert.equal(next.busy, false);
  assert.equal(context.adminLibraryPresetState(undefined, "no-series").page, 1);
});

test("library refresh hook exists and app.js saves refresh it and presets reset the page", () => {
  assert.equal(typeof context.refreshAdminAudiobookLibrary, "function");
  assert.ok(appSource.includes("window.refreshAdminAudiobookLibrary?.()"));
  assert.ok(appSource.includes("adminLibraryPresetState(state.admin.audiobookLibrary"));
});
