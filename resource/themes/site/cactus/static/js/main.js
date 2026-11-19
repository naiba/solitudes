function ready(fn) {
    if (document.readyState != 'loading') {
        fn();
    } else {
        document.addEventListener('DOMContentLoaded', fn);
    }
}

function scrollToTop(el) {
    window.scrollTo(0, 0);
}

function toggle(sel) {
    if (document.querySelector(sel).style.display) {
        document.querySelector(sel).style.display = ''
    } else { document.querySelector(sel).style.display = 'none' }
}

/**
 * Balanced columns: main.css clips the rows of a `.fill-rows` list that do not
 * fit beside the taller column by wrapping them out of view. They are still in
 * the document, so mark them inert here or keyboard focus and screen readers
 * would land on rows nobody can see.
 */
ready(function () {
    const lists = document.querySelectorAll('.fill-rows');
    if (!lists.length) return;
    function sync() {
        lists.forEach(function (list) {
            const edge = list.getBoundingClientRect().right - 1;
            Array.prototype.forEach.call(list.children, function (row) {
                row.inert = list.clientWidth > 0 && row.getBoundingClientRect().left >= edge;
            });
        });
    }
    sync();
    // The set of rows that fit changes when the list or any row changes size:
    // a neighbouring column grows, a font or late stylesheet arrives, the window resizes.
    if (window.ResizeObserver) {
        const observer = new ResizeObserver(sync);
        lists.forEach(function (list) {
            observer.observe(list);
            Array.prototype.forEach.call(list.children, function (row) { observer.observe(row); });
        });
    } else {
        window.addEventListener('resize', sync);
    }
    window.addEventListener('load', sync);
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(sync);
});

/**
 * Whether the painted palette is dark. Reads the resolved page background so it
 * holds for fixed schemes (dark, dracula, nord) and for a visitor's choice alike.
 */
function isDarkPalette() {
    const channels = getComputedStyle(document.body).backgroundColor.match(/\d+(\.\d+)?/g) || [];
    if (channels.length < 3) return false;
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2] < 128;
}

/**
 * Colour scheme: follow the device, or stay light or dark. The choice lives in
 * the cross-theme `solitudes_theme` key, which header.html applies before paint.
 */
ready(function () {
    const choices = document.querySelectorAll('[data-theme-choice]');
    if (!choices.length) return;
    function current() {
        try {
            const stored = localStorage.getItem('solitudes_theme');
            return stored === 'light' || stored === 'dark' ? stored : 'auto';
        } catch (_) { return 'auto'; }
    }
    function render(scheme) {
        const root = document.documentElement;
        root.classList.toggle('dark', scheme === 'dark');
        root.classList.toggle('light', scheme === 'light');
        choices.forEach(function (choice) {
            choice.setAttribute('aria-pressed', String(choice.dataset.themeChoice === scheme));
        });
    }
    render(current());
    choices.forEach(function (choice) {
        choice.addEventListener('click', function () {
            try { localStorage.setItem('solitudes_theme', choice.dataset.themeChoice); } catch (_) { /* Session-only when storage is blocked. */ }
            render(choice.dataset.themeChoice);
        });
    });
    // Another tab (or the admin workspace) may change the shared preference.
    window.addEventListener('storage', function (event) {
        if (event.key === 'solitudes_theme') render(current());
    });
});

// Tag colors belong to the theme stylesheet; random inline colors break contrast
// and override the directory layout on every page load.
ready(function () {
    /**
     * video height
     */
    document.querySelectorAll('.iframe__video').forEach(v => {
        v.setAttribute('height', v.clientWidth * 9 / 16)
    })

    /**
     * Shows the responsive navigation menu on mobile.
     */
    const mobileMenu = document.querySelector("#header > #nav > ul > .icon button");
    if (mobileMenu) {
        mobileMenu.addEventListener('click', function (e) {
            const navigation = document.querySelector("#header > #nav > ul");
            const isOpen = navigation.classList.toggle("responsive");
            mobileMenu.setAttribute('aria-expanded', String(isOpen));
            mobileMenu.querySelector('i').className = isOpen ? 'fa-solid fa-xmark fa-2x' : 'fa-solid fa-bars fa-2x';
        });
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') return;
            const navigation = document.querySelector("#header > #nav > ul");
            navigation.classList.remove("responsive");
            mobileMenu.setAttribute('aria-expanded', 'false');
            mobileMenu.querySelector('i').className = 'fa-solid fa-bars fa-2x';
            mobileMenu.focus();
        });
    }

    /**
     * Controls the different versions of  the menu in blog post articles 
     * for Desktop, tablet and mobile.
     */
    if (document.querySelector("#header-post #menu")) {
        var menu = document.querySelector("#menu");
        var menuIcons = document.querySelectorAll("#menu-icon, #menu-icon-tablet");

        function setPostMenuOpen(open) {
            menu.style.visibility = open ? 'visible' : 'hidden';
            menuIcons.forEach(function(icon) {
                icon.classList.toggle('active', open);
                icon.setAttribute('aria-expanded', String(open));
            });
        }

        /**
         * Desktop navigation starts expanded; tablet/mobile stay opt-in.
         */
        const desktopMenu = window.matchMedia('(min-width: 900px)');
        setPostMenuOpen(desktopMenu.matches);
        desktopMenu.addEventListener('change', event => setPostMenuOpen(event.matches));

        // Native focus scrolling can leave a partially visible link clipped.
        // Keep keyboard navigation usable when a custom menu overflows its row.
        document.querySelector('#header-post #nav').addEventListener('focusin', function (event) {
            const link = event.target.closest('a');
            if (link) link.scrollIntoView({block: 'nearest', inline: 'nearest', behavior: 'instant'});
        });

        /**
         * Display the menu if the menu icon is clicked.
         */
        menuIcons.forEach(function(menuIcon) {
            menuIcon.addEventListener('click', function () {
                setPostMenuOpen(menu.style.visibility !== "visible");
                return false;
            });
        });

        // Keep navigation available while reading; Escape returns focus to the
        // visible toggle instead of a hidden desktop/tablet counterpart.
        document.addEventListener('keydown', function (event) {
            if (document.querySelector('dialog[open]')) return;
            if (event.key !== 'Escape' || menu.style.visibility !== 'visible') return;
            setPostMenuOpen(false);
            const visibleToggle = Array.from(menuIcons).find(icon => icon.getClientRects().length);
            if (visibleToggle) visibleToggle.focus();
        });

        /**
         * Show mobile navigation menu after scrolling upwards,
         * hide it again after scrolling downwards.
         */
        if (document.querySelectorAll("#footer-post").length) {
            var lastScrollTop = 0;
            window.addEventListener('scroll', function () {
                var topDistance = document.documentElement.scrollTop ? document.documentElement.scrollTop : document.body.scrollTop;

                if (document.querySelector('#toc-footer:not([hidden])')) {
                    document.querySelector("#footer-post").style.display = '';
                } else if (topDistance > lastScrollTop) {
                    // downscroll -> show menu
                    document.querySelector("#footer-post").style.display = 'none';
                } else {
                    // upscroll -> hide menu
                    document.querySelector("#footer-post").style.display = '';
                }
                lastScrollTop = topDistance;

                // close all submenu"s on scroll
                document.querySelector("#nav-footer").style.display = 'none';
                document.querySelectorAll('#actions-footer button[aria-expanded]').forEach(button => button.setAttribute('aria-expanded', 'false'));

                // show a "navigation" icon when close to the top of the page, 
                // otherwise show a "scroll to the top" icon
                if (topDistance < 50) {
                    document.querySelector("#actions-footer > #top").style.display = 'none';
                } else if (topDistance > 100) {
                    document.querySelector("#actions-footer > #top").style.display = '';
                }
            });
        }
    }
})
