// NetRunner Audio Player — Single master player architecture
// A single hidden <audio> element is controlled by JS so that
// browse-table play buttons and the detail-modal control surface
// never conflict.

(function () {
  'use strict';

  /** @type {HTMLAudioElement|null} */
  let master = document.getElementById('master-player');
  if (!master) return; // page has no player (e.g. landing page)

  let currentTrackId = null;
  let currentTitle = '';
  let currentArtist = '';

  // Expose to inline onclick handlers
  window.NetPlayer = {
    /** Play or toggle a track. Call from onclick attributes. */
    play: function (trackId, title, artist) {
      if (!master) return;

      var url = '/tracks/' + encodeURIComponent(trackId) + '/stream';

      // Same track — just toggle
      if (currentTrackId === trackId) {
        if (master.paused) {
          master.play()['catch'](function (e) { console.warn('Playback:', e.message); });
        } else {
          master.pause();
        }
        return;
      }

      // Switch to new track
      master.src = url;
      currentTrackId = trackId;
      currentTitle = title || '';
      currentArtist = artist || '';

      master.play()['catch'](function (e) { console.warn('Playback:', e.message); });
      updateNowPlaying(currentTrackId, currentTitle, currentArtist);
    },

    /** Toggle play/pause from detail modal controls. */
    toggle: function () {
      if (!master) return;
      if (master.paused) {
        master.play()['catch'](function (e) { console.warn('Playback:', e.message); });
      } else {
        master.pause();
      }
    },

    /** Seek to a percentage of duration. */
    seek: function (pct) {
      if (!master || !master.duration) return;
      master.currentTime = (pct / 100) * master.duration;
    },

    /** Set volume (0-100). */
    volume: function (val) {
      if (!master) return;
      master.volume = Math.max(0, Math.min(1, val / 100));
    },

    /** Return current playback state for UI binding. */
    state: function () {
      if (!master) return { paused: true, currentTime: 0, duration: 0, trackId: null };
      return {
        paused: master.paused,
        currentTime: master.currentTime,
        duration: master.duration,
        trackId: currentTrackId,
        title: currentTitle,
        artist: currentArtist,
      };
    },
  };

  // ── Click delegation for play buttons with data attributes ──
  document.addEventListener('click', function (evt) {
    var btn = evt.target.closest('.np-play-btn[data-track-id]');
    if (!btn) return;
    var trackId = btn.getAttribute('data-track-id');
    var title = btn.getAttribute('data-track-title') || '';
    var artist = btn.getAttribute('data-track-artist') || '';
    window.NetPlayer.play(trackId, title, artist);
  });

  // ── Progress bar: click-to-seek + keyboard seek ──
  document.addEventListener('click', function (evt) {
    var bar = evt.target.closest('.np-progress-bar');
    if (!bar || !master || !master.duration) return;
    var pct = (evt.offsetX / bar.offsetWidth) * 100;
    window.NetPlayer.seek(pct);
  });

  document.addEventListener('keydown', function (evt) {
    var bar = evt.target.closest('.np-progress-bar');
    if (!bar || !master || !master.duration) return;
    var step = 5; // seconds
    var pageStep = 30; // seconds
    if (evt.key === 'ArrowRight' || evt.key === 'ArrowUp') {
      master.currentTime = Math.min(master.duration, master.currentTime + step);
      evt.preventDefault();
    } else if (evt.key === 'ArrowLeft' || evt.key === 'ArrowDown') {
      master.currentTime = Math.max(0, master.currentTime - step);
      evt.preventDefault();
    } else if (evt.key === 'PageUp') {
      master.currentTime = Math.min(master.duration, master.currentTime + pageStep);
      evt.preventDefault();
    } else if (evt.key === 'PageDown') {
      master.currentTime = Math.max(0, master.currentTime - pageStep);
      evt.preventDefault();
    } else if (evt.key === 'Home') {
      master.currentTime = 0;
      evt.preventDefault();
    } else if (evt.key === 'End') {
      master.currentTime = master.duration;
      evt.preventDefault();
    }
  });

  // ── Progress bar updater ──
  // Every 250ms update all progress-bar-fill and time-display elements
  // that are bound to the master player.
  setInterval(function () {
    if (!master || !master.duration) return;
    var pct = (master.currentTime / master.duration) * 100;
    var cur = fmtTime(master.currentTime);
    var tot = fmtTime(master.duration);

    document.querySelectorAll('.np-progress-fill').forEach(function (el) {
      el.style.width = pct + '%';
    });
    document.querySelectorAll('.np-progress-bar').forEach(function (el) {
      el.setAttribute('aria-valuenow', Math.round(pct));
      el.setAttribute('aria-valuetext', cur + ' of ' + tot);
    });
    document.querySelectorAll('.np-current').forEach(function (el) {
      el.textContent = cur;
    });
    document.querySelectorAll('.np-total').forEach(function (el) {
      el.textContent = tot;
    });
    document.querySelectorAll('.np-track-title').forEach(function (el) {
      el.textContent = currentTitle;
    });
    document.querySelectorAll('.np-track-artist').forEach(function (el) {
      el.textContent = currentArtist;
    });
  }, 250);

  // ── HTMX integration ──
  // When the detail modal is loaded via HTMX, look for
  // data-track-id on the controls to bind them to the master player.
  document.addEventListener('htmx:afterSwap', function (evt) {
    // If the swapped content is the track detail modal
    var controls = document.querySelector('.np-controls[data-track-id]');
    if (!controls) return;

    var modalTrackId = controls.getAttribute('data-track-id');
    var playBtn = controls.querySelector('.np-play-btn');
    if (!playBtn) return;

    var title = playBtn.getAttribute('data-track-title') || currentTitle;

    // Update button label based on whether this track is playing
    if (modalTrackId === currentTrackId && !master.paused) {
      playBtn.textContent = '⏸ Pause';
      playBtn.setAttribute('aria-label', 'Pause ' + title);
    } else {
      playBtn.textContent = '▶ Play';
      playBtn.setAttribute('aria-label', 'Play ' + title);
    }
  });

  // Update Play button label on play/pause events
  master.addEventListener('play', function () {
    document.querySelectorAll('.np-play-btn').forEach(function (btn) {
      var ctrl = btn.closest('[data-track-id]');
      if (ctrl && ctrl.getAttribute('data-track-id') === currentTrackId) {
        var title = btn.getAttribute('data-track-title') || currentTitle;
        btn.setAttribute('aria-label', 'Pause ' + title);
        if (btn.classList.contains('btn-sm')) {
          btn.textContent = '⏸';
        } else {
          btn.textContent = '⏸ Pause';
        }
      }
    });
  });

  master.addEventListener('pause', function () {
    document.querySelectorAll('.np-play-btn').forEach(function (btn) {
      var title = btn.getAttribute('data-track-title') || currentTitle;
      btn.setAttribute('aria-label', 'Play ' + title);
      if (btn.classList.contains('btn-sm')) {
        btn.textContent = '▶';
      } else {
        btn.textContent = '▶ Play';
      }
    });
  });

  // ── Helpers ──

  function fmtTime(s) {
    if (isNaN(s) || !isFinite(s)) return '0:00';
    var m = Math.floor(s / 60);
    var sec = Math.floor(s % 60);
    return m + ':' + (sec < 10 ? '0' : '') + sec;
  }

  function updateNowPlaying(trackId, title, artist) {
    // Dispatch a custom event so other widgets can react
    var evt = new CustomEvent('netplayer:change', {
      detail: { trackId: trackId, title: title, artist: artist },
    });
    document.dispatchEvent(evt);
  }
})();
