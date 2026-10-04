/* Native Markdown fences survive all Vditor modes without custom HTML nodes. */
window.SolitudesAccessEditor = (() => {
    let editor;
    let savedRange, savedTextSelection;
    const dialog = () => document.getElementById('accessDialog');
    function fence(content, level) {
        // A longer outer fence safely contains code examples and nested blocks.
        const runs = content.match(/`+/g) || [];
        const marker = '`'.repeat(Math.max(3, ...runs.map(run => run.length + 1)));
        return '\n\n' + marker + 'access:' + level + '\n' + content.trim() + '\n' + marker + '\n\n';
    }
    function open(vditor, rootID) {
        editor = vditor;
        savedRange = null;
        savedTextSelection = null;
        const root = document.getElementById(rootID);
        const selection = window.getSelection();
        if (selection.rangeCount && root.contains(selection.getRangeAt(0).startContainer)) {
            savedRange = selection.getRangeAt(0).cloneRange();
        }
        const source = root.querySelector('textarea.vditor-sv');
        if (source && source.getClientRects().length) {
            savedTextSelection = {source, start:source.selectionStart, end:source.selectionEnd};
        }
        document.getElementById('accessContent').value = vditor.getSelection() || '';
        dialog().showModal();
    }
    document.addEventListener('DOMContentLoaded', () => {
        document.getElementById('insertAccess').addEventListener('click', () => {
            const level = document.getElementById('accessLevel').value;
            const content = document.getElementById('accessContent').value;
            if (!content.trim()) { document.getElementById('accessContent').focus(); return; }
            dialog().close();
            editor.focus();
            if (savedTextSelection) {
                savedTextSelection.source.setSelectionRange(savedTextSelection.start, savedTextSelection.end);
            } else if (savedRange) {
                const selection = window.getSelection();
                selection.removeAllRanges();
                selection.addRange(savedRange);
            }
            // insertValue treats input as HTML and collapses the selection,
            // leaving the original text public. insertMD replaces it safely.
            editor.insertMD(fence(content, level));
        });
    });
    function tool(getEditor, rootID, tip) {
        return {
            name: 'restricted-content', tip,
            icon: '<svg viewBox="0 0 24 24"><path d="M6 10V6a6 6 0 0 1 12 0v4h2v14H4V10zm2 0h8V6a4 4 0 0 0-8 0z"/></svg>',
            click() { open(getEditor(), rootID); }
        };
    }
    return {open, fence, tool};
})();
