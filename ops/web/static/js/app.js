// NETRUNNER - Minimal Console JS
// Following docs: minimal JS only for console controls

// Modal management
function closeModal() {
    const container = document.getElementById('modal-container');
    if (container) {
        container.classList.remove('active');
        container.innerHTML = '';
    }
}

function openModal(html) {
    const container = document.getElementById('modal-container');
    if (container) {
        container.innerHTML = html;
        // Trigger reflow
        container.offsetHeight;
        container.classList.add('active');
    }
}

function openModalFromHTMX(target) {
    const container = document.getElementById('modal-container');
    if (container && target) {
        container.innerHTML = target;
        container.offsetHeight;
        container.classList.add('active');
    }
}

document.addEventListener('DOMContentLoaded', function() {
    // Listen for HTMX modal trigger headers
    document.body.addEventListener('htmx:afterOnLoad', function(evt) {
        const xhr = evt.detail.xhr;
        if (xhr && xhr.getResponseHeader) {
            if (xhr.getResponseHeader('HX-Trigger') === 'openModal') {
                openModalFromHTMX(evt.detail.target);
            }
        }
    });
    
    // Close modal on escape key
    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') {
            closeModal();
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
    const filterBtns = document.querySelectorAll('.filter-btn');
    let autoScroll = true;
    let filter = 'all';
    
    // Filter buttons
    filterBtns.forEach(btn => {
        btn.addEventListener('click', function() {
            filterBtns.forEach(b => b.classList.remove('active'));
            this.classList.add('active');
            filter = this.dataset.filter;
            applyFilter();
        });
    });
    
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
    if (copyBtn) {
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
    if (clearBtn) {
        clearBtn.addEventListener('click', function() {
            consoleLogs.innerHTML = '';
        });
    }
    
    // WebSocket message handler (if using htmx ws)
    document.body.addEventListener('htmx:wsMessage', function(evt) {
        const msg = evt.detail.message;
        if (msg && consoleLogs) {
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
                    headers: {'Content-Type': 'application/json'},
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
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({email: email, password: password})
                });
                if (resp.ok) {
                    var loginResp = await fetch('/api/auth/login', {
                        method: 'POST',
                        headers: {'Content-Type': 'application/json'},
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
