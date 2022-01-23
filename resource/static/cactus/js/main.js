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
    const color = '#' + Math.floor(Math.random() * 16777215).toString(16)
    const m = color.match(/^#([0-9a-f]{2})[0-9a-f]{6}$/i)
    if (m && parseInt(m[1], 16) / 255 == 0) {
        return randomColor()
    }
    return color
}

function matches(el, selector) {
    return (el.matches || el.matchesSelector || el.msMatchesSelector || el.mozMatchesSelector || el.webkitMatchesSelector || el.oMatchesSelector).call(el, selector);
};

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
    const mobileMenu = document.querySelector("#header > #nav > ul > .icon");
    if (mobileMenu) {
        mobileMenu.addEventListener('click', function (e) {
            document.querySelector("#header > #nav > ul").classList.toggle("responsive");
        });
    }

    /**
     * Controls the different versions of  the menu in blog post articles 
     * for Desktop, tablet and mobile.
     */
    if (document.querySelectorAll(".post").length) {
        var menu = document.querySelector("#menu");
        var nav = document.querySelector("#menu > #nav");
        var menuIcon = document.querySelector("#menu-icon, #menu-icon-tablet");

        /**
         * Display the menu on hi-res laptops and desktops.
         */
        const screenWidth = parseFloat(getComputedStyle(document.documentElement, null).width.replace("px", ""));
        if (screenWidth >= 1440) {
            menu.style.visibility = "visible";
            menuIcon.classList.add("active");
        }

        /**
         * Display the menu if the menu icon is clicked.
         */
        menuIcon.addEventListener('click', function () {
            if (menu.style.visibility === "hidden") {
                menu.style.visibility = "visible";
                menuIcon.classList.add("active");
            } else {
                menu.style.visibility = "hidden";
                menuIcon.classList.remove("active");
            }
            return false;
        });

        /**
         * Add a scroll listener to the menu to hide/show the navigation links.
         */
        if (document.querySelectorAll("#menu").length) {
            window.onscroll = function () {
                const rect = menu.getBoundingClientRect();
                const topDistance = rect.top + document.body.scrollTop;

                // hide only the navigation links on desktop
                if (!matches(nav, ":visible") && topDistance < 50) {
                    nav.style.display = '';
                } else if (matches(nav, ":visible") && topDistance > 100) {
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
            };
        }

        /**
         * Show mobile navigation menu after scrolling upwards,
         * hide it again after scrolling downwards.
         */
        if (document.querySelectorAll("#footer-post").length) {
            var lastScrollTop = 0;
            window.onscroll = function () {
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
            };
        }
    }
})
