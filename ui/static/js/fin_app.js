// fin_app.js — application-wide helpers, sync, theme, and modal logic.
// Loaded with `defer`, so this runs after the HTML is parsed but before
// DOMContentLoaded fires.

// ============================================================
// Fin API helpers
// ============================================================
var finApi = {
    postJSON: async function(url, data) {
        return fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Fin-Request': '1' },
            body: JSON.stringify(data),
        });
    }
};

// ============================================================
// UI helpers
// ============================================================
var finUI = {
    escapeHtml: function(str) {
        if (str == null) return '';
        return String(str)
            .replace(/&/g,'&amp;')
            .replace(/</g,'&lt;')
            .replace(/>/g,'&gt;')
            .replace(/"/g,'&quot;');
    }
};

// ============================================================
// Toast notifications
// ============================================================
function showToast(msg) {
    var toast = document.getElementById('toast');
    if (!toast) return;
    toast.textContent = msg;
    toast.classList.add('visible');
    setTimeout(function() { toast.classList.remove('visible'); }, 3000);
}

function showToastWithAction(msg, actionLabel, actionFn) {
    var toast = document.getElementById('toast');
    if (!toast) return;
    toast.innerHTML = finUI.escapeHtml(msg) +
        ' <button onclick="(' + actionFn.toString() + ')()" style="margin-left:8px;background:none;border:none;color:inherit;text-decoration:underline;cursor:pointer;">' +
        finUI.escapeHtml(actionLabel) + '</button>';
    toast.classList.add('visible');
    setTimeout(function() { toast.classList.remove('visible'); }, 6000);
}

// ============================================================
// Drilldown stub (replaced by fin_drilldown.js when available)
// ============================================================
if (typeof finDrilldown === 'undefined') {
    window.finDrilldown = {
        open: function(s) { console.warn('Drilldown not available for scope:', s); },
        close: function() {}
    };
}

// ============================================================
// Sync status
// ============================================================
async function loadSyncStatus() {
    var statusEl = document.getElementById('syncStatus');
    if (!statusEl) return;

    try {
        var response = await fetch('/api/sync-status');
        var data = await response.json();

        if (!data.has_synced) {
            statusEl.innerHTML = '<span style="color: var(--accent-yellow);">Never synced</span>';
            return;
        }

        var lastSync = new Date(data.last_sync.timestamp);
        var now = new Date();
        var diffHours = Math.floor((now - lastSync) / (1000 * 60 * 60));
        var diffDays = Math.floor(diffHours / 24);

        var timeAgo;
        if (diffDays > 0) {
            timeAgo = diffDays + 'd ago';
        } else if (diffHours > 0) {
            timeAgo = diffHours + 'h ago';
        } else {
            timeAgo = 'Just now';
        }

        var dataInfo = '';
        if (data.data_range) {
            var formatDate = function(iso) {
                var d = new Date(iso);
                var months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
                return months[d.getMonth()] + " '" + String(d.getFullYear()).slice(2);
            };
            dataInfo = '<br><span class="sync-range">Data: ' + finUI.escapeHtml(formatDate(data.data_range.earliest)) + ' – ' + finUI.escapeHtml(formatDate(data.data_range.latest)) + '</span>';
        }

        statusEl.innerHTML = '<span class="sync-time">Synced: ' + finUI.escapeHtml(timeAgo) + '</span>' + dataInfo;
    } catch (err) {
        statusEl.innerHTML = '';
    }
}

document.addEventListener('DOMContentLoaded', loadSyncStatus);

// ============================================================
// Sync trigger
// ============================================================
async function triggerSync() {
    var btn = document.getElementById('syncBtn');
    var textEl = btn.querySelector('.sync-text');
    var iconEl = btn.querySelector('.sync-icon');
    var originalText = textEl.textContent;

    btn.classList.add('syncing');
    btn.classList.remove('success', 'error');
    textEl.textContent = 'Syncing...';

    try {
        var response = await finApi.postJSON('/api/sync', {});
        var data = await response.json();

        btn.classList.remove('syncing');

        if (response.ok) {
            btn.classList.add('success');
            iconEl.textContent = '✓';
            textEl.textContent = '+' + data.inserted + ' new';

            if (data.inserted > 0 || data.updated > 0) {
                showToastWithAction(
                    'Synced: ' + data.fetched + ' from bank, ' + data.inserted + ' new, ' + data.updated + ' updated',
                    'Refresh',
                    function() { window.location.reload(); }
                );
            } else {
                showToast('Synced: ' + data.fetched + ' from bank, no new transactions');
            }

            var statusEl = document.getElementById('syncStatus');
            if (statusEl) {
                var existingRange = statusEl.querySelector('.sync-range');
                var rangeHtml = existingRange ? '<br>' + existingRange.outerHTML : '';
                statusEl.innerHTML = '<span class="sync-time">Synced: Just now</span>' + rangeHtml;
            }
            setTimeout(loadSyncStatus, 1500);
        } else {
            btn.classList.add('error');
            iconEl.textContent = '✗';
            textEl.textContent = 'Error';
            showToast('Sync failed: ' + (data.error || 'Unknown error'));
        }
    } catch (err) {
        btn.classList.remove('syncing');
        btn.classList.add('error');
        var iconEl2 = btn.querySelector('.sync-icon');
        iconEl2.textContent = '✗';
        textEl.textContent = 'Error';
        showToast('Sync failed: ' + err.message);
    }

    setTimeout(function() {
        btn.classList.remove('success', 'error');
        textEl.textContent = originalText;
        btn.querySelector('.sync-icon').textContent = '↻';
    }, 5000);
}

// ============================================================
// Theme toggle
// ============================================================
function toggleTheme() {
    var current = document.body.getAttribute('data-theme');
    var next = current === 'dark' ? 'light' : 'dark';
    if (next === 'dark') {
        document.body.setAttribute('data-theme', 'dark');
        localStorage.setItem('theme', 'dark');
    } else {
        document.body.removeAttribute('data-theme');
        localStorage.setItem('theme', 'light');
    }
}

// ============================================================
// Alert actions
// ============================================================
async function alertAction(alertKey, action) {
    try {
        var response = await finApi.postJSON('/api/alert-action', { alert_key: alertKey, action: action });
        if (response.ok) {
            var alertEl = document.querySelector('[data-alert-key="' + alertKey + '"]');
            if (alertEl) {
                if (action === 'ack') {
                    alertEl.remove();
                } else {
                    alertEl.classList.add('actioned');
                }
            }
            showToast(action === 'ack' ? 'Dismissed' : 'Marked as ' + action);
        }
    } catch (err) {
        showToast('Error: ' + err.message);
    }
}

// ============================================================
// Category override modal
// ============================================================
var categoryModalMerchant = null;
var categoryModalCurrentCategory = null;
var categoriesCache = null;

function openCategoryModalFromButton(buttonEl, currentCategory) {
    var merchantEl = buttonEl.closest('.drilldown-merchant');
    var merchant = merchantEl.dataset.merchantName;
    openCategoryModal(merchant, currentCategory);
}

async function openCategoryModal(merchant, currentCategory) {
    categoryModalMerchant = merchant;
    categoryModalCurrentCategory = currentCategory;

    document.getElementById('categoryModalMerchant').textContent = 'Merchant: ' + merchant;
    var modal = document.getElementById('categoryModal');
    var listEl = document.getElementById('categoryModalList');

    if (!categoriesCache) {
        try {
            var resp = await fetch('/api/categories');
            var data = await resp.json();
            categoriesCache = data;
        } catch (err) {
            showToast('Error loading categories');
            return;
        }
    }

    listEl.innerHTML = categoriesCache.map(function(cat) {
        return '<div class="category-option' + (cat.id === currentCategory ? ' selected' : '') +
            '" onclick="selectCategory(\'' + finUI.escapeHtml(cat.id) + '\')">' +
            '<span class="category-option-icon">' + finUI.escapeHtml(cat.icon) + '</span>' +
            '<span class="category-option-name">' + finUI.escapeHtml(cat.name) + '</span>' +
            '</div>';
    }).join('');

    modal.style.display = 'flex';
}

function closeCategoryModal() {
    document.getElementById('categoryModal').style.display = 'none';
    categoryModalMerchant = null;
    categoryModalCurrentCategory = null;
}

async function selectCategory(categoryId) {
    if (!categoryModalMerchant) return;
    try {
        var response = await finApi.postJSON('/api/category-override', {
            merchant: categoryModalMerchant,
            category_id: categoryId
        });
        if (response.ok) {
            showToast('Category updated');
            closeCategoryModal();
            setTimeout(function() { window.location.reload(); }, 500);
        } else {
            var data = await response.json();
            showToast('Error: ' + (data.error || 'Failed to update'));
        }
    } catch (err) {
        showToast('Error: ' + err.message);
    }
}

document.getElementById('categoryModal')?.addEventListener('click', function(e) {
    if (e.target === this) closeCategoryModal();
});

// Close modals on Escape key.
document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
        var dd = document.getElementById('drilldownModal');
        if (dd && dd.style.display !== 'none') { finDrilldown.close(); return; }
        var cm = document.getElementById('categoryModal');
        if (cm && cm.style.display !== 'none') { closeCategoryModal(); return; }
    }
});

// ============================================================
// Income source marking
// ============================================================
async function markIncome(merchant, isIncome) {
    try {
        var response = await finApi.postJSON('/api/income-source', { merchant: merchant, is_income: isIncome });
        if (response.ok) {
            showToast(isIncome ? 'Marked as income' : 'Excluded from income');
            setTimeout(function() { window.location.reload(); }, 500);
        }
    } catch (err) {
        showToast('Error: ' + err.message);
    }
}

// ============================================================
// Demo banner dismiss
// ============================================================
(function() {
    var banner = document.getElementById('demoBanner');
    if (!banner) return;
    if (localStorage.getItem('fin_demo_dismissed') === '1') {
        banner.style.display = 'none';
    }
})();

function finDismissDemo() {
    localStorage.setItem('fin_demo_dismissed', '1');
    var banner = document.getElementById('demoBanner');
    if (banner) banner.style.display = 'none';
}
