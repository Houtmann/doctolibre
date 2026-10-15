// Doctolibre : le site fonctionne sans JavaScript ; htmx est une amélioration progressive.
// Ce fichier est volontairement minuscule (CSP : aucun script inline).

// Agenda : le contenu chargé par htmx dans la boîte de dialogue l'ouvre.
// Sans JavaScript, les liens mènent à des pages complètes équivalentes.
document.addEventListener("htmx:afterSwap", function (e) {
  var t = e.detail && e.detail.target;
  if (!t || t.id !== "appt-dialog-body") return;
  var d = document.getElementById("appt-dialog");
  if (d && typeof d.showModal === "function" && !d.open) d.showModal();
});
