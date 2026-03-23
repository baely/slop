document.addEventListener('DOMContentLoaded', function() {
    const secret = localStorage.getItem('uploadSecret');
    if (secret) {
        document.getElementById('upload-secret').value = secret;
        showApp();
        loadSites();
    }

    // Drag and drop
    const dropZone = document.getElementById('drop-zone');

    dropZone.addEventListener('dragover', function(e) {
        e.preventDefault();
        dropZone.classList.add('drag-over');
    });

    dropZone.addEventListener('dragleave', function(e) {
        e.preventDefault();
        dropZone.classList.remove('drag-over');
    });

    dropZone.addEventListener('drop', function(e) {
        e.preventDefault();
        dropZone.classList.remove('drag-over');
        if (e.dataTransfer.files.length > 0) {
            handleFile(e.dataTransfer.files[0]);
        }
    });

    // Click drop zone to open file picker
    dropZone.addEventListener('click', function(e) {
        if (e.target === dropZone || e.target.closest('.drop-zone-content') && e.target.tagName !== 'LABEL') {
            document.getElementById('file-input').click();
        }
    });

    // Admin auto-login
    const adminSecret = localStorage.getItem('adminSecret');
    if (adminSecret) {
        document.getElementById('admin-secret').value = adminSecret;
        showAdminPanel();
        loadAdminStats();
        loadAdminSites();
    }
});

// Auth

function saveSecret() {
    const secret = document.getElementById('upload-secret').value;
    if (!secret) {
        showError('Please enter an upload secret');
        return;
    }
    localStorage.setItem('uploadSecret', secret);
    showSuccess('Connected');
    showApp();
    loadSites();
}

function showApp() {
    document.getElementById('auth-section').style.display = 'none';
    document.getElementById('app').style.display = 'block';
}

function logout() {
    localStorage.removeItem('uploadSecret');
    document.getElementById('auth-section').style.display = 'block';
    document.getElementById('app').style.display = 'none';
    document.getElementById('upload-secret').value = '';
}

// File handling

function handleFileSelect(event) {
    if (event.target.files[0]) {
        handleFile(event.target.files[0]);
    }
}

async function handleFile(file) {
    const filename = file.name.toLowerCase();
    const isZip = filename.endsWith('.zip');
    const isHtml = filename.endsWith('.html') || filename.endsWith('.htm');

    if (!isZip && !isHtml) {
        showError('Please upload a ZIP or HTML file');
        return;
    }

    if (file.size > 100 * 1024 * 1024) {
        showError('File too large (max 100MB)');
        return;
    }

    const secret = localStorage.getItem('uploadSecret');
    if (!secret) {
        showError('Please connect first');
        return;
    }

    document.getElementById('drop-zone').style.display = 'none';
    document.getElementById('upload-progress').style.display = 'block';

    try {
        const formData = new FormData();

        const subdomain = document.getElementById('custom-subdomain').value.trim();
        if (subdomain) formData.append('subdomain', subdomain);

        const domain = document.getElementById('custom-domain').value.trim();
        if (domain) formData.append('domain', domain);

        const expires = document.getElementById('custom-expires').value.trim();
        if (expires) formData.append('expires', expires);

        formData.append('file', file);

        const response = await fetch('/api/deploy', {
            method: 'POST',
            headers: { 'X-Upload-Secret': secret },
            body: formData
        });

        const data = await response.json();

        if (response.ok) {
            saveAPIKey(data.subdomain, data.api_key);
            showSuccess(`Deployed at <a href="${data.url}" target="_blank">${data.url}</a>`);
            loadSites();
        } else {
            showError(data.error || 'Deploy failed');
        }
    } catch (error) {
        showError('Network error: ' + error.message);
    }

    resetUploadForm();
}

function resetUploadForm() {
    document.getElementById('drop-zone').style.display = 'block';
    document.getElementById('upload-progress').style.display = 'none';
    document.getElementById('file-input').value = '';
}

// Sites

async function loadSites() {
    const secret = localStorage.getItem('uploadSecret');
    if (!secret) return;

    try {
        const response = await fetch('/api/sites', {
            headers: { 'X-Upload-Secret': secret }
        });

        if (response.ok) {
            const data = await response.json();
            displaySites(data.sites);
        }
    } catch (error) {
        console.error('Failed to load sites:', error);
    }
}

function displaySites(sites) {
    const sitesList = document.getElementById('sites-list');
    const noSites = document.getElementById('no-sites');

    if (!sites || sites.length === 0) {
        sitesList.innerHTML = '';
        noSites.style.display = 'block';
        return;
    }

    noSites.style.display = 'none';
    sitesList.innerHTML = sites.map(site => `
        <div class="site-item">
            <div class="site-header">
                <a href="${site.url}" target="_blank" class="site-url">${site.url}</a>
                <div class="site-actions">
                    <label class="listed-toggle" title="${site.listed ? 'Publicly listed' : 'Not listed'}">
                        <input type="checkbox" ${site.listed ? 'checked' : ''} onchange="toggleListed('${site.subdomain}', this.checked)">
                        Listed
                    </label>
                    <button onclick="copySiteURL('${site.url}')" class="btn btn-ghost btn-small">Copy</button>
                    <button onclick="deleteSite('${site.subdomain}')" class="btn btn-danger btn-small">Delete</button>
                </div>
            </div>
            <div class="site-editable">
                <input type="text" class="inline-edit" placeholder="Title" value="${escapeAttr(site.title || '')}" onchange="updateSiteMeta('${site.subdomain}', 'title', this.value)">
                <input type="text" class="inline-edit" placeholder="Description" value="${escapeAttr(site.description || '')}" onchange="updateSiteMeta('${site.subdomain}', 'description', this.value)">
            </div>
            <div class="site-meta">
                <span>${site.file_count} files</span>
                <span>${formatBytes(site.size_bytes)}</span>
                <span>${formatDate(site.created_at)}</span>
                ${site.custom_domain ? `<span>${site.custom_domain}</span>` : ''}
                ${site.expires_at ? `<span>Expires ${formatDate(site.expires_at)}</span>` : ''}
            </div>
        </div>
    `).join('');
}

function copySiteURL(url) {
    navigator.clipboard.writeText(url).then(() => {
        showSuccess('Copied');
    }).catch(() => {
        showError('Failed to copy');
    });
}

function escapeAttr(str) {
    return str.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/'/g, '&#39;').replace(/</g, '&lt;');
}

async function patchSite(subdomain, data, authHeader) {
    const response = await fetch(`/api/sites/${subdomain}`, {
        method: 'PATCH',
        headers: {
            'Content-Type': 'application/json',
            ...authHeader
        },
        body: JSON.stringify(data)
    });
    return response;
}

async function toggleListed(subdomain, listed) {
    const secret = localStorage.getItem('uploadSecret');
    if (!secret) { showError('Please connect first'); return; }

    try {
        const response = await patchSite(subdomain, { listed }, { 'X-Upload-Secret': secret });
        if (response.ok) {
            showSuccess(listed ? 'Publicly listed' : 'Unlisted');
        } else {
            showError('Failed to update');
            loadSites();
        }
    } catch (error) {
        showError('Network error: ' + error.message);
        loadSites();
    }
}

async function updateSiteMeta(subdomain, field, value) {
    const secret = localStorage.getItem('uploadSecret');
    if (!secret) { showError('Please connect first'); return; }

    try {
        const response = await patchSite(subdomain, { [field]: value }, { 'X-Upload-Secret': secret });
        if (response.ok) {
            showSuccess('Updated');
        } else {
            showError('Failed to update');
        }
    } catch (error) {
        showError('Network error: ' + error.message);
    }
}

async function deleteSite(subdomain) {
    if (!confirm(`Delete ${subdomain}? This cannot be undone.`)) return;

    const apiKey = getAPIKey(subdomain);
    if (!apiKey) {
        showError('API key not found for this site');
        return;
    }

    try {
        const response = await fetch(`/api/sites/${subdomain}`, {
            method: 'DELETE',
            headers: { 'X-API-Key': apiKey }
        });

        if (response.ok) {
            showSuccess('Deleted');
            removeAPIKey(subdomain);
            loadSites();
        } else {
            showError('Failed to delete');
        }
    } catch (error) {
        showError('Network error: ' + error.message);
    }
}

// API key storage

function saveAPIKey(subdomain, apiKey) {
    const keys = JSON.parse(localStorage.getItem('apiKeys') || '{}');
    keys[subdomain] = apiKey;
    localStorage.setItem('apiKeys', JSON.stringify(keys));
}

function getAPIKey(subdomain) {
    const keys = JSON.parse(localStorage.getItem('apiKeys') || '{}');
    return keys[subdomain];
}

function removeAPIKey(subdomain) {
    const keys = JSON.parse(localStorage.getItem('apiKeys') || '{}');
    delete keys[subdomain];
    localStorage.setItem('apiKeys', JSON.stringify(keys));
}

// Utilities

function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}

function formatDate(dateString) {
    const date = new Date(dateString);
    const now = new Date();
    const diff = now - date;
    const days = Math.floor(diff / 86400000);

    if (days === 0) return 'today';
    if (days === 1) return 'yesterday';
    if (days < 30) return `${days}d ago`;
    return date.toLocaleDateString();
}

// Toast notifications

function showSuccess(message) {
    showToast(message, 'success');
}

function showError(message) {
    showToast(message, 'error');
}

function showToast(message, type) {
    const container = document.getElementById('toast-container');
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.innerHTML = `
        <span>${message}</span>
        <button class="toast-dismiss" onclick="dismissToast(this)">&times;</button>
    `;
    container.appendChild(toast);

    setTimeout(() => dismissToast(toast.querySelector('.toast-dismiss')), 4000);
}

function dismissToast(btn) {
    const toast = btn.closest ? btn.closest('.toast') : btn.parentElement;
    if (!toast || toast.classList.contains('toast-out')) return;
    toast.classList.add('toast-out');
    setTimeout(() => toast.remove(), 200);
}

// ============== ADMIN ==============

function saveAdminSecret() {
    const secret = document.getElementById('admin-secret').value;
    if (!secret) {
        showError('Please enter an admin secret');
        return;
    }

    fetch('/api/admin/stats', {
        headers: { 'X-Admin-Secret': secret }
    }).then(response => {
        if (response.ok) {
            localStorage.setItem('adminSecret', secret);
            showSuccess('Admin logged in');
            showAdminPanel();
            loadAdminStats();
            loadAdminSites();
        } else {
            showError('Invalid admin secret');
        }
    }).catch(error => {
        showError('Network error: ' + error.message);
    });
}

function showAdminPanel() {
    document.getElementById('admin-auth').style.display = 'none';
    document.getElementById('admin-panel').style.display = 'block';
}

async function loadAdminStats() {
    const secret = localStorage.getItem('adminSecret');
    if (!secret) return;

    try {
        const response = await fetch('/api/admin/stats', {
            headers: { 'X-Admin-Secret': secret }
        });

        if (response.ok) {
            const data = await response.json();
            displayAdminStats(data);
        }
    } catch (error) {
        console.error('Failed to load admin stats:', error);
    }
}

function displayAdminStats(stats) {
    document.getElementById('admin-stats').innerHTML = `
        <div class="stats-grid">
            <div class="stat-item">
                <span class="stat-value">${stats.total_sites}</span>
                <span class="stat-label">Sites</span>
            </div>
            <div class="stat-item">
                <span class="stat-value">${stats.total_files}</span>
                <span class="stat-label">Files</span>
            </div>
            <div class="stat-item">
                <span class="stat-value">${formatBytes(stats.total_size_bytes)}</span>
                <span class="stat-label">Storage</span>
            </div>
        </div>
    `;
}

async function loadAdminSites() {
    const secret = localStorage.getItem('adminSecret');
    if (!secret) return;

    try {
        const response = await fetch('/api/admin/sites', {
            headers: { 'X-Admin-Secret': secret }
        });

        if (response.ok) {
            const data = await response.json();
            displayAdminSites(data.sites);
        }
    } catch (error) {
        console.error('Failed to load admin sites:', error);
    }
}

function displayAdminSites(sites) {
    const container = document.getElementById('admin-sites-list');

    if (!sites || sites.length === 0) {
        container.innerHTML = '<p class="empty-state">No sites</p>';
        return;
    }

    container.innerHTML = sites.map(site => `
        <div class="site-item">
            <div class="site-header">
                <a href="${site.url}" target="_blank" class="site-url">${site.url}</a>
                <div class="site-actions">
                    <label class="listed-toggle" title="${site.listed ? 'Publicly listed' : 'Not listed'}">
                        <input type="checkbox" ${site.listed ? 'checked' : ''} onchange="adminToggleListed('${site.subdomain}', this.checked)">
                        Listed
                    </label>
                    <button onclick="adminDeleteSite('${site.subdomain}')" class="btn btn-danger btn-small">Delete</button>
                </div>
            </div>
            <div class="site-editable">
                <input type="text" class="inline-edit" placeholder="Title" value="${escapeAttr(site.title || '')}" onchange="adminUpdateMeta('${site.subdomain}', 'title', this.value)">
                <input type="text" class="inline-edit" placeholder="Description" value="${escapeAttr(site.description || '')}" onchange="adminUpdateMeta('${site.subdomain}', 'description', this.value)">
            </div>
            <div class="site-meta">
                <span>${site.file_count} files</span>
                <span>${formatBytes(site.size_bytes)}</span>
                <span>${formatDate(site.created_at)}</span>
                ${site.custom_domain ? `<span>${site.custom_domain}</span>` : ''}
                ${site.expires_at ? `<span>Expires ${formatDate(site.expires_at)}</span>` : ''}
            </div>
        </div>
    `).join('');
}

async function adminToggleListed(subdomain, listed) {
    const secret = localStorage.getItem('adminSecret');
    if (!secret) { showError('Admin secret not found'); return; }

    try {
        const response = await patchSite(subdomain, { listed }, { 'X-Admin-Secret': secret });
        if (response.ok) {
            showSuccess(listed ? 'Publicly listed' : 'Unlisted');
        } else {
            showError('Failed to update');
            loadAdminSites();
        }
    } catch (error) {
        showError('Network error: ' + error.message);
        loadAdminSites();
    }
}

async function adminUpdateMeta(subdomain, field, value) {
    const secret = localStorage.getItem('adminSecret');
    if (!secret) { showError('Admin secret not found'); return; }

    try {
        const response = await patchSite(subdomain, { [field]: value }, { 'X-Admin-Secret': secret });
        if (response.ok) {
            showSuccess('Updated');
        } else {
            showError('Failed to update');
        }
    } catch (error) {
        showError('Network error: ' + error.message);
    }
}

async function adminDeleteSite(subdomain) {
    if (!confirm(`Delete ${subdomain}? This cannot be undone.`)) return;

    const secret = localStorage.getItem('adminSecret');
    if (!secret) {
        showError('Admin secret not found');
        return;
    }

    try {
        const response = await fetch(`/api/admin/sites/${subdomain}`, {
            method: 'DELETE',
            headers: { 'X-Admin-Secret': secret }
        });

        if (response.ok) {
            showSuccess('Deleted');
            loadAdminStats();
            loadAdminSites();
            loadSites();
        } else {
            showError('Failed to delete');
        }
    } catch (error) {
        showError('Network error: ' + error.message);
    }
}
