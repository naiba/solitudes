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

function matches(el, selector) {
    return (el.matches || el.matchesSelector || el.msMatchesSelector || el.mozMatchesSelector || el.webkitMatchesSelector || el.oMatchesSelector).call(el, selector);
};

ready(function () {
    /**
     * 首页哔哔区域高度限制（仅桌面端）
     */
    if (window.matchMedia('(min-width: 769px)').matches) {
        const articles = document.querySelector('.home-articles-section');
        const topics = document.querySelector('.home-topics-section');
        if (articles && topics && articles.offsetHeight > 0) {
            topics.style.maxHeight = articles.offsetHeight + 'px';
        }
    }

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
    if (document.querySelectorAll(".post").length) {
        var menu = document.querySelector("#menu");
        var nav = document.querySelector("#menu > #nav");
        var menuIcons = document.querySelectorAll("#menu-icon, #menu-icon-tablet");

        /**
         * Display the menu on hi-res laptops and desktops.
         */
        const screenWidth = parseFloat(getComputedStyle(document.documentElement, null).width.replace("px", ""));
        if (screenWidth >= 1440) {
            menu.style.visibility = "visible";
            menuIcons.forEach(function(icon) { icon.classList.add("active"); });
        }

        /**
         * Display the menu if the menu icon is clicked.
         */
        menuIcons.forEach(function(menuIcon) {
            menuIcon.addEventListener('click', function () {
                var isOpen = menu.style.visibility === "visible";
                menu.style.visibility = isOpen ? "hidden" : "visible";
                menuIcons.forEach(function(icon) { icon.classList.toggle("active", !isOpen); });
                return false;
            });
        });

        /**
         * Add a scroll listener to the menu to hide/show the navigation links.
         */
        if (document.querySelectorAll("#menu").length) {
            window.addEventListener('scroll', function () {
                const topDistance = document.documentElement.scrollTop || document.body.scrollTop;
                const navIsVisible = window.getComputedStyle(nav).display !== 'none';

                // hide only the navigation links on desktop
                if (!navIsVisible && topDistance < 50) {
                    nav.style.display = '';
                } else if (navIsVisible && topDistance > 100) {
                    nav.style.display = 'none';
                }

                // on tablet, hide the navigation icon as well and show a "scroll to top
                // icon" instead
                const menuIconVisible = matches(document.querySelector("#menu-icon"), ":visible");
                if (!menuIconVisible && topDistance < 50) {
                    document.querySelector("#menu-icon-tablet").style.display = '';
                    document.querySelector("#top-icon-tablet").style.display = 'none';
                } else if (!menuIconVisible && topDistance > 100) {
                    document.querySelector("#top-icon-tablet").style.display = '';
                    document.querySelector("#menu-icon-tablet").style.display = 'none';
                }
            });
        }

        /**
         * Show mobile navigation menu after scrolling upwards,
         * hide it again after scrolling downwards.
         */
        if (document.querySelectorAll("#footer-post").length) {
            var lastScrollTop = 0;
            window.addEventListener('scroll', function () {
                var topDistance = document.documentElement.scrollTop ? document.documentElement.scrollTop : document.body.scrollTop;

                if (topDistance > lastScrollTop) {
                    // downscroll -> show menu
                    document.querySelector("#footer-post").style.display = 'none';
                } else {
                    // upscroll -> hide menu
                    document.querySelector("#footer-post").style.display = '';
                }
                lastScrollTop = topDistance;

                // close all submenu"s on scroll
                document.querySelector("#nav-footer").style.display = 'none';
                document.querySelector("#toc-footer").style.display = 'none';
                document.querySelector("#share-footer").style.display = 'none';

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
