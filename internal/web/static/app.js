// Doctolibre : le site fonctionne sans JavaScript ; htmx est une amélioration progressive.
// Ce fichier est volontairement minuscule (CSP : aucun script inline). Il est chargé dans <head> SANS defer :
// le thème est posé avant le premier rendu (pas de flash). Sans JS, prefers-color-scheme décide (daisyUI).
(function () {
  var KEY = "doctolibre-theme";
  var LIGHT = "doctolibre";
  var DARK = "doctolibre-dark";
  var root = document.documentElement;

  function stored() {
    try {
      var t = localStorage.getItem(KEY);
      return t === LIGHT || t === DARK ? t : null;
    } catch (e) {
      return null;
    }
  }
  var saved = stored();
  if (saved) root.setAttribute("data-theme", saved);

  function effective() {
    var t = root.getAttribute("data-theme");
    if (t) return t;
    return window.matchMedia && matchMedia("(prefers-color-scheme: dark)").matches ? DARK : LIGHT;
  }

  document.addEventListener("DOMContentLoaded", function () {
    document.querySelectorAll("[data-theme-toggle]").forEach(function (b) {
      b.addEventListener("click", function () {
        var next = effective() === DARK ? LIGHT : DARK;
        root.setAttribute("data-theme", next);
        try {
          localStorage.setItem(KEY, next);
        } catch (e) {}
      });
    });
  });
})();

// Limitation de débit : htmx n'insère pas les réponses 4xx ; on laisse passer l'alerte du 429.
document.addEventListener("htmx:beforeSwap", function (e) {
  if (e.detail && e.detail.xhr && e.detail.xhr.status === 429) e.detail.shouldSwap = true;
});

// Agenda : le contenu chargé par htmx dans la boîte de dialogue l'ouvre.
// Sans JavaScript, les liens mènent à des pages complètes équivalentes.
document.addEventListener("htmx:afterSwap", function (e) {
  var t = e.detail && e.detail.target;
  if (!t || t.id !== "appt-dialog-body") return;
  var d = document.getElementById("appt-dialog");
  if (d && typeof d.showModal === "function" && !d.open) d.showModal();
});

// Ouvre une boîte de dialogue native : <button data-dialog-open="id">.
document.addEventListener("click", function (e) {
  var b = e.target.closest && e.target.closest("[data-dialog-open]");
  if (!b) return;
  var d = document.getElementById(b.getAttribute("data-dialog-open"));
  if (d && typeof d.showModal === "function" && !d.open) d.showModal();
});
