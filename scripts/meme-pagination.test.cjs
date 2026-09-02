// Run with: node --test scripts/meme-pagination.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");

const context = vm.createContext({});
const paginationStart = source.indexOf("function canAppendMemePage(");
const paginationEnd = source.indexOf("\nfunction ensureMemeGridObserver(", paginationStart);
assert.notEqual(paginationStart, -1);
assert.notEqual(paginationEnd, -1);
vm.runInContext(source.slice(paginationStart, paginationEnd), context);

test("append requests must be the next page and cannot overlap", () => {
  assert.equal(context.canAppendMemePage({
    loading: false,
    hasMore: true,
    currentPage: 2,
    requestedPage: 3,
  }), true);
  assert.equal(context.canAppendMemePage({
    loading: true,
    hasMore: true,
    currentPage: 2,
    requestedPage: 3,
  }), false);
  assert.equal(context.canAppendMemePage({
    loading: false,
    hasMore: false,
    currentPage: 2,
    requestedPage: 3,
  }), false);
  assert.equal(context.canAppendMemePage({
    loading: false,
    hasMore: true,
    currentPage: 2,
    requestedPage: 4,
  }), false);
});

test("auto pagination requires a fresh user scroll for every page", () => {
  const request = {
    armed: true,
    libraryMode: true,
    loading: false,
    hasMore: true,
    currentPage: 0,
    requestedPage: 1,
    lastRequestedPage: 0,
    scrollRevision: 4,
    lastLoadScrollRevision: 3,
  };

  assert.equal(context.canRequestMemeAutoPage(request), true);
  assert.equal(context.canRequestMemeAutoPage({
    ...request,
    scrollRevision: 3,
  }), false);
  assert.equal(context.canRequestMemeAutoPage({
    ...request,
    lastRequestedPage: 1,
  }), false);
  assert.equal(context.canRequestMemeAutoPage({
    ...request,
    armed: false,
  }), false);

  assert.equal(context.canRequestMemeAutoPage({
    ...request,
    currentPage: 1,
    requestedPage: 2,
    lastRequestedPage: 1,
    scrollRevision: 5,
    lastLoadScrollRevision: 4,
  }), true);
});
