// Source Management UI Controller

let currentEditingSourceId = null;

function showAddSourceModal() {
    currentEditingSourceId = null;
    document.getElementById('modal-title').textContent = 'Add Source';
    document.getElementById('source-id').value = '';
    document.getElementById('source-form').reset();
    document.getElementById('source-modal').classList.add('active');
}

function closeSourceModal() {
    document.getElementById('source-modal').classList.remove('active');
    currentEditingSourceId = null;
}

async function showEditSourceModal(sourceId) {
    currentEditingSourceId = sourceId;
    document.getElementById('modal-title').textContent = 'Edit Source';

    // Fetch source data
    try {
        const response = await fetch(`/api/sources/${sourceId}`);
        if (!response.ok) throw new Error('Failed to fetch source');

        const source = await response.json();

        // Populate form
        document.getElementById('source-id').value = source.id;
        document.getElementById('source-display-name').value = source.display_name;
        document.getElementById('source-type').value = source.source_type;
        document.getElementById('source-uri').value = source.source_uri;
        document.getElementById('source-sync-enabled').checked = source.sync_enabled;

        // Disable URI and type for editing
        document.getElementById('source-type').disabled = true;
        document.getElementById('source-uri').disabled = true;

        document.getElementById('source-modal').classList.add('active');
    } catch (error) {
        showNotification('Error loading source: ' + error.message, 'error');
    }
}

async function submitSourceForm(event) {
    event.preventDefault();

    const formData = new FormData(event.target);
    const sourceId = formData.get('id');

    const data = {
        source_type: formData.get('source_type'),
        source_uri: formData.get('source_uri'),
        display_name: formData.get('display_name'),
        sync_enabled: formData.get('sync_enabled') === 'on'
    };

    try {
        let response;

        if (sourceId) {
            // Update existing source
            response = await fetch(`/api/sources/${sourceId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    display_name: data.display_name,
                    sync_enabled: data.sync_enabled
                })
            });
        } else {
            // Create new source
            response = await fetch('/api/sources', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            });
        }

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.detail || 'Operation failed');
        }

        showNotification(sourceId ? 'Source updated' : 'Source created', 'success');
        closeSourceModal();

        // Reload page to refresh sources list
        setTimeout(() => window.location.reload(), 1000);

    } catch (error) {
        showNotification('Error: ' + error.message, 'error');
    }
}

async function toggleSource(sourceId, enabled) {
    try {
        const response = await fetch(`/api/sources/${sourceId}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ sync_enabled: enabled })
        });

        if (!response.ok) throw new Error('Failed to update source');

        showNotification(enabled ? 'Source enabled' : 'Source disabled', 'success');

        // Reload to refresh UI
        setTimeout(() => window.location.reload(), 1000);

    } catch (error) {
        showNotification('Error: ' + error.message, 'error');
    }
}

async function deleteSource(sourceId) {
    if (!confirm('Are you sure you want to delete this source?')) {
        return;
    }

    try {
        const response = await fetch(`/api/sources/${sourceId}`, {
            method: 'DELETE'
        });

        if (!response.ok) throw new Error('Failed to delete source');

        showNotification('Source deleted', 'success');

        // Remove from UI
        const element = document.getElementById(`source-${sourceId}`);
        if (element) {
            element.remove();
        }

    } catch (error) {
        showNotification('Error: ' + error.message, 'error');
    }
}

function showNotification(message, type = 'info') {
    const notification = document.getElementById('notification');
    notification.textContent = message;
    notification.className = `notification ${type} active`;

    setTimeout(() => {
        notification.classList.remove('active');
    }, 3000);
}

// Close modal on escape key
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        closeSourceModal();
    }
});

// Close modal on background click
document.getElementById('source-modal')?.addEventListener('click', (e) => {
    if (e.target.id === 'source-modal') {
        closeSourceModal();
    }
});

// Watchlist Management

function showAddWatchlistModal() {
    document.getElementById('watchlist-modal-title').textContent = 'Add Automated Watchlist';
    document.getElementById('watchlist-id').value = '';
    document.getElementById('watchlist-form').reset();
    updateWatchlistUriLabel();
    document.getElementById('watchlist-modal').classList.add('active');
}

function closeWatchlistModal() {
    document.getElementById('watchlist-modal').classList.remove('active');
}

function updateWatchlistUriLabel() {
    const type = document.getElementById('watchlist-type').value;
    const uriGroup = document.getElementById('watchlist-uri-group');
    const uriInput = document.getElementById('watchlist-uri');

    if (type === 'spotify_liked') {
        uriGroup.style.display = 'none';
        uriInput.required = false;
        uriInput.value = 'spotify:liked'; // Internal placeholder
    } else {
        uriGroup.style.display = 'block';
        uriInput.required = true;
        if (uriInput.value === 'spotify:liked') uriInput.value = '';
    }
}

async function submitWatchlistForm(event) {
    event.preventDefault();

    const formData = new FormData(event.target);
    const watchlistId = formData.get('id');

    const data = {
        name: formData.get('name'),
        source_type: formData.get('source_type'),
        source_uri: formData.get('source_uri'),
        quality_profile_id: formData.get('quality_profile_id'),
        enabled: formData.get('enabled') === 'on'
    };

    try {
        let response;
        if (watchlistId) {
            response = await fetch(`/api/watchlists/${watchlistId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            });
        } else {
            response = await fetch('/api/watchlists', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            });
        }

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Operation failed');
        }

        showNotification(watchlistId ? 'Watchlist updated' : 'Watchlist created', 'success');
        closeWatchlistModal();
        setTimeout(() => window.location.reload(), 1000);

    } catch (error) {
        showNotification('Error: ' + error.message, 'error');
    }
}

async function toggleWatchlist(id, enabled) {
    try {
        const response = await fetch(`/api/watchlists/${id}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled: enabled })
        });

        if (!response.ok) throw new Error('Failed to update watchlist');

        showNotification(enabled ? 'Watchlist enabled' : 'Watchlist disabled', 'success');
        setTimeout(() => window.location.reload(), 1000);

    } catch (error) {
        showNotification('Error: ' + error.message, 'error');
    }
}

async function deleteWatchlist(id) {
    if (!confirm('Are you sure you want to delete this automated watchlist?')) {
        return;
    }

    try {
        const response = await fetch(`/api/watchlists/${id}`, {
            method: 'DELETE'
        });

        if (!response.ok) throw new Error('Failed to delete watchlist');

        showNotification('Watchlist removed', 'success');
        document.getElementById(`watchlist-${id}`)?.remove();

    } catch (error) {
        showNotification('Error: ' + error.message, 'error');
    }
}

// Close watchlist modal on escape
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        closeWatchlistModal();
    }
});

// Event Handling for real-time updates
document.body.addEventListener('htmx:wsBeforeMessage', (e) => {
    const data = e.detail.message;
    
    // We can parse the HTML if we need to extract specific IDs
    // but usually we just let HTMX handle the swap if it's targeted.
    // For general "system" events that should trigger a reload:
    if (data.includes('watchlist_sync_triggered') || data.includes('job_log')) {
        // Optionally refresh specific elements
        // htmx.trigger('#watchlists-list', 'refresh');
    }
});
