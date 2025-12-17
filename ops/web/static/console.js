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

// Initialize console controller
let consoleController;
document.addEventListener('DOMContentLoaded', () => {
    consoleController = new ConsoleController();
});

// Setup HTMX WebSocket afterSwap handler
document.body.addEventListener('htmx:wsAfterMessage', (event) => {
    if (consoleController) {
        consoleController.onNewLogLine();
    }
});
