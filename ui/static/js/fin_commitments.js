// fin_commitments.js — commitments page logic.
// Loaded with `defer`; runs after HTML is parsed.

function toggleAddForm(direction) {
    const form = document.getElementById('add-form-' + direction);
    form.classList.toggle('hidden');
    if (!form.classList.contains('hidden')) {
        form.querySelector('input[name="name"]').focus();
    }
}

async function confirmCommitment(id, btn) {
    btn.textContent = 'Confirmed!';
    btn.disabled = true;
    btn.style.opacity = '0.7';
    const row = btn.closest('tr');
    row.style.background = 'var(--accent-green-dim)';
    const resp = await fetch('/api/commitments/' + id, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json', 'X-Fin-Request': '1'},
        body: JSON.stringify({confirmed: 1})
    });
    if (!resp.ok) { btn.textContent = 'Retry'; btn.disabled = false; btn.style.opacity = '1'; return; }
    setTimeout(() => location.reload(), 600);
}

function dismissCommitment(id, btn) {
    const originalHTML = btn.innerHTML;
    btn.innerHTML = 'Undo';
    btn.style.color = 'var(--accent-blue)';
    const row = btn.closest('tr');
    row.style.opacity = '0.5';

    const timer = setTimeout(async () => {
        const resp = await fetch('/api/commitments/' + id, {
            method: 'PATCH',
            headers: {'Content-Type': 'application/json', 'X-Fin-Request': '1'},
            body: JSON.stringify({confirmed: 0, source: 'dismissed'})
        });
        if (resp.ok) {
            row.remove();
        } else {
            row.style.opacity = '1';
            btn.innerHTML = originalHTML;
            btn.style.color = '';
        }
    }, 3000);

    btn.onclick = () => {
        clearTimeout(timer);
        btn.innerHTML = originalHTML;
        btn.style.color = '';
        row.style.opacity = '1';
        btn.onclick = () => dismissCommitment(id, btn);
    };
}

async function deleteCommitment(id) {
    if (!confirm('Delete this commitment?')) return;
    const resp = await fetch('/api/commitments/' + id, {method: 'DELETE', headers: {'X-Fin-Request': '1'}});
    if (resp.ok) {
        const el = document.getElementById('row-' + id);
        if (el) el.remove();
    }
}

function toggleDismissed(direction) {
    const container = document.getElementById('dismissed-' + direction);
    const toggle = document.getElementById('dismissed-toggle-' + direction);
    const isHidden = container.classList.toggle('hidden');
    toggle.textContent = toggle.textContent.replace(isHidden ? 'Hide' : 'Show', isHidden ? 'Show' : 'Hide');
}

async function restoreCommitment(id, btn) {
    btn.textContent = 'Restoring…';
    btn.disabled = true;
    const resp = await fetch('/api/commitments/' + id, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json', 'X-Fin-Request': '1'},
        body: JSON.stringify({source: 'detected', confirmed: 0})
    });
    if (resp.ok) {
        location.reload();
    } else {
        btn.textContent = 'Retry';
        btn.disabled = false;
    }
}

const CADENCE_OPTIONS = [
    ['monthly', 'Monthly'],
    ['biweekly', 'Biweekly'],
    ['weekly', 'Weekly'],
    ['annual', 'Annual'],
    ['quarterly', 'Quarterly'],
    ['one_time', 'One-time'],
];

document.querySelectorAll('.editable-cadence').forEach(span => {
    span.addEventListener('click', function() {
        const id = this.dataset.id;
        const current = this.dataset.cadence;
        const td = this.parentElement;

        const select = document.createElement('select');
        select.style.cssText = 'padding:4px 8px; border:1px solid var(--accent-blue); border-radius:6px; background:var(--bg-primary); color:var(--text-primary); font-size: 0.8125rem; outline:none;';
        select.setAttribute('aria-label', 'Change frequency');
        for (const [value, label] of CADENCE_OPTIONS) {
            const opt = document.createElement('option');
            opt.value = value;
            opt.textContent = label;
            if (value === current) opt.selected = true;
            select.appendChild(opt);
        }

        td.replaceChild(select, this);
        select.focus();

        const revert = () => {
            if (td.contains(select)) td.replaceChild(span, select);
        };

        select.addEventListener('change', async () => {
            const newCadence = select.value;
            if (newCadence === current) { revert(); return; }
            select.disabled = true;
            select.style.opacity = '0.6';
            const resp = await fetch('/api/commitments/' + id, {
                method: 'PATCH',
                headers: {'Content-Type': 'application/json', 'X-Fin-Request': '1'},
                body: JSON.stringify({cadence: newCadence})
            });
            if (resp.ok) {
                location.reload();
            } else {
                select.disabled = false;
                select.style.opacity = '1';
            }
        });

        select.addEventListener('blur', () => {
            setTimeout(revert, 150);
        });
    });
});

function showFormError(formId, message) {
    let errorEl = document.getElementById(formId + '-error');
    if (!errorEl) {
        errorEl = document.createElement('div');
        errorEl.id = formId + '-error';
        errorEl.style.cssText = 'color: var(--accent-red); font-size: 0.8125rem; margin-top: 8px;';
        errorEl.setAttribute('role', 'alert');
        document.getElementById(formId).appendChild(errorEl);
    }
    errorEl.textContent = message;
    setTimeout(() => { errorEl.textContent = ''; }, 5000);
}

async function submitAddForm(e, direction) {
    e.preventDefault();
    const form = e.target;
    const fd = new FormData(form);
    const data = {
        name: fd.get('name'),
        direction: direction,
        cadence: fd.get('cadence'),
        source: 'manual',
        confirmed: 1
    };
    const dollars = fd.get('amount_dollars');
    if (dollars) data.expected_cents = Math.round(parseFloat(dollars) * 100);
    const resp = await fetch('/api/commitments', {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'X-Fin-Request': '1'},
        body: JSON.stringify(data),
    });
    if (resp.ok) location.reload();
    else {
        const body = await resp.json().catch(() => null);
        showFormError('add-form-' + direction, (body && body.error) ? body.error : 'Failed to save. Please fill in at least a name.');
    }
}

async function dismissDuplicateGroup(merchant, groupId) {
    const groupEl = document.getElementById(groupId);
    if (groupEl) {
        groupEl.style.opacity = '0.5';
        groupEl.style.pointerEvents = 'none';
    }
    try {
        const resp = await fetch('/api/dismiss-duplicate', {
            method: 'POST',
            headers: {'Content-Type': 'application/json', 'X-Fin-Request': '1'},
            body: JSON.stringify({ merchant: merchant, dismiss: true }),
        });
        if (resp.ok) {
            if (groupEl) groupEl.remove();
            const headsUpSection = groupEl ? groupEl.closest('section') : null;
            if (headsUpSection && headsUpSection.querySelectorAll('[id^="dup-group-"]').length === 0) {
                headsUpSection.remove();
            }
        } else {
            if (groupEl) { groupEl.style.opacity = '1'; groupEl.style.pointerEvents = ''; }
        }
    } catch (err) {
        if (groupEl) { groupEl.style.opacity = '1'; groupEl.style.pointerEvents = ''; }
    }
}
