// Session player. State lives here; the DOM in play.html is the view.
// Timers derive from timestamps so background throttling cannot drift them.
(function () {
  var plan = JSON.parse(document.getElementById('plan').textContent);
  var $ = function (id) { return document.getElementById(id); };
  var el = { intro: $('intro'), work: $('work'), rest: $('rest'), finish: $('finish'), progress: $('progress'), label: $('label'),
             target: $('target'), clock: $('clock'), stepper: $('stepper'), actual: $('actual'), primary: $('primary'),
             stop: $('stop'), restclock: $('restclock'), next: $('next'), skip: $('skip'), total: $('total'), summary: $('summary'),
             scaled: $('scaled'), incomplete: $('incomplete'), save: $('save'), savestatus: $('savestatus') };

  var st = { round: 1, idx: 0, startedAt: null, endedAt: null, actuals: {}, timer: null, timerEnd: null, paused: null, stopped: false, wakeLock: null };

  function item() { return plan.items[st.idx]; }
  function key(r, i) { return r + '-' + plan.items[i].position; }
  function fmt(s) { s = Math.max(0, Math.round(s)); var m = Math.floor(s / 60); return m + ':' + String(s % 60).padStart(2, '0'); }
  function show(section) { [el.intro, el.work, el.rest, el.finish].forEach(function (s) { s.hidden = s !== section; }); window.scrollTo(0, 0); }

  function beep() {
    try {
      var ctx = new (window.AudioContext || window.webkitAudioContext)();
      var o = ctx.createOscillator(), g = ctx.createGain();
      o.connect(g); g.connect(ctx.destination); o.frequency.value = 880; g.gain.value = 0.15;
      o.start(); o.stop(ctx.currentTime + 0.18);
    } catch (e) {}
  }
  function keepAwake() {
    if (navigator.wakeLock && !st.wakeLock) navigator.wakeLock.request('screen').then(function (l) { st.wakeLock = l; }).catch(function () {});
  }

  function stopTimer() { if (st.timer) { clearInterval(st.timer); st.timer = null; } }
  function countdown(seconds, display, onDone) {
    stopTimer();
    st.timerEnd = Date.now() + seconds * 1000;
    st.paused = null;
    display.textContent = fmt(seconds);
    st.timer = setInterval(function () {
      if (st.paused !== null) return;
      var left = (st.timerEnd - Date.now()) / 1000;
      display.textContent = fmt(left);
      if (left <= 0) { stopTimer(); beep(); onDone(); }
    }, 200);
  }

  function showWork() {
    var it = item();
    el.progress.textContent = 'Round ' + st.round + ' of ' + plan.rounds + ' · ' + (st.idx + 1) + ' of ' + plan.items.length;
    el.label.textContent = it.label;
    el.target.textContent = (it.unit === 'secs' ? it.target + ' seconds' : it.target + ' reps') + (it.load_kg ? ' · ' + it.load_kg + ' kg per hand' : '');
    if (it.unit === 'secs') {
      el.clock.hidden = false; el.stepper.hidden = true;
      el.clock.textContent = fmt(it.target);
      el.primary.textContent = 'Start';
      st.mode = 'ready';
    } else {
      el.clock.hidden = true; el.stepper.hidden = false;
      el.actual.textContent = String(it.target);
      el.primary.textContent = 'Done';
      st.mode = 'reps';
    }
    show(el.work);
  }

  function completeItem(actual) {
    st.actuals[key(st.round, st.idx)] = actual;
    var it = item();
    var last = st.idx === plan.items.length - 1 && st.round === plan.rounds;
    if (last) return finish(false);
    if (it.rest > 0) return showRest(it.rest);
    advance();
  }

  function advance() {
    if (st.idx < plan.items.length - 1) { st.idx++; } else { st.idx = 0; st.round++; }
    showWork();
  }

  function showRest(secs) {
    var nextIdx = st.idx < plan.items.length - 1 ? st.idx + 1 : 0;
    el.next.textContent = 'Next: ' + plan.items[nextIdx].label;
    show(el.rest);
    countdown(secs, el.restclock, advance);
  }

  function finish(stopped) {
    stopTimer();
    st.endedAt = Date.now();
    st.stopped = stopped;
    var total = Math.round((st.endedAt - st.startedAt) / 1000);
    el.total.textContent = fmt(total) + ' total';
    el.summary.innerHTML = '';
    var under = false;
    for (var r = 1; r <= plan.rounds; r++) {
      for (var i = 0; i < plan.items.length; i++) {
        var k = key(r, i), a = st.actuals[k];
        if (a === undefined) continue;
        var li = document.createElement('li');
        li.textContent = 'R' + r + ' · ' + plan.items[i].label + ': ' + a + (plan.items[i].unit === 'secs' ? ' s' : '') + ' / ' + plan.items[i].target;
        el.summary.appendChild(li);
        if (a < plan.items[i].target) under = true;
      }
    }
    el.scaled.checked = under;
    el.incomplete.checked = stopped;
    show(el.finish);
    if (st.wakeLock) { st.wakeLock.release().catch(function () {}); st.wakeLock = null; }
  }

  function payload() {
    var items = [];
    for (var r = 1; r <= plan.rounds; r++) {
      for (var i = 0; i < plan.items.length; i++) {
        var a = st.actuals[key(r, i)];
        if (a === undefined) continue;
        var it = plan.items[i];
        items.push({ round: r, position: it.position, label: it.label, unit: it.unit, target: it.target, actual: a, load_kg: it.load_kg || null });
      }
    }
    return {
      protocol_id: plan.protocol_id,
      started_at: new Date(st.startedAt).toISOString(),
      total_secs: Math.round((st.endedAt - st.startedAt) / 1000),
      completed: !el.incomplete.checked,
      scaled: el.scaled.checked,
      items: items
    };
  }

  // ---- wiring ----
  $('begin').addEventListener('click', function () { st.startedAt = Date.now(); keepAwake(); showWork(); });

  el.stepper.addEventListener('click', function (e) {
    var d = e.target.getAttribute('data-delta');
    if (!d) return;
    el.actual.textContent = String(Math.max(0, parseInt(el.actual.textContent, 10) + parseInt(d, 10)));
  });

  el.primary.addEventListener('click', function () {
    var it = item();
    if (st.mode === 'reps') return completeItem(parseInt(el.actual.textContent, 10));
    if (st.mode === 'ready') {
      st.mode = 'running'; el.primary.textContent = 'Pause';
      countdown(it.target, el.clock, function () { completeItem(it.target); });
      return;
    }
    if (st.mode === 'running') { st.paused = Date.now(); st.mode = 'paused'; el.primary.textContent = 'Resume'; return; }
    if (st.mode === 'paused') { st.timerEnd += Date.now() - st.paused; st.paused = null; st.mode = 'running'; el.primary.textContent = 'Pause'; }
  });

  el.stop.addEventListener('click', function () {
    var it = item();
    if (st.mode === 'running' || st.mode === 'paused') {
      var elapsed = it.target - Math.max(0, (st.timerEnd - (st.paused || Date.now())) / 1000);
      st.actuals[key(st.round, st.idx)] = Math.round(elapsed);
    } else if (st.mode === 'reps') {
      var v = parseInt(el.actual.textContent, 10);
      if (v > 0 && v !== it.target) st.actuals[key(st.round, st.idx)] = v;
    }
    finish(true);
  });

  el.skip.addEventListener('click', function () { stopTimer(); advance(); });

  el.save.addEventListener('click', function () {
    var body = payload();
    el.save.disabled = true;
    el.savestatus.textContent = 'Saving…';
    fetch('/api/sessions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
      .then(function (r) { if (!r.ok) throw new Error(r.status); return r.json(); })
      .then(function (j) { window.location.href = j.rate_url; })
      .catch(function () {
        window.fitlog.queue(body);
        el.savestatus.textContent = 'No connection. Saved on this phone; it will sync when you are back online. You can rate it from the home screen then.';
        el.save.textContent = 'Saved offline';
      });
  });
})();
