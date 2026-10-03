/* Register before loading the editor: slow/failed CDN requests must not leave
 * title and metadata edits unprotected. Compare values, not input callbacks:
 * toolbar actions, undo, and autocomplete can change data without an event. */
window.SolitudesPublishGuard = (() => {
    const fields = [...document.querySelectorAll('.editor-container input, .editor-container select')]
        .filter(field => field.id !== 'inputID' && !field.closest('#accessDialog'));
    let editor;
    function snapshot() {
        return JSON.stringify({
            fields: fields.map(field => field.type === 'checkbox' ? field.checked : field.value),
            content: editor ? editor.getValue() : null,
        });
    }
    let saved = snapshot();
    function changed() {
        try { return snapshot() !== saved; }
        catch (_) { return true; } // Never discard work if the editor fails.
    }
    window.addEventListener('beforeunload', event => {
        if (!changed()) return;
        event.preventDefault();
        event.returnValue = ''; // Browsers supply their own localized warning.
    });
    return {
        snapshot,
        changed,
        attach(instance) {
            editor = instance;
            // Normalize the initial Markdown without resetting metadata edits
            // made while Vditor was loading.
            const baseline = JSON.parse(saved);
            baseline.content = editor.getValue();
            saved = JSON.stringify(baseline);
        },
        saved(snapshot) { saved = snapshot; },
    };
})();
