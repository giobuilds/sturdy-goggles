// Shared: register the service worker and flush sessions saved while offline.
(function () {
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(function () {});
  }
  var KEY = 'fitlog.pending';
  function pending() {
    try { return JSON.parse(localStorage.getItem(KEY) || '[]'); } catch (e) { return []; }
  }
  function setPending(list) {
    try { localStorage.setItem(KEY, JSON.stringify(list)); } catch (e) {}
    var box = document.getElementById('pending');
    if (!box) return;
    if (list.length) {
      box.textContent = list.length + ' session' + (list.length > 1 ? 's' : '') + ' saved on this phone, waiting to sync.';
      box.hidden = false;
    } else {
      box.hidden = true;
    }
  }
  window.fitlog = {
    queue: function (payload) { var l = pending(); l.push(payload); setPending(l); },
    flush: function () {
      var list = pending();
      if (!list.length) { setPending(list); return Promise.resolve(); }
      var next = list[0];
      return fetch('/api/sessions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(next) })
        .then(function (r) { if (!r.ok) throw new Error(r.status); return r.json(); })
        .then(function () { setPending(list.slice(1)); return window.fitlog.flush(); })
        .catch(function () { setPending(list); });
    }
  };
  setPending(pending());
  if (navigator.onLine) window.fitlog.flush();
  window.addEventListener('online', function () { window.fitlog.flush(); });
})();
