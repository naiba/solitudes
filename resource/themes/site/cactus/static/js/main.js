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

function randomColor() {
    // HSL: hue 0-360, saturation 60-100%, lightness 20-60%
    const h = Math.floor(Math.random() * 360);
    const s = Math.floor(Math.random() * 40) + 60;
    const l = Math.floor(Math.random() * 40) + 20;
    return `hsl(${h},${s}%,${l}%)`;
}

ready(function () {
    /**
     * 标签云
     */
    const tagsCloud = document.querySelectorAll("#tags>a");
    if (tagsCloud.length > 0) {
        for (let i = 0; i < tagsCloud.length; i++) {
            while (!tagsCloud[i].style.backgroundColor) {
                tagsCloud[i].style.backgroundColor = randomColor();
            }
        }
    }

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
         * Display the menu on hi-res laptops and desktops.
         */
        const desktopMenu = window.matchMedia('(min-width: 1800px)');
        setPostMenuOpen(desktopMenu.matches);
        desktopMenu.addEventListener('change', event => setPostMenuOpen(event.matches));

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
