// Ticks once a second so the audiobook player's playback-sync timers
// (heartbeat, progress save, "playing elsewhere" poll) stay on schedule even
// while the tab is backgrounded. A plain setInterval in the page itself gets
// throttled by the browser once the tab is hidden - Chrome can clamp it to
// roughly once a minute after a few minutes hidden - which is exactly the
// common case of listening with the tab switched away or the phone locked.
// A Worker's own timers are exempt from that page-visibility throttling.
setInterval(() => postMessage(1), 1000);
