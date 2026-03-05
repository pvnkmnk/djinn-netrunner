// Djinn NETRUNNER Console Controls
// Minimal JS for console filtering, auto-scroll, and controls

class ConsoleController {
    constructor() {
        this.viewport = document.querySelector('.console-viewport');
        this.isScrolledToBottom = true;
        this.autoScrollEnabled = true;
        this.currentFilter = 'all';

        this.init();
    }

    init() {
        // Monitor scroll position
        if (this.viewport) {
            this.viewport.addEventListener('scroll', () => {
                this.checkScrollPosition();
            });
        }

        // Setup filter buttons
        document.querySelectorAll('[data-filter]').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const filter = e.target.dataset.filter;
                this.applyFilter(filter);
            });
        });

        // Setup control buttons
        const resumeBtn = document.getElementById('resume-live');
        if (resumeBtn) {
            resumeBtn.addEventListener('click', () => {
                this.resumeLive();
            });
        }

        const copyBtn = document.getElementById('copy-logs');
        if (copyBtn) {
            copyBtn.addEventListener('click', () => {
                this.copyLastLogs(200);
            });
        }

        const clearBtn = document.getElementById('clear-console');
        if (clearBtn) {
            clearBtn.addEventListener('click', () => {
                this.clearConsole();
            });
        }
    }

    checkScrollPosition() {
        if (!this.viewport) return;

        const threshold = 50;
        const scrolledToBottom =
            this.viewport.scrollHeight - this.viewport.scrollTop - this.viewport.clientHeight < threshold;

        if (scrolledToBottom !== this.isScrolledToBottom) {
            this.isScrolledToBottom = scrolledToBottom;
            this.autoScrollEnabled = scrolledToBottom;
        }
    }

    scrollToBottom() {
        if (this.viewport && this.autoScrollEnabled) {
            this.viewport.scrollTop = this.viewport.scrollHeight;
        }
    }

    resumeLive() {
        this.autoScrollEnabled = true;
        this.scrollToBottom();
    }

    applyFilter(filter) {
        this.currentFilter = filter;

        // Update active button
        document.querySelectorAll('[data-filter]').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.filter === filter);
        });

        // Filter log lines
        const logs = document.querySelectorAll('.log-line');
        logs.forEach(log => {
            if (filter === 'all') {
                log.classList.remove('hidden');
            } else {
                const matches = log.classList.contains(`log-${filter.toLowerCase()}`);
                log.classList.toggle('hidden', !matches);
            }
        });
    }

    copyLastLogs(count) {
        const logs = Array.from(document.querySelectorAll('.log-line:not(.hidden)'))
            .slice(-count)
            .map(log => log.textContent.trim())
            .join('\n');

        navigator.clipboard.writeText(logs).then(() => {
            console.log('Copied last', count, 'lines');
        });
    }

    clearConsole() {
        if (this.viewport) {
            this.viewport.innerHTML = '';
        }
    }

    // Called when new log line is added via WebSocket
    onNewLogLine() {
        this.scrollToBottom();
    }
}

class UIController {
    constructor() {
        this.stats = {};
        this.init();
    }

    init() {
        // Intercept HTMX beforeSwap to animate stat changes
        document.body.addEventListener('htmx:beforeSwap', (event) => {
            if (event.detail.target.classList.contains('stats-grid')) {
                this.captureStats();
            }
        });

        document.body.addEventListener('htmx:afterSwap', (event) => {
            if (event.detail.target.classList.contains('stats-grid')) {
                this.animateStats();
            }
        });
    }

    captureStats() {
        document.querySelectorAll('.stat-value').forEach(el => {
            const label = el.previousElementSibling.textContent;
            this.stats[label] = parseInt(el.textContent) || 0;
        });
    }

    animateStats() {
        document.querySelectorAll('.stat-value').forEach(el => {
            const label = el.previousElementSibling.textContent;
            const newValue = parseInt(el.textContent) || 0;
            const oldValue = this.stats[label] || 0;

            if (newValue !== oldValue) {
                this.countUp(el, oldValue, newValue, 1000);
                el.classList.add('stat-changed');
                setTimeout(() => el.classList.remove('stat-changed'), 1000);
            }
        });
    }

    countUp(el, start, end, duration) {
        let current = start;
        const range = end - start;
        const increment = end > start ? 1 : -1;
        const stepTime = Math.abs(Math.floor(duration / range));
        
        const timer = setInterval(() => {
            current += increment;
            el.textContent = current;
            if (current === end) {
                clearInterval(timer);
            }
        }, stepTime || 10);
    }
}

// Initialize controllers
let consoleController;
let uiController;
document.addEventListener('DOMContentLoaded', () => {
    consoleController = new ConsoleController();
    uiController = new UIController();
});

// Setup HTMX WebSocket afterSwap handler
document.body.addEventListener('htmx:wsAfterMessage', (event) => {
    if (consoleController) {
        consoleController.onNewLogLine();
    }
});
