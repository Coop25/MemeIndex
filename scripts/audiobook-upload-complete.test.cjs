// Run with: node --test scripts/audiobook-upload-complete.test.cjs
const assert = require("node:assert/strict");
const { test } = require("node:test");
const vm = require("node:vm");
const { readAppSource, extractAll } = require("./app-source.cjs");

const source = readAppSource();
const errorClasses = source.match(/^class AudiobookUpload\w+ extends Error \{\}$/gm).join("\n");
const uploadSource = extractAll(source, [
  "AUDIOBOOK_CHUNK_SIZE", "AUDIOBOOK_UPLOAD_CONCURRENCY_DEFAULT", "AUDIOBOOK_UPLOAD_STORAGE_KEY",
  "audiobookUploadQueue", "audiobookUploadQueueCancelled",
  "computeAudiobookUploadGaps", "splitAudiobookUploadChunks", "uploadAudiobookChunk",
  "submitAudiobookUpload", "uploadOneAudiobookFile", "uploadMissingAudiobookChunks",
  "completeAudiobookUpload", "readAudiobookUploadConflict", "fetchAudiobookUploadRanges",
  "loadSavedAudiobookUpload", "saveAudiobookUploadMarker",
]);

const MiB = 1024 * 1024;

// A file the upload code can slice: each slice remembers its byte range so
// the fake server knows what a PATCH carried.
function fakeFile(name, size) {
  return { name, size, type: "audio/mp4", slice: (start, end) => ({ start, end }) };
}

function response(status, body) {
  return { status, ok: status >= 200 && status < 300, json: async () => body };
}

// fakeUploadServer plays the audiobook upload API for one file. reply decides
// what complete answers (given whether every byte has arrived); lose lists
// chunk offsets whose PATCH reports success but whose bytes never land, for
// as many times as each is listed.
function fakeUploadServer({ totalBytes, reply, lose = [] }) {
  const server = { received: [], patches: [], completes: 0, statusChecks: 0, lose: [...lose] };
  server.ranges = () => {
    const sorted = [...server.received].sort((a, b) => a.start - b.start);
    const merged = [];
    for (const range of sorted) {
      const last = merged[merged.length - 1];
      if (last && range.start <= last.end) last.end = Math.max(last.end, range.end);
      else merged.push({ ...range });
    }
    return merged;
  };
  server.complete = () => server.ranges().reduce((sum, r) => sum + (r.end - r.start), 0) === totalBytes;
  server.fetch = async (url, options = {}) => {
    const method = options.method || "GET";
    if (method === "POST" && url === "/api/audiobooks/uploads") {
      return response(201, { upload_id: "up-1", received_ranges: [] });
    }
    if (method === "PATCH") {
      const offset = Number(options.headers["Upload-Offset"]);
      server.patches.push(offset);
      const lost = server.lose.indexOf(offset);
      if (lost >= 0) server.lose.splice(lost, 1);
      else server.received.push({ start: options.body.start, end: options.body.end });
      return response(204, null);
    }
    if (method === "GET" && url === "/api/audiobooks/uploads/up-1") {
      server.statusChecks += 1;
      return response(200, { upload_id: "up-1", received_ranges: server.ranges() });
    }
    if (method === "POST" && url === "/api/audiobooks/uploads/up-1/complete") {
      server.completes += 1;
      return reply(server.complete());
    }
    throw new Error(`unexpected ${method} ${url}`);
  };
  return server;
}

const incomplete = () => response(409, { error: "upload incomplete: received 1 of 2 bytes", code: "incomplete" });
const created = () => response(201, { id: "book-1" });

function createHarness(serverFor) {
  const storage = new Map();
  const context = vm.createContext({
    console: { error() {} },
    JSON,
    window: {
      localStorage: {
        getItem: (key) => (storage.has(key) ? storage.get(key) : null),
        setItem: (key, value) => storage.set(key, String(value)),
        removeItem: (key) => storage.delete(key),
      },
      setTimeout: (fn) => setTimeout(fn, 0),
      location: { href: "" },
    },
    fetch: (url, options) => serverFor(url, options),
    toasts: [],
  });
  vm.runInContext(`
    var state = { audiobooks: { upload: null }, auth: {} };
    var audiobookUploadStatus = { textContent: "" };
    var audiobookUploadCancel = null;
    var audiobookUploadProgressWrap = null;
    var audiobookUploadTitleInput = null;
    var audiobookUploadAuthorInput = null;
    var audiobookUploadModal = { close() {} };
    function updateAudiobookUploadProgress() {}
    function renderAudiobookUploadParts() {}
    function setAudiobookUploadPartState() {}
    function renderAudiobookUploadQueue() {}
    function setAudiobookUploadFormBusy() {}
    function renderContentMode() {}
    async function fetchAudiobooks() {}
    async function probeAudioDuration() { return 0; }
    async function expectAuthorized(response) { return response.ok; }
    function showToast(message) { toasts.push(message); }
    ${errorClasses}
  `, context);
  vm.runInContext(uploadSource, context);
  return {
    context,
    run: (code) => vm.runInContext(code, context),
    marker: () => storage.get(vm.runInContext("AUDIOBOOK_UPLOAD_STORAGE_KEY", context)) ?? null,
  };
}

async function uploadOne(harness, file) {
  try {
    await harness.run("uploadOneAudiobookFile")(file, "", "");
    return null;
  } catch (error) {
    return error;
  }
}

test("a duplicate is skipped as before and its resume marker dropped", async () => {
  const file = fakeFile("book.m4b", 3 * MiB);
  const server = fakeUploadServer({
    totalBytes: file.size,
    reply: () => response(409, { error: "an audiobook with this title already exists", code: "duplicate" }),
  });
  const harness = createHarness(server.fetch);
  const error = await uploadOne(harness, file);
  assert.ok(error instanceof harness.run("AudiobookUploadDuplicate"), `got ${error}`);
  assert.equal(harness.marker(), null);
  assert.equal(server.completes, 1);
});

test("a file that is not audio reports the server's reason, not a duplicate", async () => {
  const file = fakeFile("notes.m4b", 3 * MiB);
  const server = fakeUploadServer({
    totalBytes: file.size,
    reply: () => response(409, { error: "that file is not an audio file", code: "not_audio" }),
  });
  const harness = createHarness(server.fetch);
  const error = await uploadOne(harness, file);
  assert.ok(!(error instanceof harness.run("AudiobookUploadDuplicate")), "not_audio was reported as a duplicate");
  assert.ok(error instanceof harness.run("AudiobookUploadRejected"), `got ${error}`);
  assert.equal(error.message, "that file is not an audio file");
  assert.equal(harness.marker(), null, "the server discarded the upload, so the marker must go");
});

test("an incomplete upload re-sends the missing ranges and completes", async () => {
  const file = fakeFile("book.m4b", 20 * MiB);
  const server = fakeUploadServer({
    totalBytes: file.size,
    reply: (complete) => (complete ? created() : incomplete()),
    lose: [8 * MiB], // the second chunk's PATCH "succeeds" but never lands
  });
  const harness = createHarness(server.fetch);
  const error = await uploadOne(harness, file);
  assert.equal(error, null, `upload failed: ${error}`);
  assert.equal(server.completes, 2);
  assert.equal(server.statusChecks, 1, "the client should ask the server which ranges it has");
  assert.deepEqual(server.patches.slice(3), [8 * MiB], "only the missing range should be sent again");
  assert.deepEqual(server.ranges(), [{ start: 0, end: file.size }]);
  assert.equal(harness.marker(), null);
});

test("an upload still incomplete after one retry stops with an error and stays resumable", async () => {
  const file = fakeFile("book.m4b", 20 * MiB);
  const server = fakeUploadServer({
    totalBytes: file.size,
    reply: (complete) => (complete ? created() : incomplete()),
    lose: [8 * MiB, 8 * MiB],
  });
  const harness = createHarness(server.fetch);
  const error = await uploadOne(harness, file);
  assert.ok(error instanceof harness.run("AudiobookUploadIncomplete"), `got ${error}`);
  assert.equal(server.completes, 2, "completion is retried exactly once");
  assert.notEqual(harness.marker(), null, "the upload is still open, so keep the resume marker");
});

test("the queue shows a not-audio file's error and carries on with the next file", async () => {
  const files = [fakeFile("notes.m4b", 1 * MiB), fakeFile("book.m4b", 1 * MiB)];
  let server = null;
  const harness = createHarness((url, options) => {
    if ((options?.method || "GET") === "POST" && url === "/api/audiobooks/uploads") {
      const name = JSON.parse(options.body).filename;
      server = fakeUploadServer({
        totalBytes: 1 * MiB,
        reply: () => (name === "notes.m4b"
          ? response(409, { error: "that file is not an audio file", code: "not_audio" })
          : created()),
      });
    }
    return server.fetch(url, options);
  });
  harness.run("setQueue = (files) => { audiobookUploadQueue = files.map((file) => ({ file, status: 'queued', message: '' })); }")(files);
  await harness.run("submitAudiobookUpload")({ preventDefault() {} });
  const queue = harness.run("audiobookUploadQueue");
  assert.equal(queue[0].status, "error");
  assert.equal(queue[0].message, "that file is not an audio file");
  assert.equal(queue[1].status, "done", "the queue should move on past a file that is not audio");
  assert.equal(harness.run("toasts").length, 1);
  assert.match(harness.run("toasts")[0], /notes\.m4b.*not an audio file/);
});

test("clicking Upload again after a still-incomplete upload retries that file", async () => {
  const file = fakeFile("book.m4b", 20 * MiB);
  const server = fakeUploadServer({
    totalBytes: file.size,
    reply: (complete) => (complete ? created() : incomplete()),
    lose: [8 * MiB, 8 * MiB], // lost on the first send and on the re-send
  });
  const harness = createHarness(server.fetch);
  harness.run("setQueue = (files) => { audiobookUploadQueue = files.map((file) => ({ file, status: 'queued', message: '' })); }")([file]);
  await harness.run("submitAudiobookUpload")({ preventDefault() {} });
  let queue = harness.run("audiobookUploadQueue");
  assert.equal(queue[0].status, "error");
  assert.match(harness.run("audiobookUploadStatus").textContent, /Click Upload to try again/);

  // The message says to click Upload, so that click has to pick the file up
  // again and resume it from the saved marker.
  await harness.run("submitAudiobookUpload")({ preventDefault() {} });
  queue = harness.run("audiobookUploadQueue");
  assert.equal(queue[0].status, "done", `the retry did not upload the file: ${queue[0].message}`);
  assert.equal(server.completes, 3);
  assert.deepEqual(server.ranges(), [{ start: 0, end: file.size }]);
  assert.equal(harness.marker(), null);
});

test("clicking Upload again does not resend a file the server refused", async () => {
  const files = [fakeFile("notes.m4b", 1 * MiB)];
  const server = fakeUploadServer({
    totalBytes: 1 * MiB,
    reply: () => response(409, { error: "that file is not an audio file", code: "not_audio" }),
  });
  const harness = createHarness(server.fetch);
  harness.run("setQueue = (files) => { audiobookUploadQueue = files.map((file) => ({ file, status: 'queued', message: '' })); }")(files);
  await harness.run("submitAudiobookUpload")({ preventDefault() {} });
  await harness.run("submitAudiobookUpload")({ preventDefault() {} });
  assert.equal(server.completes, 1, "a refused file must not be uploaded again");
  assert.equal(harness.run("audiobookUploadQueue")[0].status, "error");
});
