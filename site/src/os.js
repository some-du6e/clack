// The page's only script: points the hero and closing download buttons at
// the build for this system and marks its card. Without it those buttons
// go to #download, which lists all three systems. Phones and tablets get
// no pick, since Clack is desktop only.
(function () {
  var d = document, n = navigator, u = n.userAgent;
  var s = ((n.userAgentData && n.userAgentData.platform) || n.platform || "") + " " + u;
  if (/Android|iPhone|iPad|iPod|CrOS/.test(s) || (/Mac/.test(s) && n.maxTouchPoints > 1)) return;
  var os = /Win/.test(s) ? "win" : /Mac/.test(s) ? "mac" : /Linux|X11/.test(s) ? "linux" : "";
  var card = os && d.querySelector('.dl-card[data-os="' + os + '"]');
  if (!card) return;
  d.documentElement.setAttribute("data-os", os);
  var href = card.querySelector("[data-primary]").href;
  ["hero-dl", "cta-dl"].forEach(function (id) {
    var b = d.getElementById(id);
    if (b) { b.href = href; b.textContent = "Download for " + card.getAttribute("data-name"); }
  });
})();
