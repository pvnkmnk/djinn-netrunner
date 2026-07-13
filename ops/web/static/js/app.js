// NETRUNNER - Minimal Console JS
// Following docs: minimal JS only for console controls

function getCookie(name) {
    const value = "; " + document.cookie;
    const parts = value.split("; " + name + "=");
    if (parts.length === 2) {
        return parts.pop().split(";").shift();
    }
    return "";
}

// Modal management
let lastFocusedElement = null;

function closeModal() {
    const container = document.getElementById('modal-container');
    if (container) {
        container.classList.remove('active');
        container.innerHTML = '';
        // Restore focus to the element that was focused before modal opened
        if (lastFocusedElement) {
            lastFocusedElement.focus();
            lastFocusedElement = null;
        }
    }
}

function openModal(html) {
    const container = document.getElementById('modal-container');
    if (container) {
        // Save the currently focused element before opening modal
        lastFocusedElement = document.activeElement;
        
        // NOTE: DOMParser prevents <script> execution but inline handlers
        // (onerror, onclick, etc.) in the parsed HTML remain active once
        // inserted. Currently unused — HTML source is server-generated and
        // trusted. If refactored to accept user-supplied HTML, add a
        // sanitizer (e.g. DOMPurify) before the replaceChildren call.
        const parser = new DOMParser();
        const doc = parser.parseFromString(html, 'text/html');
        const fragment = document.createDocumentFragment();
        while (doc.body.firstChild) {
            fragment.appendChild(doc.body.firstChild);
        }
        container.replaceChildren(fragment);
        // Trigger reflow
        container.offsetHeight;
        container.classList.add('active');
        
        // Focus the first focusable element inside the modal
        const focusable = container.querySelectorAll(
            'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
        );
        if (focusable.length > 0) {
            focusable[0].focus();
        }
    }
}

function openModalFromHTMX() {
    var container = document.getElementById('modal-container');
    if (container) {
        // HTMX already swapped the modal content into #modal-container with
        // proper bindings. Just show the container — no cloning needed.
        container.offsetHeight; // force reflow for CSS transition
        container.classList.add('active');
    }
}

document.addEventListener('DOMContentLoaded', function() {
    const navToggle = document.getElementById('nav-toggle');
    const primaryNav = document.getElementById('primary-nav');
    if (navToggle && primaryNav) {
        navToggle.addEventListener('click', function() {
            const expanded = navToggle.getAttribute('aria-expanded') === 'true';
            navToggle.setAttribute('aria-expanded', expanded ? 'false' : 'true');
            primaryNav.classList.toggle('nav-open', !expanded);
        });
        
        // Keyboard support for mobile nav toggle
        navToggle.addEventListener('keydown', function(e) {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                const expanded = navToggle.getAttribute('aria-expanded') === 'true';
                navToggle.setAttribute('aria-expanded', expanded ? 'false' : 'true');
                primaryNav.classList.toggle('nav-open', !expanded);
            }
        });
    }

    // Add CSRF token to all HTMX requests.
    document.body.addEventListener('htmx:configRequest', function(evt) {
        const csrfToken = getCookie('csrf_');
        if (csrfToken) {
            evt.detail.headers['X-CSRF-Token'] = csrfToken;
        }
    });

    // Listen for HTMX modal trigger headers
    document.body.addEventListener('htmx:afterOnLoad', function(evt) {
        const xhr = evt.detail.xhr;
        if (xhr && xhr.getResponseHeader) {
            if (xhr.getResponseHeader('HX-Trigger') === 'openModal') {
                openModalFromHTMX();
            }
        }
    });
    
    // Process HTMX attributes in modal content after swap
    document.body.addEventListener('htmx:afterSettle', function(evt) {
        const container = document.getElementById('modal-container');
        if (container && (container === evt.detail.target || container.contains(evt.detail.target))) {
            htmx.process(container);
        }
    });
    
    // Listen for HTMX closeModal trigger (fired via HX-Trigger response header)
    document.body.addEventListener('closeModal', function() {
        closeModal();
    });

    // Global keyboard handler: Escape closes modal or mobile nav
    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') {
            const container = document.getElementById('modal-container');
            if (container && container.classList.contains('active')) {
                closeModal();
                return;
            }
            if (primaryNav && primaryNav.classList.contains('nav-open')) {
                primaryNav.classList.remove('nav-open');
                if (navToggle) {
                    navToggle.setAttribute('aria-expanded', 'false');
                    navToggle.focus();
                }
            }
        }
        // Trap focus inside modal when open
        if (e.key === 'Tab') {
            const container = document.getElementById('modal-container');
            if (!container || !container.classList.contains('active')) return;
            const focusable = container.querySelectorAll(
                'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
            );
            if (focusable.length === 0) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (!container.contains(document.activeElement)) {
                e.preventDefault();
                if (e.shiftKey) { last.focus(); } else { first.focus(); }
            } else if (e.shiftKey && document.activeElement === first) {
                e.preventDefault();
                last.focus();
            } else if (!e.shiftKey && document.activeElement === last) {
                e.preventDefault();
                first.focus();
            }
        }
    });
    
    // Delegate click events for modal closing
    document.addEventListener('click', function(e) {
        const container = document.getElementById('modal-container');
        if (!container || !container.classList.contains('active')) return;
        
        // Close on overlay click
        if (e.target.classList.contains('modal-overlay')) {
            closeModal();
        }
        // Close on close button click
        if (e.target.matches('[data-close-modal]')) {
            closeModal();
        }
    });
    
    const consoleLogs = document.getElementById('console-logs');
    const hasConsolePage = Boolean(consoleLogs);
    const filterBtns = document.querySelectorAll('.filter-btn');
    let autoScroll = true;
    let filter = 'all';
    
    // Filter buttons
    if (hasConsolePage) {
        filterBtns.forEach(btn => {
            btn.addEventListener('click', function() {
                filterBtns.forEach(b => b.classList.remove('active'));
                this.classList.add('active');
                filter = this.dataset.filter;
                applyFilter();
            });
        });
    }
    
    function applyFilter() {
        const lines = consoleLogs.querySelectorAll('.log-line');
        lines.forEach(line => {
            if (filter === 'all') {
                line.style.display = '';
            } else {
                const level = line.querySelector('.log-level');
                // Use exact match for robust filtering
                if (level && level.textContent.trim() === '[' + filter + ']') {
                    line.style.display = '';
                } else {
                    line.style.display = 'none';
                }
            }
        });
    }
    
    // Auto-scroll
    if (consoleLogs) {
        consoleLogs.addEventListener('scroll', function() {
            const atBottom = this.scrollHeight - this.scrollTop <= this.clientHeight + 50;
            autoScroll = atBottom;
        });
    }
    
    // Resume Live button
    const resumeBtn = document.getElementById('btn-resume');
    if (resumeBtn) {
        resumeBtn.addEventListener('click', function() {
            autoScroll = true;
            if (consoleLogs) {
                consoleLogs.scrollTop = consoleLogs.scrollHeight;
            }
        });
    }
    
    // Copy Last 200 button
    const copyBtn = document.getElementById('btn-copy');
    const statusAnnouncer = document.getElementById('status-announcer');
    if (copyBtn && hasConsolePage) {
        copyBtn.addEventListener('click', function() {
            const lines = Array.from(consoleLogs.querySelectorAll('.log-line'))
                .slice(-200)
                .map(line => line.textContent)
                .join('\n');
            navigator.clipboard.writeText(lines).then(() => {
                copyBtn.textContent = 'Copied!';
                if (statusAnnouncer) {
                    statusAnnouncer.textContent = 'Copied logs to clipboard';
                    // Clear after delay so next announcement can be heard
                    setTimeout(() => statusAnnouncer.textContent = '', 3000);
                }
                setTimeout(() => copyBtn.textContent = 'Copy Last 200', 2000);
            });
        });
    }
    
    // Clear button
    const clearBtn = document.getElementById('btn-clear');
    if (clearBtn && hasConsolePage) {
        clearBtn.addEventListener('click', function() {
            if (consoleLogs) {
                consoleLogs.innerHTML = '';
            }
        });
    }
    
    // WebSocket message handler (if using htmx ws)
    document.body.addEventListener('htmx:wsMessage', function(evt) {
        const msg = evt.detail.message;
        if (msg && hasConsolePage && consoleLogs) {
            // Use DOMParser to safely parse HTML and prevent XSS
            const parser = new DOMParser();
            const doc = parser.parseFromString(msg, 'text/html');
            const fragment = document.createDocumentFragment();
            while (doc.body.firstChild) {
                fragment.appendChild(doc.body.firstChild);
            }
            consoleLogs.appendChild(fragment);
            if (autoScroll) {
                consoleLogs.scrollTop = consoleLogs.scrollHeight;
            }
        }
    });

    // Apply cover art backgrounds from data-cover attributes
    function applyCoverArt() {
        document.querySelectorAll('.track-cover[data-cover]').forEach(function(el) {
            el.style.backgroundImage = 'url(' + el.dataset.cover + ')';
        });
    }
    applyCoverArt();
    // Re-apply after HTMX swaps in new content
    document.body.addEventListener('htmx:afterSettle', applyCoverArt);

    // Watchlist: sp_dc cookie form handler
    function initSpdcForm() {
        var form = document.getElementById('spdc-form');
        if (!form || form.dataset.bound) return;
        form.dataset.bound = 'true';
        form.addEventListener('submit', async function(e) {
            e.preventDefault();
            var input = document.getElementById('spdc_cookie');
            var status = document.getElementById('spdc-status');
            if (!input.value.trim()) { status.textContent = 'Cookie value is required'; return; }
            try {
                var resp = await fetch('/api/auth/spotify/spdc', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json', 'X-CSRF-Token': getCookie('csrf_')},
                    body: JSON.stringify({sp_dc: input.value.trim()})
                });
                var data = await resp.json();
                if (resp.ok) {
                    status.textContent = 'Linked successfully';
                    status.className = 'form-status status-ok';
                    input.value = '';
                } else {
                    status.textContent = data.error || 'Failed to save';
                    status.className = 'form-status status-err';
                }
            } catch(err) {
                status.textContent = 'Connection error';
                status.className = 'form-status status-err';
            }
        });
    }
    initSpdcForm();

    // Watchlist: dynamic source-type hints
    function updateSourceHint() {
        var sel = document.getElementById('source_type');
        var uri = document.getElementById('source_uri');
        var hint = document.getElementById('source_hint');
        if (!sel || !uri || !hint) return;
        var hints = {
            'spotify_playlist': ['https://open.spotify.com/playlist/... or spotify:playlist:...', 'Spotify playlist URL or URI'],
            'spotify_liked': ['liked', 'Enter "liked" \u2014 requires sp_dc cookie'],
            'spotify_discover': ['Discover Weekly', 'Playlist name, e.g. "Discover Weekly", "Daily Mix 1"'],
            'lastfm_loved': ['your-username', 'Last.fm username'],
            'lastfm_top': ['your-username', 'Last.fm username'],
            'listenbrainz_listens': ['your-username', 'ListenBrainz username'],
            'discogs_wantlist': ['your-username', 'Discogs username'],
            'lidarr_wanted': ['wanted', 'Enter "wanted" \u2014 pulls missing albums from Lidarr'],
            'rss_feed': ['https://example.com/feed.xml', 'RSS/Atom feed URL'],
            'local_file': ['/path/to/tracks.txt', 'Path to a text file (one "Artist - Title" per line)'],
            'local_directory': ['/path/to/music/', 'Path to a directory of audio files']
        };
        var h = hints[sel.value];
        if (h) {
            uri.placeholder = h[0];
            hint.textContent = h[1];
        } else {
            uri.placeholder = '';
            hint.textContent = '';
        }
    }

    function initWatchlistForm() {
        var sel = document.getElementById('source_type');
        if (!sel || sel.dataset.bound) return;
        sel.dataset.bound = 'true';
        sel.addEventListener('change', updateSourceHint);
        updateSourceHint();
    }
    initWatchlistForm();

    // Re-init watchlist widgets after HTMX swaps in new content
    document.body.addEventListener('htmx:afterSettle', function() {
        initSpdcForm();
        initWatchlistForm();
    });

    // Login/Register form handlers (only on login page)
    var showRegister = document.getElementById('show-register');
    if (showRegister) {
        showRegister.addEventListener('click', function() {
            document.getElementById('login-card').classList.add('hidden');
            document.getElementById('register-card').classList.remove('hidden');
        });
    }
    var showLogin = document.getElementById('show-login');
    if (showLogin) {
        showLogin.addEventListener('click', function() {
            document.getElementById('register-card').classList.add('hidden');
            document.getElementById('login-card').classList.remove('hidden');
        });
    }
    var loginForm = document.getElementById('login-form');
    if (loginForm) {
        loginForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            var email = document.getElementById('email').value;
            var password = document.getElementById('password').value;
            var errorDiv = document.getElementById('login-error');
            try {
                var resp = await fetch('/api/auth/login', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-CSRF-Token': getCookie('csrf_')
                    },
                    body: JSON.stringify({email: email, password: password})
                });
                if (resp.ok) {
                    window.location.href = '/';
                } else {
                    var data = await resp.json();
                    errorDiv.textContent = data.error || 'Login failed';
                    errorDiv.classList.remove('hidden');
                }
            } catch(err) {
                errorDiv.textContent = 'Connection error';
                errorDiv.classList.remove('hidden');
            }
        });
    }
    var registerForm = document.getElementById('register-form');
    if (registerForm) {
        registerForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            var email = document.getElementById('reg-email').value;
            var password = document.getElementById('reg-password').value;
            var errorDiv = document.getElementById('register-error');
            try {
                var resp = await fetch('/api/auth/register', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-CSRF-Token': getCookie('csrf_')
                    },
                    body: JSON.stringify({email: email, password: password})
                });
                if (resp.ok) {
                    var loginResp = await fetch('/api/auth/login', {
                        method: 'POST',
                        headers: {
                            'Content-Type': 'application/json',
                            'X-CSRF-Token': getCookie('csrf_')
                        },
                        body: JSON.stringify({email: email, password: password})
                    });
                    if (loginResp.ok) {
                        window.location.href = '/';
                    }
                } else {
                    var data = await resp.json();
                    errorDiv.textContent = data.error || 'Registration failed';
                    errorDiv.classList.remove('hidden');
                }
            } catch(err) {
                errorDiv.textContent = 'Connection error';
                errorDiv.classList.remove('hidden');
            }
        });
    }
});
