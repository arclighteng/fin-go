// fin_dashboard.js — dashboard page logic.
// Loaded with `defer`; runs after HTML is parsed. Server-rendered values
// are read from <script id="dashboard-data" type="application/json">…</script>
// to avoid embedding any executable JavaScript inline.

(function readDashboardData() {
    var dataEl = document.getElementById('dashboard-data');
    if (!dataEl) {
        window.__finDashboardData = null;
        return;
    }
    try {
        window.__finDashboardData = JSON.parse(dataEl.textContent);
    } catch (e) {
        console.error('Failed to parse dashboard data:', e);
        window.__finDashboardData = null;
    }
})();

// Month navigation
function navigateMonth(delta) {
    var params = new URLSearchParams(window.location.search);
    var currentPeriod = params.get('period') || 'this_month';
    var targetDate;
    var startStr = params.get('start_date');

    if (startStr) {
        targetDate = new Date(startStr + 'T00:00:00');
    } else if (currentPeriod === 'last_month') {
        var now = new Date();
        targetDate = new Date(now.getFullYear(), now.getMonth() - 1, 1);
    } else {
        targetDate = new Date();
        targetDate.setDate(1);
    }

    targetDate.setMonth(targetDate.getMonth() + delta);

    var now = new Date();
    var isThisMonth = targetDate.getFullYear() === now.getFullYear() && targetDate.getMonth() === now.getMonth();
    var lm = new Date(now.getFullYear(), now.getMonth() - 1, 1);
    var isLastMonth = targetDate.getFullYear() === lm.getFullYear() && targetDate.getMonth() === lm.getMonth();

    var url = new URL(window.location.href);
    if (isThisMonth) {
        url.searchParams.set('period', 'this_month');
        url.searchParams.delete('start_date');
        url.searchParams.delete('end_date');
    } else if (isLastMonth) {
        url.searchParams.set('period', 'last_month');
        url.searchParams.delete('start_date');
        url.searchParams.delete('end_date');
    } else {
        var start = targetDate.toISOString().split('T')[0];
        var endDate = new Date(targetDate.getFullYear(), targetDate.getMonth() + 1, 1);
        var end = endDate.toISOString().split('T')[0];
        url.searchParams.set('start_date', start);
        url.searchParams.set('end_date', end);
        url.searchParams.set('period', 'this_month');
    }
    window.location = url;
}

function navigateToMonth(startDate) {
    var targetDate = new Date(startDate + 'T00:00:00');
    var now = new Date();
    var isThisMonth = targetDate.getFullYear() === now.getFullYear() && targetDate.getMonth() === now.getMonth();
    var url = new URL(window.location.href);
    if (isThisMonth) {
        url.searchParams.set('period', 'this_month');
        url.searchParams.delete('start_date');
        url.searchParams.delete('end_date');
    } else {
        var start = targetDate.toISOString().split('T')[0];
        var endDate = new Date(targetDate.getFullYear(), targetDate.getMonth() + 1, 1);
        var end = endDate.toISOString().split('T')[0];
        url.searchParams.set('start_date', start);
        url.searchParams.set('end_date', end);
        url.searchParams.set('period', 'this_month');
    }
    window.location = url;
}

// Account filter
function toggleAccounts(e) {
    e.stopPropagation();
    var dropdown = document.getElementById('accountDropdown');
    if (!dropdown) return;
    dropdown.classList.toggle('open');
    var isOpen = dropdown.classList.contains('open');
    var toggle = document.getElementById('accountToggle');
    if (toggle) toggle.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
    if (isOpen) {
        var first = dropdown.querySelector('input');
        if (first) first.focus();
    }
}

// Close account dropdown on Escape.
document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
        var dropdown = document.getElementById('accountDropdown');
        if (dropdown && dropdown.classList.contains('open')) {
            dropdown.classList.remove('open');
            var toggle = document.getElementById('accountToggle');
            if (toggle) { toggle.setAttribute('aria-expanded', 'false'); toggle.focus(); }
        }
    }
});

function updateAccountFilter() {
    var checkboxes = document.querySelectorAll('#accountDropdown input[type="checkbox"]');
    var selected = Array.from(checkboxes).filter(function(cb) { return cb.checked; }).map(function(cb) { return cb.value; });
    var url = new URL(window.location.href);
    if (selected.length === 0 || selected.length === checkboxes.length) {
        url.searchParams.delete('accounts');
    } else {
        url.searchParams.set('accounts', selected.join(','));
    }
    window.location = url;
}

document.addEventListener('click', function(e) {
    var dropdown = document.getElementById('accountDropdown');
    if (dropdown && !e.target.closest('.account-toggle-v3') && !e.target.closest('.account-dropdown-v3')) {
        dropdown.classList.remove('open');
    }
});
document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
        var dropdown = document.getElementById('accountDropdown');
        if (dropdown) dropdown.classList.remove('open');
    }
});

// Alert dismissal
async function dismissAlert(key, action, index) {
    try {
        var response = await finApi.postJSON('/api/alert-action', { alert_key: key, action: action });
        if (response.ok) {
            var el = document.getElementById('huItem' + index);
            if (el) {
                el.style.transition = 'opacity 300ms ease, transform 300ms ease';
                el.style.opacity = '0';
                el.style.transform = 'translateX(20px)';
                setTimeout(function() { el.remove(); }, 300);
            }
        } else {
            var el2 = document.getElementById('huItem' + index);
            if (el2) {
                el2.style.outline = '2px solid var(--accent-red)';
                setTimeout(function() { el2.style.outline = ''; }, 2000);
            }
        }
    } catch (e) {
        console.error('Failed to dismiss alert:', e);
    }
}

// Transaction search
(function() {
    var input = document.getElementById('txnSearchV3');
    if (!input) return;
    var debounceTimer = null;

    input.addEventListener('input', function() {
        clearTimeout(debounceTimer);
        var q = this.value.trim();
        if (q.length < 2) { hideSearchResults(); return; }
        debounceTimer = setTimeout(function() { doSearch(q); }, 300);
    });

    input.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') { hideSearchResults(); this.blur(); }
    });

    document.addEventListener('click', function(e) {
        if (!e.target.closest('.controls-right-v3')) hideSearchResults();
    });

    async function doSearch(q) {
        var params = new URLSearchParams(window.location.search);
        var accounts = params.get('accounts');
        var url = '/api/search?q=' + encodeURIComponent(q) + '&days=365';
        if (accounts) url += '&accounts=' + encodeURIComponent(accounts);
        try {
            var resp = await fetch(url);
            var data = await resp.json();
            renderSearchResults(data);
        } catch (e) { console.error('Search failed:', e); }
    }

    function renderSearchResults(data) {
        var resultsEl = document.getElementById('searchResultsV3');
        if (!resultsEl) {
            resultsEl = document.createElement('div');
            resultsEl.id = 'searchResultsV3';
            resultsEl.className = 'search-results-v3';
            input.parentElement.appendChild(resultsEl);
        }
        if (!data.matches || data.matches.length === 0) {
            resultsEl.innerHTML = '<div class="search-empty-v3">No results found</div>';
            resultsEl.style.display = 'block';
            return;
        }
        resultsEl.innerHTML = '';
        var fragment = document.createDocumentFragment();
        for (var i = 0; i < Math.min(data.matches.length, 20); i++) {
            var txn = data.matches[i];
            var amt = Math.abs(txn.amount_cents / 100).toFixed(2);
            var sign = txn.amount_cents >= 0 ? '+' : '-';
            var amountClass = txn.amount_cents >= 0 ? 'positive' : 'negative';
            var merchantRaw = txn.merchant || txn.description || 'Unknown';
            var resultItem = document.createElement('div');
            resultItem.className = 'search-result-item-v3';
            resultItem.setAttribute('tabindex', '0');
            resultItem.setAttribute('role', 'button');
            var capturedMerchant = merchantRaw.toLowerCase();
            resultItem.addEventListener('click', (function(m) {
                return function() { finDrilldown.open('merchant:' + encodeURIComponent(m)); };
            })(capturedMerchant));
            var infoDiv = document.createElement('div');
            infoDiv.className = 'search-result-info-v3';
            var merchantDiv = document.createElement('div');
            merchantDiv.className = 'search-result-merchant-v3';
            merchantDiv.textContent = merchantRaw;
            var metaDiv = document.createElement('div');
            metaDiv.className = 'search-result-meta-v3';
            metaDiv.textContent = txn.date + ' • ' + (txn.account_name || '');
            infoDiv.appendChild(merchantDiv);
            infoDiv.appendChild(metaDiv);
            var amountDiv = document.createElement('div');
            amountDiv.className = 'search-result-amount-v3 ' + amountClass;
            amountDiv.textContent = sign + '$' + amt;
            resultItem.appendChild(infoDiv);
            resultItem.appendChild(amountDiv);
            fragment.appendChild(resultItem);
        }
        resultsEl.appendChild(fragment);
        resultsEl.style.display = 'block';
    }

    function hideSearchResults() {
        var resultsEl = document.getElementById('searchResultsV3');
        if (resultsEl) resultsEl.style.display = 'none';
    }
})();

// Mid-month pacing (client-side; reads server-rendered values from JSON block).
(function() {
    var d = window.__finDashboardData;
    if (!d || !d.is_this_month || !d.start_date) return;

    var startDate = new Date(d.start_date);
    var today = new Date();
    var daysInMonth = new Date(today.getFullYear(), today.getMonth() + 1, 0).getDate();
    var daysElapsed = Math.floor((today - startDate) / (1000 * 60 * 60 * 24));

    if (daysElapsed > 0 && daysElapsed < daysInMonth) {
        var income = d.income_cents || 0;
        var expenses = d.expenses_cents || 0;
        var projectedIncome = Math.round(income / daysElapsed * daysInMonth);
        var projectedExpenses = Math.round(expenses / daysElapsed * daysInMonth);
        var projectedKept = projectedIncome - projectedExpenses;

        var pacingEl = document.getElementById('cfPacing');
        if (pacingEl && projectedKept !== 0) {
            var projectedDollars = Math.round(Math.abs(projectedKept) / 100);
            if (projectedKept >= 0) {
                pacingEl.textContent = daysElapsed + ' days in — on pace to keep ~$' + projectedDollars + ' this month';
            } else {
                pacingEl.textContent = daysElapsed + ' days in — on pace to end ~$' + projectedDollars + ' over this month';
                pacingEl.style.color = 'var(--accent-yellow, #d29922)';
            }
            pacingEl.style.display = 'block';
        }
    }
})();
