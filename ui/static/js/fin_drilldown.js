// fin_drilldown.js — Drilldown modal for transaction detail views.
// Replaces the stub in base.html when loaded.
(function() {
    'use strict';

    var modal = document.getElementById('drilldownModal');
    var titleEl = document.getElementById('drilldownTitle');
    var summaryEl = document.getElementById('drilldownSummary');
    var tableEl = document.getElementById('drilldownTable');
    var footerEl = document.getElementById('drilldownFooter');
    var opener = null;

    // Get current date range from URL params or default to this month.
    function getDateRange() {
        var params = new URLSearchParams(window.location.search);
        var start = params.get('start_date');
        var end = params.get('end_date');
        if (start && end) return { start: start, end: end };

        var now = new Date();
        var period = params.get('period') || 'this_month';
        var y, m;
        if (period === 'last_month') {
            var lm = new Date(now.getFullYear(), now.getMonth() - 1, 1);
            y = lm.getFullYear(); m = lm.getMonth();
        } else {
            y = now.getFullYear(); m = now.getMonth();
        }
        var s = y + '-' + String(m + 1).padStart(2, '0') + '-01';
        var em = new Date(y, m + 1, 1);
        var e = em.getFullYear() + '-' + String(em.getMonth() + 1).padStart(2, '0') + '-01';
        return { start: s, end: e };
    }

    function formatUSD(cents) {
        var neg = cents < 0;
        if (neg) cents = -cents;
        var d = Math.floor(cents / 100);
        var c = cents % 100;
        var s = '$' + d.toLocaleString() + '.' + String(c).padStart(2, '0');
        return neg ? '-' + s : s;
    }

    function escapeHtml(s) {
        if (typeof finUI !== 'undefined' && finUI.escapeHtml) return finUI.escapeHtml(s);
        var div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
    }

    function showModal() {
        modal.style.display = 'flex';
        // Focus the close button for keyboard users.
        var closeBtn = modal.querySelector('.modal-close');
        if (closeBtn) closeBtn.focus();
    }

    function hideModal() {
        modal.style.display = 'none';
        if (opener) { opener.focus(); opener = null; }
    }

    // Close on overlay click.
    modal.addEventListener('click', function(e) {
        if (e.target === modal) hideModal();
    });

    window.finDrilldown = {
        open: function(scope) {
            opener = document.activeElement;
            titleEl.textContent = 'Loading...';
            summaryEl.innerHTML = '';
            tableEl.innerHTML = '<div style="text-align:center;padding:24px;color:var(--text-secondary);">Loading...</div>';
            footerEl.innerHTML = '';
            showModal();

            var range = getDateRange();
            var url = '/api/drilldown?scope=' + encodeURIComponent(scope) +
                '&start_date=' + encodeURIComponent(range.start) +
                '&end_date=' + encodeURIComponent(range.end);

            fetch(url)
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    if (data.error) {
                        tableEl.innerHTML = '<div style="padding:16px;color:var(--accent-red);">' + escapeHtml(data.error) + '</div>';
                        return;
                    }

                    titleEl.textContent = data.title || scope;
                    summaryEl.innerHTML = '<span>' + escapeHtml(String(data.count)) + ' transactions</span>' +
                        '<span style="margin-left:auto;font-weight:600;">' + escapeHtml(formatUSD(data.total_cents)) + '</span>';

                    if (!data.transactions || data.transactions.length === 0) {
                        tableEl.innerHTML = '<div style="padding:16px;color:var(--text-secondary);">No transactions found.</div>';
                        return;
                    }

                    var html = '<table style="width:100%;border-collapse:collapse;font-size:0.8125rem;">';
                    html += '<thead><tr style="border-bottom:1px solid var(--border);text-align:left;">' +
                        '<th style="padding:6px 8px;">Date</th>' +
                        '<th style="padding:6px 8px;">Merchant</th>' +
                        '<th style="padding:6px 8px;">Description</th>' +
                        '<th style="padding:6px 8px;text-align:right;">Amount</th>' +
                        '<th style="padding:6px 8px;">Account</th></tr></thead><tbody>';

                    data.transactions.forEach(function(t) {
                        html += '<tr style="border-bottom:1px solid var(--border-light,var(--border));">' +
                            '<td style="padding:6px 8px;white-space:nowrap;">' + escapeHtml(t.date) + '</td>' +
                            '<td style="padding:6px 8px;">' + escapeHtml(t.merchant) + '</td>' +
                            '<td style="padding:6px 8px;color:var(--text-secondary);">' + escapeHtml(t.description) + '</td>' +
                            '<td style="padding:6px 8px;text-align:right;font-family:monospace;">' + escapeHtml(formatUSD(t.amount_cents)) + '</td>' +
                            '<td style="padding:6px 8px;color:var(--text-secondary);">' + escapeHtml(t.account_name) + '</td></tr>';
                    });

                    html += '</tbody></table>';
                    tableEl.innerHTML = html;
                    footerEl.innerHTML = '<span style="font-size:0.75rem;color:var(--text-secondary);">' +
                        escapeHtml(range.start) + ' to ' + escapeHtml(range.end) + '</span>';
                })
                .catch(function(err) {
                    tableEl.innerHTML = '<div style="padding:16px;color:var(--accent-red);">Error: ' + escapeHtml(err.message) + '</div>';
                });
        },
        close: function() {
            hideModal();
        }
    };
})();
