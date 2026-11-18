// The page's only script, and nothing needs it. It points the hero and
// closing download buttons at the build for this system and marks its card.
// Without it those buttons go to #download, which lists all three systems.
// Phones and tablets get no pick: Clack is desktop only. It also closes the
// phone menu when one of its links is followed.
(function () {
  var d = document, n = navigator, u = n.userAgent, p = n.platform || "";
  var s = ((n.userAgentData && n.userAgentData.platform) || p) + " " + u;
  var menu = d.querySelector(".menu");
  if (menu) menu.addEventListener("click", function (e) { if (e.target.closest("a")) menu.open = false; });
  if (/Android|iPhone|iPad|iPod|CrOS/.test(s) || (/Mac/.test(s) && n.maxTouchPoints > 1)) return;
  var os = /Win/.test(s) ? "win" : /Mac/.test(s) ? "mac" : /Linux|X11/.test(s) ? "linux" : "";
  var card = os && d.querySelector('.dl-card[data-os="' + os + '"]');
  if (!card) return;
  d.documentElement.setAttribute("data-os", os);
  // Linux on ARM says so in navigator.platform ("Linux aarch64"); Chrome's
  // user agent string claims x86_64 there
  var href = os == "linux" && /aarch64|arm/i.test(p) ? card.getAttribute("data-arm") : card.querySelector("[data-primary]").href;
  ["hero-dl", "cta-dl"].forEach(function (id) {
    var b = d.getElementById(id);
    if (b) { b.href = href; b.textContent = "Download for " + card.getAttribute("data-name"); }
  });
})();
