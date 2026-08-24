// fin_connect.js — connect page logic (CSV import + SimpleFIN setup).
// Loaded with `defer`; runs after HTML is parsed.

var _csvFile = null;
var _csvPreviewData = null;

var dropZone    = document.getElementById('dropZone');
var fileInput   = document.getElementById('csvFileInput');
var loadingEl   = document.getElementById('csvLoading');
var previewCard = document.getElementById('csvPreviewCard');
var mappingForm = document.getElementById('colMappingForm');
var statusMsg   = document.getElementById('csvStatusMsg');
var importBtn   = document.getElementById('csvImportBtn');

if (dropZone) {
    dropZone.addEventListener('dragover', function(e) { e.preventDefault(); dropZone.classList.add('drag-over'); });
    dropZone.addEventListener('dragleave', function() { dropZone.classList.remove('drag-over'); });
    dropZone.addEventListener('drop', function(e) {
        e.preventDefault();
        dropZone.classList.remove('drag-over');
        var files = e.dataTransfer.files;
        if (files.length > 0) handleFileSelected(files[0]);
    });
}
if (fileInput) {
    fileInput.addEventListener('change', function() { if (fileInput.files.length > 0) handleFileSelected(fileInput.files[0]); });
}

function handleFileSelected(file) {
    _csvFile = file;
    showCsvLoading(true);
    hidePreview();
    hideMappingForm();
    clearStatus();
    postPreview(file);
}

function showCsvLoading(show) {
    loadingEl.style.display = show ? 'block' : 'none';
    dropZone.style.display  = show ? 'none'  : 'flex';
}

function hidePreview() { previewCard.classList.remove('visible'); }
function hideMappingForm() { mappingForm.classList.remove('visible'); }

function clearStatus() {
    statusMsg.style.display = 'none';
    statusMsg.textContent = '';
    statusMsg.className = 'import-status-msg';
}

function showStatus(msg, type) {
    statusMsg.textContent = msg;
    statusMsg.className = 'import-status-msg ' + (type || '');
    statusMsg.style.display = 'block';
}

function escHtml(str) {
    if (str == null) return '';
    return String(str).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

async function postPreview(file) {
    var formData = new FormData();
    formData.append('file', file);
    try {
        var resp = await fetch('/api/import/csv/preview', { method: 'POST', headers: { 'X-Fin-Request': '1' }, body: formData });
        var data = await resp.json();
        if (!resp.ok) {
            showCsvLoading(false);
            showStatus(data.error || 'Preview failed.', 'error');
            return;
        }
        _csvPreviewData = data;
        showCsvLoading(false);
        renderPreview(data);
    } catch (err) {
        showCsvLoading(false);
        showStatus('Network error — could not analyse file.', 'error');
    }
}

function renderPreview(data) {
    var bankName = data.bank_display_name || (data.detected_bank ? data.detected_bank : null);
    var summaryEl = document.getElementById('csvPreviewSummary');
    summaryEl.textContent = data.row_count + ' transaction' + (data.row_count !== 1 ? 's' : '') +
        (bankName ? ' from ' + bankName : '');
    var bankBadge = document.getElementById('csvPreviewBank');
    if (bankName) { bankBadge.textContent = bankName; bankBadge.style.display = 'inline'; }
    else { bankBadge.style.display = 'none'; }
    var wrap = document.getElementById('csvPreviewTableWrap');
    if (data.preview && data.preview.length > 0) {
        var html = '<table class="csv-preview-table"><thead><tr><th>Date</th><th>Amount</th><th>Description</th></tr></thead><tbody>';
        for (var i = 0; i < data.preview.length; i++) {
            var row = data.preview[i];
            var amtCls = row.amount >= 0 ? 'amount-pos' : 'amount-neg';
            var amtFmt = (row.amount >= 0 ? '+' : '') + '$' + Math.abs(row.amount).toFixed(2);
            html += '<tr><td>' + escHtml(row.date) + '</td><td class="' + amtCls + '">' + escHtml(amtFmt) + '</td><td>' + escHtml(row.description) + '</td></tr>';
        }
        html += '</tbody></table>';
        wrap.innerHTML = html;
    } else {
        wrap.innerHTML = '<div style="padding:12px 16px;font-size:0.75rem;color:var(--text-muted);">No rows to preview.</div>';
    }
    previewCard.classList.add('visible');
    if (!data.detected_bank) renderMappingForm(data.headers, data.column_mapping);
}

function renderMappingForm(headers, existingMapping) {
    var roles = ['date', 'amount', 'description'];
    var container = document.getElementById('colMappingRows');
    var html = '';
    for (var i = 0; i < roles.length; i++) {
        var role = roles[i];
        html += '<div class="col-mapping-row"><label>' + role + '</label><select id="mapCol_' + role + '"><option value="">-- select column --</option>';
        for (var j = 0; j < headers.length; j++) {
            var h = headers[j];
            html += '<option value="' + escHtml(h) + '"' + (existingMapping && existingMapping[role] === h ? ' selected' : '') + '>' + escHtml(h) + '</option>';
        }
        html += '</select></div>';
    }
    container.innerHTML = html;
    mappingForm.classList.add('visible');
}

function applyColMapping() {
    if (!_csvFile || !_csvPreviewData) return;
    var dateCol   = document.getElementById('mapCol_date').value;
    var amountCol = document.getElementById('mapCol_amount').value;
    var descCol   = document.getElementById('mapCol_description').value;
    if (!dateCol || !amountCol || !descCol) { showStatus('Please select all three columns before proceeding.', 'error'); return; }
    _csvPreviewData._manualMapping = { date: dateCol, amount: amountCol, description: descCol };
    hideMappingForm();
    showStatus('Column mapping applied. Click "Import transactions" to continue.', '');
}

async function confirmCsvImport() {
    if (!_csvFile) return;
    importBtn.disabled = true;
    importBtn.innerHTML = '<span class="spinner"></span> Importing...';
    clearStatus();
    var formData = new FormData();
    formData.append('file', _csvFile);
    var mapping = (_csvPreviewData && _csvPreviewData._manualMapping)
        ? _csvPreviewData._manualMapping
        : (_csvPreviewData && _csvPreviewData.column_mapping ? _csvPreviewData.column_mapping : {});
    var params = new URLSearchParams();
    if (mapping.date)        params.set('date_col', mapping.date);
    if (mapping.amount)      params.set('amount_col', mapping.amount);
    if (mapping.description) params.set('description_col', mapping.description);
    try {
        var resp = await fetch('/api/import/csv/confirm?' + params.toString(), { method: 'POST', headers: { 'X-Fin-Request': '1' }, body: formData });
        var data = await resp.json();
        if (!resp.ok) {
            importBtn.disabled = false;
            importBtn.textContent = 'Import transactions';
            showStatus(data.error || 'Import failed.', 'error');
            return;
        }
        importBtn.innerHTML = '&#10003; Imported!';
        importBtn.classList.add('btn-success');
        showStatus('Imported ' + data.imported + ' transaction' + (data.imported !== 1 ? 's' : '') +
            (data.skipped_duplicates > 0 ? ', skipped ' + data.skipped_duplicates + ' duplicate' + (data.skipped_duplicates !== 1 ? 's' : '') : '') + '.', 'success');
        setTimeout(function() { window.location.href = '/dashboard'; }, 1800);
    } catch (err) {
        importBtn.disabled = false;
        importBtn.textContent = 'Import transactions';
        showStatus('Network error — import failed.', 'error');
    }
}

function resetCsvImport() {
    _csvFile = null;
    _csvPreviewData = null;
    fileInput.value = '';
    hidePreview();
    hideMappingForm();
    clearStatus();
    dropZone.style.display = 'flex';
    importBtn.disabled = false;
    importBtn.textContent = 'Import transactions';
    importBtn.classList.remove('btn-success');
}

function toggleAccordion(trigger) {
    var item = trigger.closest('.bank-accordion-item');
    var isOpen = item.classList.contains('open');
    document.querySelectorAll('.bank-accordion-item.open').forEach(function(el) { el.classList.remove('open'); });
    if (!isOpen) item.classList.add('open');
}

document.addEventListener('DOMContentLoaded', function() {
    var hash = window.location.hash.replace('#', '').replace(/[^a-zA-Z0-9-]/g, '');
    if (hash) {
        var item = document.querySelector('.bank-accordion-item[data-bank="' + hash + '"]');
        if (item) item.classList.add('open');
        if (hash === 'simplefin') {
            document.getElementById('simplefinSection').scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
    }
});

async function saveSimplefinToken() {
    var token = document.getElementById('simplefinTokenInput').value.trim();
    var btn   = document.getElementById('simplefinSaveBtn');
    var msgEl = document.getElementById('simplefinStatusMsg');
    if (!token) {
        msgEl.textContent = 'Please paste your SimpleFIN access URL.';
        msgEl.className = 'import-status-msg error';
        msgEl.style.display = 'block';
        return;
    }
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span> Saving...';
    msgEl.style.display = 'none';
    try {
        var resp = await fetch('/api/simplefin-token', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Fin-Request': '1' },
            body: JSON.stringify({ access_url: token }),
        });
        var data = await resp.json();
        if (!resp.ok) {
            btn.disabled = false;
            btn.textContent = 'Save access URL';
            msgEl.textContent = data.error || 'Failed to save access URL.';
            msgEl.className = 'import-status-msg error';
            msgEl.style.display = 'block';
            return;
        }
        btn.textContent = 'Saved!';
        msgEl.textContent = 'Access URL saved. Use the Sync button on the dashboard to import your transactions.';
        msgEl.className = 'import-status-msg success';
        msgEl.style.display = 'block';
    } catch (err) {
        btn.disabled = false;
        btn.textContent = 'Save access URL';
        msgEl.textContent = 'Network error — could not save access URL.';
        msgEl.className = 'import-status-msg error';
        msgEl.style.display = 'block';
    }
}
