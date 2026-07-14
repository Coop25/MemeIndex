const CACHE_NAME = "memeindex-public-v1";
const CACHE_PREFIX = "memeindex-public-";
const PUBLIC_ASSETS = [
  "/manifest.webmanifest",
  "/pwa-icons/android-chrome-192x192.png",
  "/pwa-icons/android-chrome-512x512.png",
  "/pwa-icons/apple-touch-icon.png"
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then((cache) => cache.addAll(PUBLIC_ASSETS))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((key) => key.startsWith(CACHE_PREFIX) && key !== CACHE_NAME).map((key) => caches.delete(key))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  if (event.request.method !== "GET") return;
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) return;

  const isPublicInstallAsset = url.pathname === "/manifest.webmanifest" || url.pathname.startsWith("/pwa-icons/");
  if (!isPublicInstallAsset) return;

  event.respondWith(
    caches.match(event.request).then((cached) => cached || fetch(event.request))
  );
});
