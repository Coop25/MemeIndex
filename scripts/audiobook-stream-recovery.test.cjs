// Run with: node --test scripts/audiobook-stream-recovery.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");
const recoveryStart = source.indexOf("// The server can replace a book's file mid-listen");
const recoveryEnd = source.indexOf("// --- Media Session (lock screen", recoveryStart);
assert.notEqual(recoveryStart, -1);
assert.notEqual(recoveryEnd, -1);
const recoverySource = source.slice(recoveryStart, recoveryEnd);

const STREAM_URL = "/audiobook-media/book-1";
const HAVE_NOTHING = 0;
const HAVE_METADATA = 1;
const HAVE_CURRENT_DATA = 2;
const HAVE_FUTURE_DATA = 3;

// The parts of HTMLMediaElement recovery depends on: assigning src resets the
// position and pauses without firing "pause", same as the load algorithm.
class FakeAudio extends EventTarget {
  constructor() {
    super();
    this.srcValue = STREAM_URL;
    this.loads = 0;
    this.paused = false;
    this.ended = false;
    this.seeking = false;
    this.error = null;
    this.currentTime = 1234;
    this.readyState = HAVE_FUTURE_DATA;
    this.playCalls = 0;
    this.playResult = () => Promise.resolve();
  }
  getAttribute() { return this.srcValue; }
  set src(value) {
    this.srcValue = value;
    this.loads += 1;
    this.paused = true;
    this.error = null;
    this.currentTime = 0;
    this.readyState = HAVE_NOTHING;
  }
  play() {
    this.playCalls += 1;
    this.paused = false;
    return this.playResult();
  }
  loadMetadata() {
    this.readyState = HAVE_METADATA;
    this.dispatchEvent(new Event("loadedmetadata"));
  }
}

function createHarness() {
  const audio = new FakeAudio();
  const context = vm.createContext({ AbortController, console, audio });
  vm.runInContext(`
    var clock = 1000000;
    var Date = { now: () => clock };
    var HTMLMediaElement = { HAVE_FUTURE_DATA: ${HAVE_FUTURE_DATA} };
    var state = { audiobooks: { player: { id: "book-1", streamUrl: "${STREAM_URL}" } } };
    var audiobookAudioEl = audio;
    var audiobookPlaybackElsewhereState = null;
    var status = "";
    function setAudiobookPlayerStatus(text) { status = text; }
    function clearAudiobookPlayerStatus(text) { if (status === text) status = ""; }
    function setAudiobookPlayIcon() {}
    function updateAudiobookBufferingUI() {}
    function isAudiobookAudioForPlayer(el, player) { return el.getAttribute("src") === player.streamUrl; }
    function fail(wasPlaying) { handleAudiobookStreamError(audio, wasPlaying); }
    function advance(ms) { clock += ms; tickAudiobookStreamRecovery(); }
  `, context);
  vm.runInContext(recoverySource, context);
  return { audio, context, run: (code) => vm.runInContext(code, context) };
}

test("keeps retrying through an outage longer than one attempt, then resumes in place", () => {
  const { audio, run } = createHarness();

  run("fail(true)");
  assert.equal(audio.loads, 1, "first retry is immediate, for the remux file swap");
  assert.equal(run("status"), "Connection lost - reconnecting...");

  // Every reload fails while the network is still down.
  for (let i = 0; i < 6; i += 1) {
    run("fail(!audio.paused)");
    run("advance(30000)");
  }
  assert.equal(audio.loads, 7);

  audio.loadMetadata();
  assert.equal(audio.currentTime, 1234, "position survives failed reloads that reset currentTime");
  assert.equal(audio.playCalls, 1);
  audio.readyState = HAVE_FUTURE_DATA;
  run("finishAudiobookStreamRecovery()");
  assert.equal(run("status"), "");
  run("advance(60000)");
  assert.equal(audio.loads, 7, "no retries after recovering");
});

test("backs off between attempts instead of hammering the server", () => {
  const { audio, run } = createHarness();

  run("fail(true)");
  run("fail(true)");
  run("advance(1000)");
  assert.equal(audio.loads, 1);
  run("advance(1000)");
  assert.equal(audio.loads, 2);
});

test("a reload that hangs without an error event still moves on", () => {
  const { audio, run } = createHarness();

  run("fail(true)");
  run("advance(29000)");
  assert.equal(audio.loads, 1);
  run("advance(1000)");
  assert.equal(audio.loads, 2);
});

test("gives up with the error status once the retry window is used up", () => {
  const { audio, run } = createHarness();

  run("fail(true)");
  run("advance(15 * 60 * 1000)");
  assert.match(run("status"), /^Playback error/);
  assert.equal(run("audiobookStreamRecovery"), null);
  const loads = audio.loads;
  run("advance(60000)");
  assert.equal(audio.loads, loads);
});

test("play after giving up restarts from the position playback failed at", () => {
  const { audio, run } = createHarness();

  run("fail(true)");
  run("advance(15 * 60 * 1000)");
  audio.error = { code: 2 };
  run("retryAudiobookStreamNow(audio)");
  assert.equal(run("status"), "Connection lost - reconnecting...");
  audio.loadMetadata();
  assert.equal(audio.currentTime, 1234);
  assert.equal(audio.playCalls, 1);
});

test("a silent stall with no error event triggers recovery", () => {
  const { audio, run } = createHarness();

  run("noteAudiobookStreamActivity()");
  audio.readyState = HAVE_CURRENT_DATA;
  run("advance(19000)");
  assert.equal(audio.loads, 0);
  run("advance(1000)");
  assert.equal(audio.loads, 1);
  audio.loadMetadata();
  assert.equal(audio.currentTime, 1234);
  assert.equal(audio.playCalls, 1);
});

test("arriving data on a slow connection is not a stall", () => {
  const { audio, run } = createHarness();

  run("noteAudiobookStreamActivity()");
  audio.readyState = HAVE_CURRENT_DATA;
  for (let i = 0; i < 5; i += 1) {
    run("advance(15000)");
    run("noteAudiobookStreamActivity()");
  }
  assert.equal(audio.loads, 0);
});

test("a paused player is never treated as stalled", () => {
  const { audio, run } = createHarness();

  run("noteAudiobookStreamActivity()");
  audio.paused = true;
  audio.readyState = HAVE_CURRENT_DATA;
  run("advance(600000)");
  assert.equal(audio.loads, 0);
});

test("a failure while paused reloads without starting playback", () => {
  const { audio, run } = createHarness();

  audio.paused = true;
  run("fail(false)");
  audio.loadMetadata();
  assert.equal(audio.currentTime, 1234);
  assert.equal(audio.playCalls, 0);
  assert.equal(run("audiobookStreamRecovery"), null);
});

test("closing or switching the book abandons recovery", () => {
  const { audio, run } = createHarness();

  run("fail(true)");
  run('state.audiobooks.player = { id: "book-2", streamUrl: "/audiobook-media/book-2" }');
  run("advance(60000)");
  assert.equal(run("audiobookStreamRecovery"), null);
  assert.equal(audio.loads, 1);
  audio.loadMetadata();
  assert.equal(audio.playCalls, 0, "a stale reload never seeks or plays the new book");
});

test("another device taking over stops recovery from resuming here", () => {
  const { audio, run } = createHarness();

  run("fail(true)");
  run("audiobookPlaybackElsewhereState = { positionSeconds: 1300 }");
  run("advance(60000)");
  assert.equal(run("audiobookStreamRecovery"), null);
  assert.equal(audio.loads, 1);
});

test("a blocked background autoplay asks for a tap and stops retrying", async () => {
  const { audio, run } = createHarness();

  audio.playResult = () => Promise.reject(Object.assign(new Error("blocked"), { name: "NotAllowedError" }));
  run("fail(true)");
  audio.loadMetadata();
  await new Promise((done) => setImmediate(done));
  assert.equal(run("status"), "Tap play to keep listening.");
  assert.equal(run("audiobookStreamRecovery"), null);
});
