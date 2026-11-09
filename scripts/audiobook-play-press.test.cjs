// Run with: node --test scripts/audiobook-play-press.test.cjs
//
// Pressing play used to await the saved-position fetch with nothing on
// screen, then call play() outside the tap - which a phone's browser refuses
// for an element that hasn't played yet, silently. These pin the fix: the
// button shows busy from the press, the first play starts inside the press,
// and a refusal says so.
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
const bufferingSource = slice('const AUDIOBOOK_BUFFERING_STATUS = "Buffering...";', "// renderAudiobookMiniPlayerInfo fills");
const resumeSource = slice("// Below this, a seek onto the saved position", "// setAudiobookPlayerPosition is the shared path");

const HAVE_NOTHING = 0;
const HAVE_FUTURE_DATA = 3;

class FakeAudio extends EventTarget {
  constructor() {
    super();
    this.paused = true;
    this.ended = false;
    this.seeking = false;
    this.error = null;
    this.currentTime = 0;
    this.duration = 40000;
    this.readyState = HAVE_NOTHING;
    this.playCalls = [];
    this.playResult = () => Promise.resolve();
  }
  getAttribute() { return "/audiobook-media/book-1"; }
  play() {
    this.playCalls.push(this.currentTime);
    const result = this.playResult();
    // A refused play() leaves the element paused, per spec.
    this.paused = false;
    result.catch(() => { this.paused = true; });
    return result;
  }
}

function createHarness({ unlocked = false } = {}) {
  const audio = new FakeAudio();
  const button = { classes: new Set(), classList: { toggle(name, on) { on ? button.classes.add(name) : button.classes.delete(name); } }, setAttribute() {} };
  let resolvePosition;
  const context = vm.createContext({ console, audio, button });
  vm.runInContext(`
    var HTMLMediaElement = { HAVE_FUTURE_DATA: ${HAVE_FUTURE_DATA} };
    var state = { audiobooks: { player: { id: "book-1", durationSeconds: 40000 } } };
    var audiobookAudioEl = audio;
    var audiobookPlayerPlayButton = button;
    var audiobookPlayerStatus = { textContent: "" };
    var audiobookPlayerOpenItem = { id: "book-1", position_seconds: 30000 };
    var audiobookStreamRecovery = null;
    var positionRequests = [];
    function fetchAudiobookSavedPosition() { return new Promise((r) => positionRequests.push(r)); }
    function setAudiobookPlayerStatus(text) { audiobookPlayerStatus.textContent = text; }
    function clearAudiobookPlayerStatus(text) { if (audiobookPlayerStatus.textContent === text) audiobookPlayerStatus.textContent = ""; }
    function renderAudiobookMiniPlayerInfo() {}
    function retryAudiobookStreamNow() {}
  `, context);
  vm.runInContext(bufferingSource, context);
  vm.runInContext(resumeSource, context);
  if (unlocked) vm.runInContext("audiobookPlaybackUnlocked = true", context);
  const run = (code) => vm.runInContext(code, context);
  return {
    audio,
    run,
    busy: () => button.classes.has("is-buffering"),
    press: () => run("resumeAudiobookPlaybackFromSavedPosition()"),
    answerPosition: (value) => run("positionRequests.shift()")(value),
  };
}

const flush = () => new Promise((r) => setImmediate(r));

test("first play starts inside the press, at the known position, and shows busy", async () => {
  const h = createHarness();
  h.run('audiobookPlayPending = "open"');
  const done = h.press();
  assert.deepEqual(h.audio.playCalls, [30000], "played synchronously from the open flow's known position");
  assert.equal(h.busy(), true, "spinner while the deep seek loads");
  h.answerPosition(30500);
  await done;
  assert.equal(h.audio.currentTime, 30500, "reconciled with the server's position afterwards");
});

test("an unlocked element waits for the position but shows busy right away", async () => {
  const h = createHarness({ unlocked: true });
  h.audio.currentTime = 30000;
  const done = h.press();
  assert.equal(h.audio.playCalls.length, 0);
  assert.equal(h.busy(), true, "busy before the element is even unpaused");
  h.answerPosition(31000);
  await done;
  assert.deepEqual(h.audio.playCalls, [31000]);
  assert.equal(h.busy(), true, "element took over: unpaused but no data yet");
  h.audio.readyState = HAVE_FUTURE_DATA;
  h.run("updateAudiobookBufferingUI()");
  assert.equal(h.busy(), false);
});

test("a refused play() clears busy and asks for another tap instead of doing nothing", async () => {
  const h = createHarness({ unlocked: true });
  h.audio.playResult = () => Promise.reject(Object.assign(new Error("nope"), { name: "NotAllowedError" }));
  const done = h.press();
  h.answerPosition(null);
  await done;
  await flush();
  assert.equal(h.busy(), false);
  assert.equal(h.run("audiobookPlayerStatus.textContent"), "Tap play to start listening.");
});

test("cancelling a pending press drops it", async () => {
  const h = createHarness({ unlocked: true });
  const done = h.press();
  h.run("cancelAudiobookPlayRequest()");
  assert.equal(h.busy(), false);
  h.answerPosition(31000);
  await done;
  assert.equal(h.audio.playCalls.length, 0);
});

test("a timed-out position fetch still plays from the known position while opening", async () => {
  const h = createHarness({ unlocked: true });
  h.run('audiobookPlayPending = "open"');
  const done = h.press();
  h.answerPosition(null);
  await done;
  assert.deepEqual(h.audio.playCalls, [30000]);
});

test("an early press on a finished book starts over instead of seeking to its end", async () => {
  const h = createHarness();
  h.run('audiobookPlayPending = "open"; audiobookPlayerOpenItem.position_seconds = 40000');
  const done = h.press();
  assert.deepEqual(h.audio.playCalls, [0], "not seeked onto the very end");
  h.answerPosition(null);
  await done;
  assert.equal(h.audio.currentTime, 0);
});

test("once the open flow has seeked, a deliberate jump to 0:00 is respected", async () => {
  // e.g. "mark as not started", or scrubbing back to the start, before ever
  // pressing play on this device.
  const h = createHarness();
  h.audio.currentTime = 0;
  const done = h.press();
  assert.deepEqual(h.audio.playCalls, [0], "not dragged back to the stale known position");
  h.answerPosition(null);
  await done;
  assert.equal(h.audio.currentTime, 0);
});
