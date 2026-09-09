// Service worker: app shell offline. Static files cache-first; pages
// network-first with a cached fallback so the player and start screen open
// with no connection. Bump VERSION on any static change.
var VERSION = 'fitlog-v1';
var SHELL = ['/', '/start', '/static/app.css', '/static/app.js', '/static/player.js', '/static/icon-192.png', '/manifest.webmanifest'];

self.addEventListener('install', function (e) {
  e.waitUntil(caches.open(VERSION).then(function (c) { return c.addAll(SHELL); }).then(function () { return self.skipWaiting(); }));
});
self.addEventListener('activate', function (e) {
  e.waitUntil(caches.keys().then(function (keys) {
    return Promise.all(keys.filter(function (k) { return k !== VERSION; }).map(function (k) { return caches.delete(k); }));
  }).then(function () { return self.clients.claim(); }));
});
self.addEventListener('fetch', function (e) {
  var req = e.request;
  if (req.method !== 'GET') return;
  var url = new URL(req.url);
  if (url.pathname.startsWith('/static/') || url.pathname === '/manifest.webmanifest') {
    e.respondWith(caches.match(req).then(function (hit) { return hit || fetch(req).then(function (res) {
      var copy = res.clone(); caches.open(VERSION).then(function (c) { c.put(req, copy); }); return res; }); }));
    return;
  }
  e.respondWith(fetch(req).then(function (res) {
    if (res.ok && (url.pathname === '/' || url.pathname === '/start' || url.pathname.startsWith('/play/'))) {
      var copy = res.clone(); caches.open(VERSION).then(function (c) { c.put(req, copy); });
    }
    return res;
  }).catch(function () { return caches.match(req); }));
});
