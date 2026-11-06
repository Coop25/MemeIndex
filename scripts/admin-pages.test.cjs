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

test("user activity sorts by listening hours", () => {
  const users = [
    { user_id: "a", display_name: "Ann", seconds_all_time: 3600, seconds_30d: 60 },
    { user_id: "b", display_name: "Bob", seconds_all_time: 36000, seconds_30d: 10 },
    { user_id: "c", display_name: "Cy", seconds_all_time: 7200, seconds_30d: 5 },
  ];
  const html = context.buildUserActivityTable(users, { sort: "hours", dir: "desc" });
  const order = ["Bob", "Cy", "Ann"].map((name) => html.indexOf(`>${name}<`));
  assert.ok(order.every((i) => i >= 0), "all names rendered");
  assert.ok(order[0] < order[1] && order[1] < order[2]);
  const asc = context.buildUserActivityTable(users, { sort: "hours", dir: "asc" });
  assert.ok(asc.indexOf(">Ann<") < asc.indexOf(">Bob<"));
  assert.match(html, /data-user-activity-id="b"/);
});

test("user activity escapes names and shows empty state", () => {
  const html = context.buildUserActivityTable([{ user_id: "x", display_name: "<img onerror=1>" }], { sort: "name", dir: "asc" });
  assert.ok(!html.includes("<img"));
  assert.match(context.buildUserActivityTable([], { sort: "name", dir: "asc" }), /admin-pages-empty/);
});

test("user activity tab, panel, actor chip and actor param are wired once", () => {
  const html = readFileSync(resolve(__dirname, "../static/index.html"), "utf8");
  assert.equal((html.match(/id="admin-user-activity-panel"/g) || []).length, 1);
  assert.match(appSource, /"user-activity": \{[^}]*panel: "admin-user-activity-panel"/);
  assert.match(appSource, /sibling: "user-activity"/);
  assert.ok(appSource.includes("window.loadAdminUserActivity()"));
  assert.ok(appSource.includes("window.renderAdminUserActivity?.()"));
  assert.ok(appSource.includes("data-clear-audit-actor"));
  assert.ok(appSource.includes('params.set("actor"'));
});

test("setting the audit actor resets paging", () => {
  const st = { admin: { audit: { offset: 200, total: 500, hasMore: true }, auditActor: null } };
  context.state = st;
  context.setAdminAuditActor({ user_id: "u1", display_name: "Ann" });
  assert.equal(st.admin.auditActor.user_id, "u1");
  assert.equal(st.admin.audit.offset, 0);
  st.admin.audit.offset = 100;
  context.setAdminAuditActor(null);
  assert.equal(st.admin.auditActor, null);
  assert.equal(st.admin.audit.offset, 0);
});

/* ---------- Series & Tags page ---------- */

test("series delete confirm mentions book count", () => {
  assert.equal(
    context.seriesDeleteConfirmText({ name: "Dune", book_count: 6 }),
    "Delete “Dune”? Its 6 books will no longer be in a series.",
  );
  assert.equal(context.seriesDeleteConfirmText({ name: "Empty", book_count: 0 }), "Delete “Empty”?");
  assert.equal(context.seriesDeleteConfirmText({ name: "One", book_count: 1 }), "Delete “One”? Its 1 book will no longer be in a series.");
});

test("series merge confirm text", () => {
  assert.equal(
    context.seriesMergeConfirmText({ name: "A", book_count: 3 }, { name: "B" }),
    "Move 3 books from “A” into “B” and delete “A”?",
  );
  assert.equal(context.seriesMergeConfirmText({ name: "A", book_count: 1 }, { name: "B" }), "Move 1 book from “A” into “B” and delete “A”?");
});

test("merge select excludes the series itself", () => {
  const series = [
    { id: "s1", name: "Alpha", book_count: 1 },
    { id: "s2", name: "Beta", book_count: 2 },
    { id: "s3", name: "Gamma", book_count: 0 },
  ];
  const html = context.buildSeriesList(series, "", []);
  const selects = html.split('<select').slice(1);
  assert.equal(selects.length, 3);
  assert.ok(!selects[0].includes('value="s1"'));
  assert.ok(selects[0].includes('value="s2"') && selects[0].includes('value="s3"'));
  assert.ok(!selects[1].includes('value="s2"'));
});

test("expanded series lists books with sequence inputs", () => {
  const html = context.buildSeriesList(
    [{ id: "s1", name: "Alpha", book_count: 2 }], "s1",
    [{ id: "b2", title: "Two", series_sequence: 2 }, { id: "b1", title: "One", series_sequence: 1.5 }],
    { b1: "Enter a number." },
  );
  assert.ok(html.includes('data-series-sequence="b1"') && html.includes('step="0.5"'));
  assert.ok(html.indexOf(">One<") < html.indexOf(">Two<"), "sorted by sequence");
  assert.ok(html.includes("Enter a number."));
});

test("series names are escaped", () => {
  const html = context.buildSeriesList(
    [{ id: "s1", name: '<img src=x onerror=1>"', book_count: 1 }], "s1",
    [{ id: "b1", title: "<script>x</script>", series_sequence: 1 }],
  );
  assert.ok(!html.includes("<img"));
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&lt;img"));
});

test("tag usage lists restricted users", () => {
  const html = context.buildAudiobookTagUsage([
    { tag: "scifi", audiobooks: 4, memes: 2, restricted_users: [{ user_id: "u1", display_name: "Ann <b>" }] },
    { tag: "misc", audiobooks: 1, memes: 0, restricted_users: [] },
  ]);
  assert.ok(html.includes("Ann &lt;b&gt;"));
  assert.ok(!html.includes("<b>"));
  assert.ok(html.includes('data-series-tag-hygiene="scifi"'));
  assert.ok(html.includes("data-series-tag-hygiene=\"misc\""));
});

test("sequence parsing validates numbers", () => {
  assert.equal(context.parseSeriesSequence("2.5"), 2.5);
  assert.equal(context.parseSeriesSequence(" 3 "), 3);
  assert.ok(Number.isNaN(context.parseSeriesSequence("abc")));
  assert.ok(Number.isNaN(context.parseSeriesSequence("")));
  assert.ok(Number.isNaN(context.parseSeriesSequence("-1")));
});

test("series tab is wired once", () => {
  const html = readFileSync(resolve(__dirname, "../static/index.html"), "utf8");
  assert.equal((html.match(/id="admin-audiobook-series-panel"/g) || []).length, 1);
  assert.equal((html.match(/id="admin-series-list"/g) || []).length, 1);
  assert.equal((html.match(/id="admin-audiobook-tags"/g) || []).length, 1);
  assert.match(appSource, /"audiobook-series": \{[^}]*group: "Audiobooks"[^}]*panel: "admin-audiobook-series-panel"/);
  assert.ok(appSource.includes("window.loadAdminAudiobookSeries()"));
  assert.ok(appSource.includes("window.renderAdminAudiobookSeries?.()"));
});

test("largest files: audiobook rows get an open button", () => {
  const html = context.buildStorageLargest([
    { kind: "audiobook", id: "ab1", name: "Book", bytes: 5, owner: "Ann" },
    { kind: "cover", id: "c1", name: "Cover", bytes: 4, owner: "Ann" },
  ]);
  assert.match(html, /data-storage-open-audiobook="ab1"/);
  assert.ok(!html.includes('data-storage-open-audiobook="c1"'));
});

test("orphan delete confirm pluralizes", () => {
  assert.match(context.orphanDeleteMessage(1, 1024), /^Delete 1 file \(/);
  assert.match(context.orphanDeleteMessage(2, 2048), /^Delete 2 files \(/);
});

test("storage load failure shows the error and a later success clears it", async () => {
  vm.runInContext(`
    var state = { admin: {} };
    var __renders = 0;
    renderAdminStorage = function () { __renders += 1; };
    var __next = { ok: false, error: "Failed to load storage." };
    adminPagesFetchJSON = async function () { return __next; };
  `, context);
  assert.equal(await vm.runInContext("loadAdminStorage()", context), false);
  assert.equal(vm.runInContext("adminStorageState().status", context), "Failed to load storage.");
  assert.equal(vm.runInContext("__renders", context), 1);

  vm.runInContext('__next = { ok: true, body: { dirs: [] } };', context);
  assert.equal(await vm.runInContext("loadAdminStorage()", context), true);
  assert.equal(vm.runInContext("adminStorageState().status", context), "");

  // A post-delete message must survive its reload.
  vm.runInContext('adminStorageState().status = "Deleted 1 file, freed 1 KB.";', context);
  await vm.runInContext("loadAdminStorage()", context);
  assert.equal(vm.runInContext("adminStorageState().status", context), "Deleted 1 file, freed 1 KB.");
});
