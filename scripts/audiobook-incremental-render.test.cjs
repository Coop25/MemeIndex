// Run with: node --test scripts/audiobook-incremental-render.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");

function slice(startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start);
  assert.notEqual(start, -1, startMarker);
  assert.notEqual(end, -1, endMarker);
  return source.slice(start, end);
}

const fetchSource = slice("// The books grid is fetched a page at a time", "async function fetchAudiobookSeries");
const loaderSource = slice("// Audiobook lists load a chunk at a time", "// renderAudiobookStatsView");
const gridSource = slice("// The main grid shows either books", "async function toggleAudiobookFavorite");

class FakeClassList {
  constructor(initial = []) { this.set = new Set(initial); }
  contains(name) { return this.set.has(name); }
  toggle(name, force) {
    const on = force === undefined ? !this.set.has(name) : force;
    if (on) this.set.add(name); else this.set.delete(name);
    return on;
  }
}

// Just enough of an element for the grid: cards are tracked as an array of
// ids parsed back out of the HTML the card templates produce.
function fakeGrid() {
  return {
    cards: [],
    classList: new FakeClassList(),
    set innerHTML(value) { this.cards = value ? [value] : []; },
    insertAdjacentHTML(_where, html) {
      for (const match of html.matchAll(/data-id="([^"]+)"/g)) this.cards.push(match[1]);
    },
    querySelector() { return null; },
  };
}

const flush = async () => {
  for (let i = 0; i < 10; i++) await new Promise((done) => setImmediate(done));
};

// A stand-in for GET /api/audiobooks with the same limit/offset/has_more
// contract as handleAudiobooks. Each filter gets its own id prefix so tests
// can tell which list landed. hold() makes the next response wait.
function fakeServer(total) {
  const server = { requests: [], books: null, fail: false, held: [] };
  server.listFor = (filter) => server.books ?? Array.from({ length: total }, (_, i) => ({ id: `${filter || "all"}-${i}` }));
  server.fetch = async (url) => {
    const params = new URL(url, "http://x").searchParams;
    const request = { limit: params.get("limit"), offset: params.get("offset"), filter: params.get("filter") || "" };
    server.requests.push(request);
    if (server.held.length) await server.held.shift();
    if (server.fail) throw new Error("network down");
    const list = server.listFor(request.filter);
    const limit = Number(request.limit || 0);
    const offset = Number(request.offset || 0);
    const page = limit > 0 ? list.slice(offset, offset + limit) : list;
    return { json: async () => ({ audiobooks: page, has_more: limit > 0 && offset + limit < list.length }) };
  };
  server.hold = () => {
    let release;
    server.held.push(new Promise((done) => { release = done; }));
    return () => release();
  };
  return server;
}

function setup({ books = 0, series = 0, observer = true } = {}) {
  const observers = [];
  class FakeIntersectionObserver {
    constructor(callback) { this.callback = callback; this.targets = new Set(); this.observes = 0; observers.push(this); }
    observe(target) { this.targets.add(target); this.observes += 1; }
    unobserve(target) { this.targets.delete(target); }
    // Simulate the sentinel being in (or near) the viewport.
    fire() { this.callback([...this.targets].map((target) => ({ target, isIntersecting: true }))); }
  }
  const server = fakeServer(books);
  const context = {
    window: observer ? { IntersectionObserver: FakeIntersectionObserver } : {},
    IntersectionObserver: FakeIntersectionObserver,
    JSON, URLSearchParams, Set, Math, Promise, console: { error() {} },
    fetch: (url) => server.fetch(url),
    expectAuthorized: async () => true,
    audiobooksStatus: null,
    audiobooksGrid: fakeGrid(),
    audiobooksGridSentinel: { classList: new FakeClassList(["hidden"]), textContent: "" },
    audiobooksEmpty: { classList: new FakeClassList(["hidden"]) },
    audiobookCardHTML: (item) => `<article data-id="${item.id}"></article>`,
    audiobookSeriesCardHTML: (entry) => `<article data-id="${entry.id}"></article>`,
    wireAudiobookSeriesCard: () => {},
    state: {
      filters: { query: "" },
      audiobooks: {
        view: "books", filter: "", seriesId: "", author: "", tag: "",
        loading: false, appending: false, hasMore: false,
        items: [],
        series: Array.from({ length: series }, (_, i) => ({ id: `s${i}` })),
      },
    },
  };
  vm.createContext(context);
  vm.runInContext(`${fetchSource}\n${loaderSource}\n${gridSource}`, context);
  return { context, observers, server, grid: context.audiobooksGrid, sentinel: context.audiobooksGridSentinel };
}

async function scrollToSentinel(observers) {
  observers[0].fire();
  await flush();
}

test("loads the first page, then one more page each time the sentinel is reached", async () => {
  const { context, observers, server, grid, sentinel } = setup({ books: 120 });
  await context.fetchAudiobooks();
  assert.deepEqual(server.requests[0], { limit: "48", offset: null, filter: "" });
  assert.equal(grid.cards.length, 48);
  assert.equal(sentinel.classList.contains("hidden"), false);

  await scrollToSentinel(observers);
  assert.deepEqual(server.requests[1], { limit: "48", offset: "48", filter: "" });
  assert.equal(grid.cards.length, 96);

  await scrollToSentinel(observers);
  assert.equal(grid.cards.length, 120);
  assert.equal(grid.cards[0], "all-0");
  assert.equal(grid.cards[119], "all-119");
  assert.equal(sentinel.classList.contains("hidden"), true);

  await scrollToSentinel(observers);
  assert.equal(server.requests.length, 3);
});

test("a page landing re-checks the sentinel so a tall window keeps filling", async () => {
  const { context, observers } = setup({ books: 120 });
  await context.fetchAudiobooks();
  const before = observers[0].observes;
  await scrollToSentinel(observers);
  assert.equal(observers[0].observes, before + 1);
});

test("refreshing the same list reloads everything already scrolled in", async () => {
  const { context, observers, server, grid } = setup({ books: 120 });
  await context.fetchAudiobooks();
  await scrollToSentinel(observers);
  await context.fetchAudiobooks();
  assert.deepEqual(server.requests.at(-1), { limit: "96", offset: null, filter: "" });
  assert.equal(grid.cards.length, 96);
});

test("switching to another list starts back at one page", async () => {
  const { context, observers, server, grid } = setup({ books: 120 });
  await context.fetchAudiobooks();
  await scrollToSentinel(observers);
  context.state.audiobooks.filter = "favorites";
  await context.fetchAudiobooks();
  assert.deepEqual(server.requests.at(-1), { limit: "48", offset: null, filter: "favorites" });
  assert.equal(grid.cards.length, 48);
  assert.equal(grid.cards[0], "favorites-0");
});

test("a slow response for a list you've left doesn't overwrite the new one", async () => {
  const { context, server, grid } = setup({ books: 120 });
  const releaseOld = server.hold();
  const oldFetch = context.fetchAudiobooks();
  context.state.audiobooks.filter = "finished";
  await context.fetchAudiobooks();
  releaseOld();
  assert.equal(await oldFetch, false);
  assert.equal(grid.cards[0], "finished-0");
  assert.equal(context.state.audiobooks.items[0].id, "finished-0");
});

test("a book shifted into the next page by a new upload isn't shown twice", async () => {
  const { context, observers, server } = setup({ books: 0 });
  server.books = Array.from({ length: 60 }, (_, i) => ({ id: `b${i}` }));
  await context.fetchAudiobooks();
  server.books = [{ id: "new" }, ...server.books];
  await scrollToSentinel(observers);
  const ids = context.state.audiobooks.items.map((item) => item.id);
  assert.equal(ids.length, new Set(ids).size);
  assert.equal(ids.length, 60);
});

test("a failed page shows a retry hint and doesn't retry on its own", async () => {
  const { context, observers, server, sentinel } = setup({ books: 120 });
  await context.fetchAudiobooks();
  server.fail = true;
  const before = observers[0].observes;
  await scrollToSentinel(observers);
  assert.match(sentinel.textContent, /Could not load more/);
  assert.equal(observers[0].observes, before);
  assert.equal(context.state.audiobooks.loading, false);

  server.fail = false;
  await scrollToSentinel(observers);
  assert.equal(context.state.audiobooks.items.length, 96);
});

test("the series gallery draws its whole list a chunk at a time", async () => {
  const { context, observers, grid, sentinel, server } = setup({ series: 70 });
  context.state.audiobooks.view = "series";
  context.renderAudiobooksGrid();
  assert.equal(grid.cards.length, 48);
  assert.equal(grid.classList.contains("audiobooks-grid-series"), true);
  await scrollToSentinel(observers);
  assert.equal(grid.cards.length, 70);
  assert.equal(sentinel.classList.contains("hidden"), true);
  assert.equal(server.requests.length, 0);
});

test("without IntersectionObserver the whole list is fetched at once", async () => {
  const { context, server, grid } = setup({ books: 120, observer: false });
  await context.fetchAudiobooks();
  assert.equal(server.requests[0].limit, null);
  assert.equal(grid.cards.length, 120);
});
