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
const sizeStart = source.indexOf("function formatSize(");
vm.runInContext(source.slice(sizeStart, source.indexOf("\n}", sizeStart) + 2), context);
const chartStart = source.indexOf("function dashboardTrend(");
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

test("top tag chart compares displayed tags without an unlisted remainder or center total", () => {
  const tags = [
    { tag: "music", count: 49 }, { tag: "song", count: 37 }, { tag: "politics", count: 31 },
    { tag: "food", count: 29 }, { tag: "cat", count: 23 }, { tag: "dog", count: 21 },
    { tag: "gaming", count: 19 }, { tag: "sports", count: 17 },
  ];
  const html = context.buildAdminTopTags(tags);
  assert.match(html, /gaming\n19 tag assignments/);
  assert.doesNotMatch(html, /Other tags|2,271|Assignments<\/span>/);
  assert.equal((html.match(/class="admin-tag-slice"/g) || []).length, tags.length);
  assert.equal((html.match(/data-admin-tag=/g) || []).length, tags.length);
  assert.match(html, /data-admin-tag="music"/);
});

test("top tag chart caps dense distributions at twelve named slices", () => {
  const tags = Array.from({ length: 15 }, (_, index) => ({ tag: `tag-${index + 1}`, count: 20 - index }));
  const html = context.buildAdminTopTags(tags);
  assert.equal((html.match(/class="admin-tag-slice"/g) || []).length, 12);
  assert.match(html, /data-admin-tag="tag-12"/);
  assert.doesNotMatch(html, /tag-13|Other tags/);
});

test("empty and full-circle tag distributions render without phantom sections", () => {
  assert.match(context.buildAdminTopTags([]), /No tags have been used yet/);
  const html = context.buildAdminTopTags([{ tag: "only", count: 7 }]);
  assert.equal((html.match(/class="admin-tag-slice"/g) || []).length, 1);
  assert.doesNotMatch(html, /Other tags|NaN|Infinity/);
  assert.match(html, /7 tag assignments \(100.0% of top tags\)/);
  assert.equal((html.match(/A65,65/g) || []).length, 2);
});

test("tag names are escaped in both tooltip attributes and legend text", () => {
  const html = context.buildAdminTopTags([{ tag: '\"><img src=x onerror=alert(1)>', count: 1 }]);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&quot;&gt;&lt;img/);
});

test("storage card totals memes and audiobooks", () => {
  const metrics = context.buildAdminDashboardMetrics({
    total_size_bytes: 1024, total_storage_bytes: 3072,
    audiobook_stats: { total_bytes: 2048, count: 2, duration_seconds: 7200, added_last_30_days: 1, series: [] },
    metric_series: [], counts: {},
  });
  const storage = metrics.find((metric) => /^Storage/.test(metric.label));
  assert.equal(storage.value, context.formatSize(3072));
  assert.match(storage.note, /Audiobooks/);
  assert.match(storage.note, new RegExp(`Memes ${context.formatSize(1024)}`));
  const books = metrics.find((metric) => metric.label === "Audiobooks");
  assert.equal(books.value, "2");
  assert.match(books.note, /^2 h total/);
  assert.match(books.note, /\+1 in 30 days/);
});

test("audiobook card hidden without stats", () => {
  const metrics = context.buildAdminDashboardMetrics({ counts: {}, metric_series: [] });
  assert.equal(metrics.some((metric) => metric.label === "Audiobooks"), false);
});

test("disk card shows Unknown and low", () => {
  const unknown = context.buildAdminDashboardMetrics({ counts: {}, metric_series: [] }).find((metric) => metric.label === "Disk");
  assert.equal(unknown.value, "Unknown");
  const low = context.buildAdminDashboardMetrics({ counts: {}, metric_series: [], disk: { total_bytes: 100, free_bytes: 5, used_bytes: 95, low: true } }).find((metric) => metric.label === "Disk");
  assert.equal(low.accent, "red");
  assert.equal(low.meter, 0.95);
  assert.match(low.value, /free$/);
});

test("empty dashboard produces no NaN", () => {
  const metrics = context.buildAdminDashboardMetrics({ counts: {}, metric_series: [] });
  assert.doesNotMatch(JSON.stringify(metrics), /NaN/);
});

test("combined storage series adds audiobook bytes by date", () => {
  const metrics = context.buildAdminDashboardMetrics({
    total_size_bytes: 10, total_storage_bytes: 15, counts: {},
    metric_series: [{ date: "2026-01-01", storage_bytes: 5 }, { date: "2026-01-02", storage_bytes: 10 }],
    audiobook_stats: { total_bytes: 5, count: 1, duration_seconds: 0, added_last_30_days: 0, series: [{ date: "2026-01-02", count: 1, bytes: 5 }] },
  });
  const storage = metrics.find((metric) => /^Storage/.test(metric.label));
  assert.deepEqual(Array.from(storage.series, (entry) => entry.storage_bytes), [5, 15]);
});

test("work queue chips link to their tabs", () => {
  const html = context.buildAdminWorkQueues({ delete_queue: 3, suggested_tags: 0, link_retries: 2, rejected_links: 0, conversion_queue: 7 });
  const chips = [...html.matchAll(/<button[^>]*class="admin-queue-chip"[^>]*>/g)].map((m) => m[0]);
  assert.equal(chips.length, 5);
  for (const tab of ["delete-queue", "tag-review", "link-retries", "rejected-links", "audiobook-conversion"]) {
    assert.ok(chips.some((chip) => chip.includes(`data-admin-open-tab="${tab}"`)), tab);
  }
  assert.ok(chips.find((c) => c.includes('"tag-review"')).includes('data-empty="true"'));
  assert.ok(chips.find((c) => c.includes('"delete-queue"')).includes('data-empty="false"'));
  assert.doesNotMatch(context.buildAdminWorkQueues(null), /NaN/);
  const noTab = context.buildAdminWorkQueues({ delete_queue: 1 }, () => false);
  assert.doesNotMatch(noTab, /data-admin-open-tab/);
});

test("health buttons carry library filters", () => {
  const html = context.buildAdminAudiobookHealth({ awaiting_conversion: 1, failed_conversion: 2, missing_cover: 3, missing_chapters: 0, missing_series: 5 });
  for (const f of ["no-cover", "no-chapters", "no-series", "not-converted", "conversion-failed"]) {
    assert.match(html, new RegExp(`data-admin-library-filter="${f}"`));
  }
  assert.match(html, /data-admin-open-tab="audiobook-library"/);
  const plain = context.buildAdminAudiobookHealth({ missing_cover: 3 }, () => false);
  assert.doesNotMatch(plain, /<button|data-admin-open-tab/);
  assert.doesNotMatch(plain, /NaN/);
});

test("listening chart handles empty activity", () => {
  const html = context.buildAdminListening({ daily_seconds: [], top_books: [], top_listeners: [] });
  assert.doesNotMatch(html, /NaN/);
  assert.match(html, /No listening yet/);
  const full = context.buildAdminListening({
    daily_seconds: [{ date: "2026-01-01", seconds: 600 }, { date: "2026-01-02", seconds: 0 }],
    top_books: [{ id: "b<1>", title: "<b>Book</b>", seconds: 3700 }],
    top_listeners: [{ user_id: "u", display_name: "Al & Co", seconds: 90 }],
  });
  assert.doesNotMatch(full, /NaN|<b>Book/);
  assert.match(full, /Minutes: 10/);
  assert.match(full, /Al &amp; Co/);
});

test("upload chart output is unchanged by default labels", () => {
  const html = context.buildAdminUploadChart([{ date: "2026-01-01", uploads: 2 }]);
  assert.match(html, /uploads in 30 days/);
  assert.match(html, /Uploads: 2/);
});
