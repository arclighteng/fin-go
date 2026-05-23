// fin_budget.js — budget page logic.
// Loaded with `defer`; runs after HTML is parsed.

function showBudgetFormError(msg) {
    const el = document.getElementById('budgetFormError');
    if (!el) return;
    el.textContent = msg;
    el.style.display = msg ? 'inline' : 'none';
}

async function setBudgetTarget() {
    const select = document.getElementById('budgetCategorySelect');
    const input = document.getElementById('budgetAmountInput');
    const categoryId = select.value;
    const amount = parseInt(input.value, 10);

    if (!categoryId) { showBudgetFormError('Select a category first.'); select.focus(); return; }
    if (!amount || amount < 0) { showBudgetFormError('Enter a valid amount greater than 0.'); input.focus(); return; }
    showBudgetFormError('');

    try {
        const resp = await fetch('/api/budget/target', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Fin-Request': '1' },
            body: JSON.stringify({
                category_id: categoryId,
                monthly_target_cents: amount * 100,
            }),
        });
        if (!resp.ok) {
            const err = await resp.json();
            showBudgetFormError(err.error || 'Failed to set target.');
            return;
        }
        location.reload();
    } catch (e) {
        showBudgetFormError('Error: ' + e.message);
    }
}

async function actuallyDeleteBudgetTarget(categoryId) {
    try {
        const resp = await fetch('/api/budget/target/' + encodeURIComponent(categoryId), {
            method: 'DELETE',
            headers: { 'X-Fin-Request': '1' },
        });
        if (!resp.ok) {
            showBudgetFormError('Failed to remove target.');
            return;
        }
        location.reload();
    } catch (e) {
        showBudgetFormError('Error: ' + e.message);
    }
}

function deleteBudgetTarget(categoryId) {
    const row = document.querySelector('.budget-row[data-category="' + categoryId + '"]');
    if (!row) { actuallyDeleteBudgetTarget(categoryId); return; }
    const actionsCell = row.querySelector('.budget-row-actions');
    if (!actionsCell) { actuallyDeleteBudgetTarget(categoryId); return; }

    const original = actionsCell.innerHTML;
    actionsCell.innerHTML =
        '<span style="font-size:0.8rem; color:var(--text-secondary); white-space:nowrap;">' +
        '<span id="confirmDeleteBtn" style="color:var(--accent-red);cursor:pointer;font-weight:600;" tabindex="0" role="button">Delete?</span>' +
        '&nbsp;<span id="cancelDeleteBtn" style="color:var(--accent-blue);cursor:pointer;" tabindex="0" role="button">Cancel</span>' +
        '</span>';

    actionsCell.querySelector('#confirmDeleteBtn').addEventListener('click', (e) => {
        e.stopPropagation();
        actuallyDeleteBudgetTarget(categoryId);
    });
    actionsCell.querySelector('#confirmDeleteBtn').addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); actuallyDeleteBudgetTarget(categoryId); }
    });
    actionsCell.querySelector('#cancelDeleteBtn').addEventListener('click', (e) => {
        e.stopPropagation();
        actionsCell.innerHTML = original;
        const newBtn = actionsCell.querySelector('.btn-icon.delete');
        if (newBtn) newBtn.addEventListener('click', (ev) => { ev.stopPropagation(); deleteBudgetTarget(categoryId); });
    });
    actionsCell.querySelector('#cancelDeleteBtn').addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault(); e.stopPropagation();
            actionsCell.innerHTML = original;
            const newBtn = actionsCell.querySelector('.btn-icon.delete');
            if (newBtn) newBtn.addEventListener('click', (ev) => { ev.stopPropagation(); deleteBudgetTarget(categoryId); });
        }
    });
    actionsCell.querySelector('#confirmDeleteBtn').focus();
}

document.querySelectorAll('.budget-row').forEach(row => {
    const catId = row.dataset.category;
    if (!catId) return;

    const pctCell = row.querySelector('.budget-row-pct');
    if (pctCell && pctCell.textContent.trim() !== '--') {
        const actions = document.createElement('div');
        actions.className = 'budget-row-actions';
        actions.innerHTML = '<button class="btn-icon delete" onclick="event.stopPropagation(); deleteBudgetTarget(\'' + finUI.escapeHtml(catId) + '\')" title="Remove target" aria-label="Remove budget target">&#10005;</button>';
        row.appendChild(actions);
        row.style.gridTemplateColumns = '200px 1fr 120px 80px 40px';
    }

    row.style.cursor = 'pointer';
    row.addEventListener('click', () => {
        const select = document.getElementById('budgetCategorySelect');
        select.value = catId;
        document.getElementById('budgetAmountInput').focus();
        select.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    });
});
