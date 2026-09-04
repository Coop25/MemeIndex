// Run with: node --test scripts/modal-history.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");
const historyStart = source.indexOf("const MODAL_HISTORY_STATE_KEY");
const historyEnd = source.indexOf("\nlet memeGridObserver", historyStart);
assert.notEqual(historyStart, -1);
assert.notEqual(historyEnd, -1);
const historySource = source.slice(historyStart, historyEnd);

function createContext() {
  const context = vm.createContext({});
  vm.runInContext(`
    var pushedStates = [];
    var backCalls = 0;
    var reelCloseOptions = null;
    var memeCloseOptions = null;
    var allowMemeClose = true;
    var randomReelModal = { open: false };
    var memeModal = { open: false };
    var window = {
      location: { href: "https://memeindex.test/library" },
      history: {
        state: null,
        pushState(state, title, url) {
          this.state = state;
          pushedStates.push({ state, title, url });
        },
        back() { backCalls += 1; },
      },
    };
    function closeRandomReel(options) { reelCloseOptions = options; }
    function closeModal(options) {
      memeCloseOptions = options;
      return allowMemeClose;
    }
  `, context);
  vm.runInContext(historySource, context);
  return context;
}

test("opening a modal adds one same-URL history guard", () => {
  const context = createContext();

  vm.runInContext('pushModalHistoryState("meme")', context);
  vm.runInContext('pushModalHistoryState("meme")', context);

  assert.equal(vm.runInContext("pushedStates.length", context), 1);
  assert.equal(vm.runInContext("window.history.state.memeIndexModal", context), "meme");
  assert.equal(vm.runInContext("pushedStates[0].url", context), "https://memeindex.test/library");
});

test("a normal close unwinds its guard without closing again on popstate", () => {
  const context = createContext();
  vm.runInContext('pushModalHistoryState("random-reel")', context);
  vm.runInContext('unwindModalHistoryState("random-reel")', context);
  vm.runInContext("randomReelModal.open = true; handleModalHistoryPop()", context);

  assert.equal(vm.runInContext("backCalls", context), 1);
  assert.equal(vm.runInContext("reelCloseOptions", context), null);
});

test("Android Back closes each supported modal without another history change", () => {
  const reelContext = createContext();
  vm.runInContext("randomReelModal.open = true; handleModalHistoryPop()", reelContext);
  assert.equal(vm.runInContext("reelCloseOptions.syncHistory", reelContext), false);
  assert.equal(vm.runInContext("backCalls", reelContext), 0);

  const memeContext = createContext();
  vm.runInContext("memeModal.open = true; handleModalHistoryPop()", memeContext);
  assert.equal(vm.runInContext("memeCloseOptions.syncHistory", memeContext), false);
  assert.equal(vm.runInContext("backCalls", memeContext), 0);
});

test("canceling the unsaved-changes prompt restores the meme guard", () => {
  const context = createContext();
  vm.runInContext("allowMemeClose = false; memeModal.open = true; handleModalHistoryPop()", context);

  assert.equal(vm.runInContext("pushedStates.length", context), 1);
  assert.equal(vm.runInContext("window.history.state.memeIndexModal", context), "meme");
});
