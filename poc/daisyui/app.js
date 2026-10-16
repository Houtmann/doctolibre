/* Thème clair/sombre (daisyUI) + sélecteur de jours de la page de réservation. Aucun script inline. */
(function () {
  var KEY = "poc-theme";
  var root = document.documentElement;
  function stored() { try { return localStorage.getItem(KEY); } catch (e) { return null; } }
  function current() {
    var t = stored();
    if (t) return t;
    return window.matchMedia && matchMedia("(prefers-color-scheme: dark)").matches ? "doctolibre-dark" : "doctolibre";
  }
  root.setAttribute("data-theme", current());

  document.addEventListener("DOMContentLoaded", function () {
    document.querySelectorAll("[data-theme-toggle]").forEach(function (b) {
      b.addEventListener("click", function () {
        var next = root.getAttribute("data-theme") === "doctolibre-dark" ? "doctolibre" : "doctolibre-dark";
        root.setAttribute("data-theme", next);
        try { localStorage.setItem(KEY, next); } catch (e) {}
      });
    });

    // Réservation : un panneau d'heures par jour.
    var days = document.querySelectorAll("input[name=day]");
    function showDay() {
      document.querySelectorAll("[data-day-panel]").forEach(function (p) {
        p.classList.toggle("hidden", p.getAttribute("data-day-panel") !== (document.querySelector("input[name=day]:checked") || {}).value);
      });
    }
    days.forEach(function (d) { d.addEventListener("change", showDay); });
    if (days.length) showDay();
  });
})();
