// fin_theme_init.js — applies the saved theme before content renders
// to prevent a flash of unstyled / wrong-theme content.
// Loaded synchronously (no defer) immediately before page content.
(function() {
    try {
        var saved = localStorage.getItem('theme');
        if (saved === 'dark') {
            document.body.setAttribute('data-theme', 'dark');
        }
    } catch (_) {
        // localStorage may be blocked; fall back to default theme.
    }
})();
