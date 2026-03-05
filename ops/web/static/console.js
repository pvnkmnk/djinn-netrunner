// Djinn NETRUNNER Console Controls
// Enhanced JS for syntax highlighting, error tracking, and advanced filtering

class ConsoleController {
    constructor() {
        this.viewport = null;
        this.isScrolledToBottom = true;
        this.autoScrollEnabled = true;
        this.currentFilter = 'all';
        this.errorCount = 0;
        this.lastErrorEl = null;

        this.patterns = [
            { regex: /(\/[a-zA-Z0-9._\-\/]+)/g, class: 'log-path' },
            { regex: /(https?:\/\/[^\s]+)/g, class: 'log-url' },
            { regex: /(#[0-9]+|[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})/g, class: 'log-id' }
        ];

        this.init();
    }

    init() {
        // Setup static listeners
        document.querySelectorAll('[data-filter]').forEach(btn => {
            btn.addEventListener('click', (e) => this.applyFilter(e.target.dataset.filter));
        });

        document.getElementById('resume-live')?.addEventListener('click', () => this.resumeLive());
        document.getElementById('clear-console')?.addEventListener('click', () => this.clearConsole());

        // Initial viewport if it exists
        this.updateViewport();
    }

    updateViewport() {
        this.viewport = document.querySelector('.console-viewport');
        if (this.viewport) {
            this.viewport.addEventListener('scroll', () => this.checkScrollPosition());
            this.processExistingLogs();
        }
    }

    processExistingLogs() {
        this.errorCount = 0;
        document.querySelectorAll('.log-line').forEach(el => this.processLogLine(el));
        this.updateErrorIndicator();
    }

    processLogLine(el) {
        if (el.dataset.processed) return;

        // 1. Apply Syntax Highlighting to msg
        const msgEl = el.querySelector('.log-msg');
        if (msgEl) {
            let html = msgEl.textContent;
            this.patterns.forEach(p => {
                html = html.replace(p.regex, `<span class="${p.class}">$1</span>`);
            });
            msgEl.innerHTML = html;
        }

        // 2. Error Tracking
        if (el.classList.contains('log-err')) {
            this.errorCount++;
            this.lastErrorEl = el;
        }

        el.dataset.processed = "true";
        this.scrollToBottom();
    }

    checkScrollPosition() {
        if (!this.viewport) return;
        const threshold = 50;
        const scrolledToBottom = this.viewport.scrollHeight - this.viewport.scrollTop - this.viewport.clientHeight < threshold;
        this.autoScrollEnabled = scrolledToBottom;
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
        document.querySelectorAll('[data-filter]').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.filter === filter);
        });

        const logs = document.querySelectorAll('.log-line');
        logs.forEach(log => {
            if (filter === 'all') {
                log.style.display = 'block';
            } else {
                log.style.display = log.classList.contains(`log-${filter}`) ? 'block' : 'none';
            }
        });
        this.scrollToBottom();
    }

    updateErrorIndicator() {
        const indicator = document.getElementById('error-indicator');
        if (indicator) {
            indicator.classList.toggle('active', this.errorCount > 0);
        }
    }

    jumpToLastError() {
        if (this.lastErrorEl) {
            this.autoScrollEnabled = false;
            this.lastErrorEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
            this.lastErrorEl.style.background = 'rgba(255, 51, 102, 0.2)';
            setTimeout(() => {
                this.lastErrorEl.style.background = '';
            }, 2000);
        }
    }

    clearConsole() {
        if (this.viewport) {
            this.viewport.innerHTML = '';
            this.errorCount = 0;
            this.lastErrorEl = null;
            this.updateErrorIndicator();
        }
    }
}

class UIController {
    constructor() {
        this.stats = {};
        this.init();
    }

    init() {
        document.body.addEventListener('htmx:beforeSwap', (e) => {
            if (e.detail.target.classList.contains('stats-grid')) this.captureStats();
        });
        document.body.addEventListener('htmx:afterSwap', (e) => {
            if (e.detail.target.classList.contains('stats-grid')) this.animateStats();
            if (e.detail.target.id === 'console-socket') consoleController.updateViewport();
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
            if (newValue !== oldValue) this.countUp(el, oldValue, newValue, 800);
        });
    }

    countUp(el, start, end, duration) {
        let current = start;
        const range = end - start;
        if (range === 0) return;
        const increment = end > start ? 1 : -1;
        const stepTime = Math.abs(Math.floor(duration / range)) || 20;
        const timer = setInterval(() => {
            current += increment;
            el.textContent = current;
            if (current === end) clearInterval(timer);
        }, stepTime);
    }
}

let consoleController;
let uiController;
document.addEventListener('DOMContentLoaded', () => {
    consoleController = new ConsoleController();
    uiController = new UIController();
});

document.body.addEventListener('htmx:wsAfterMessage', (e) => {
    if (consoleController && consoleController.viewport) {
        const newLines = consoleController.viewport.querySelectorAll('.log-line:not([data-processed])');
        newLines.forEach(line => consoleController.processLogLine(line));
        consoleController.updateErrorIndicator();
    }
});
