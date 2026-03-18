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

document.addEventListener('DOMContentLoaded', function() {
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
    if (copyBtn) {
        copyBtn.addEventListener('click', function() {
            const lines = Array.from(consoleLogs.querySelectorAll('.log-line'))
                .slice(-200)
                .map(line => line.textContent)
                .join('\n');
            navigator.clipboard.writeText(lines).then(() => {
                copyBtn.textContent = 'Copied!';
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
});
