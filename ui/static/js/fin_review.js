// fin_review.js — review page logic (account filter + category override).
// Loaded with `defer`; runs after HTML is parsed.

function updateAccountFilter() {
    const checked = [...document.querySelectorAll('.account-chip input:checked')].map(el => el.value);
    const params = new URLSearchParams(window.location.search);
    const total = document.querySelectorAll('.account-chip input').length;
    if (checked.length === 0) {
        params.set('accounts', 'none');
    } else if (checked.length === total) {
        params.delete('accounts');
    } else {
        params.set('accounts', checked.join(','));
    }
    window.location.search = params.toString();
}

async function setCategory(merchant, categoryId, selectEl) {
    if (!categoryId) return;
    const row = selectEl.closest('tr');
    try {
        const resp = await finApi.postJSON('/api/category-override', {merchant: merchant, category_id: categoryId});
        if (!resp.ok) throw new Error(await resp.text());
        selectEl.disabled = true;
        row.style.opacity = '0.5';
        const label = selectEl.options[selectEl.selectedIndex].text;
        selectEl.parentElement.innerHTML = '<span class="status-badge status-tracked">' + finUI.escapeHtml(label) + '</span>';
    } catch (e) {
        alert('Failed: ' + e.message);
        selectEl.value = '';
    }
}
