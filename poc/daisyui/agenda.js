/* Agenda : EventCalendar (vkurko) + fiche de RDV daisyUI. Données factices. */
(function () {
  "use strict";
  var PRACT = { lea: { id: "lea", title: "Léa Marchand" }, hugo: { id: "hugo", title: "Hugo Bernard" } };
  var LABEL = { booked: "Réservé", done: "Honoré", noshow: "Absent", cancelled: "Annulé" };
  var BADGE = { booked: "badge-primary", done: "badge-success", noshow: "badge-warning", cancelled: "badge-ghost" };
  var APPTS = [
 {
  "id": 1,
  "day": "2026-10-12",
  "start": "08:30",
  "end": "09:15",
  "patient": "Martine Dubois",
  "motif": "Bilan initial",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 2,
  "day": "2026-10-12",
  "start": "09:30",
  "end": "10:00",
  "patient": "Karim Benali",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 3,
  "day": "2026-10-12",
  "start": "10:00",
  "end": "10:30",
  "patient": "Chloé Fabre",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "noshow"
 },
 {
  "id": 4,
  "day": "2026-10-12",
  "start": "09:00",
  "end": "09:30",
  "patient": "Paul Lemoine",
  "motif": "Séance de rééducation",
  "pract": "hugo",
  "status": "done"
 },
 {
  "id": 5,
  "day": "2026-10-12",
  "start": "14:00",
  "end": "14:45",
  "patient": "Anne Rousseau",
  "motif": "Séance à domicile",
  "pract": "hugo",
  "status": "done"
 },
 {
  "id": 6,
  "day": "2026-10-12",
  "start": "15:00",
  "end": "15:30",
  "patient": "Jean Morel",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 7,
  "day": "2026-10-13",
  "start": "08:00",
  "end": "08:30",
  "patient": "Sophie Garnier",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 8,
  "day": "2026-10-13",
  "start": "09:00",
  "end": "09:45",
  "patient": "Thomas Perrin",
  "motif": "Bilan initial",
  "pract": "hugo",
  "status": "done"
 },
 {
  "id": 9,
  "day": "2026-10-13",
  "start": "10:30",
  "end": "11:00",
  "patient": "Nadia Haddad",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "cancelled"
 },
 {
  "id": 10,
  "day": "2026-10-13",
  "start": "14:30",
  "end": "15:00",
  "patient": "Louis Petit",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 11,
  "day": "2026-10-13",
  "start": "16:00",
  "end": "16:45",
  "patient": "Isabelle Roy",
  "motif": "Séance à domicile",
  "pract": "hugo",
  "status": "done"
 },
 {
  "id": 12,
  "day": "2026-10-14",
  "start": "08:30",
  "end": "09:00",
  "patient": "Marc Colin",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 13,
  "day": "2026-10-14",
  "start": "11:00",
  "end": "11:45",
  "patient": "Julie Noël",
  "motif": "Bilan initial",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 14,
  "day": "2026-10-14",
  "start": "10:00",
  "end": "10:30",
  "patient": "Émile Girard",
  "motif": "Séance de rééducation",
  "pract": "hugo",
  "status": "noshow"
 },
 {
  "id": 15,
  "day": "2026-10-14",
  "start": "15:30",
  "end": "16:00",
  "patient": "Fatou Diallo",
  "motif": "Séance de rééducation",
  "pract": "hugo",
  "status": "done"
 },
 {
  "id": 16,
  "day": "2026-10-15",
  "start": "09:00",
  "end": "09:30",
  "patient": "Pierre Lambert",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 17,
  "day": "2026-10-15",
  "start": "09:00",
  "end": "09:45",
  "patient": "Camille Faure",
  "motif": "Bilan initial",
  "pract": "hugo",
  "status": "done"
 },
 {
  "id": 18,
  "day": "2026-10-15",
  "start": "16:30",
  "end": "17:00",
  "patient": "Yasmine Amrani",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "cancelled"
 },
 {
  "id": 19,
  "day": "2026-10-16",
  "start": "08:30",
  "end": "09:00",
  "patient": "Hélène Marchetti",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "done"
 },
 {
  "id": 20,
  "day": "2026-10-16",
  "start": "10:00",
  "end": "10:45",
  "patient": "Olivier Blanc",
  "motif": "Bilan initial",
  "pract": "lea",
  "status": "booked"
 },
 {
  "id": 21,
  "day": "2026-10-16",
  "start": "14:00",
  "end": "14:30",
  "patient": "Sandrine Vidal",
  "motif": "Séance de rééducation",
  "pract": "hugo",
  "status": "booked"
 },
 {
  "id": 22,
  "day": "2026-10-16",
  "start": "15:00",
  "end": "15:45",
  "patient": "Robert Caron",
  "motif": "Séance à domicile",
  "pract": "hugo",
  "status": "booked"
 },
 {
  "id": 23,
  "day": "2026-10-17",
  "start": "09:00",
  "end": "09:30",
  "patient": "Inès Moreau",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "booked"
 },
 {
  "id": 24,
  "day": "2026-10-17",
  "start": "09:30",
  "end": "10:00",
  "patient": "Antoine Lefèvre",
  "motif": "Séance de rééducation",
  "pract": "lea",
  "status": "booked"
 },
 {
  "id": 25,
  "day": "2026-10-17",
  "start": "10:30",
  "end": "11:15",
  "patient": "Mathilde Aubert",
  "motif": "Bilan initial",
  "pract": "hugo",
  "status": "booked"
 }
];
  var DAYS = ["2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16", "2026-10-17"];

  function el(id) { return document.getElementById(id); }
  function fmtDay(iso) {
    return new Date(iso + "T12:00:00").toLocaleDateString("fr-FR", { weekday: "long", day: "numeric", month: "long" });
  }

  function toEvent(a) {
    return {
      id: String(a.id),
      resourceIds: [a.pract],
      start: a.day + " " + a.start,
      end: a.day + " " + a.end,
      title: a.patient,
      backgroundColor: "var(--st-" + a.status + "-bg)",
      textColor: "var(--st-" + a.status + "-fg)",
      classNames: ["appt-" + a.status],
      extendedProps: { appt: a.id }
    };
  }
  function lunch() {
    return DAYS.map(function (d) {
      return { id: "lunch-" + d, display: "background", start: d + " 12:00", end: d + " 13:30",
               backgroundColor: "var(--color-base-300)", classNames: ["lunch"] };
    });
  }

  var filter = "";
  function events() {
    return APPTS.filter(function (a) { return !filter || a.pract === filter; }).map(toEvent).concat(lunch());
  }

  var ec = EventCalendar.create(el("ec"), {
    view: "timeGridWeek",
    date: "2026-10-16",
    locale: "fr",
    firstDay: 1,
    hiddenDays: [0],
    height: "calc(100vh - 14rem)",
    slotMinTime: "08:00:00",
    slotMaxTime: "20:00:00",
    slotDuration: "00:30:00",
    scrollTime: "08:00:00",
    allDaySlot: false,
    nowIndicator: true,
    editable: false,
    eventStartEditable: false,
    resources: [PRACT.lea, PRACT.hugo],
    headerToolbar: { start: "prev,next today", center: "title", end: "timeGridWeek,resourceTimeGridDay" },
    buttonText: { today: "Aujourd\u2019hui", timeGridWeek: "Semaine", resourceTimeGridDay: "Par praticien" },
    views: { timeGridWeek: { titleFormat: { year: "numeric", month: "long", day: "numeric" } } },
    events: events(),
    eventContent: function (info) {
      if (info.event.display === "background") return { html: "" };
      var a = byId(info.event.extendedProps.appt);
      var who = a.pract === "lea" ? "LM" : "HB";
      return { html: '<div class="font-semibold truncate">' + a.patient + '</div><div class="truncate opacity-80">' + a.motif + " · " + who + "</div>" };
    },
    eventDidMount: function (info) {
      if (info.event.display === "background") return;
      var a = byId(info.event.extendedProps.appt);
      info.el.setAttribute("tabindex", "0");
      info.el.setAttribute("role", "button");
      info.el.setAttribute("aria-label", a.patient + ", " + a.motif + ", " + a.start + ", " + LABEL[a.status]);
    },
    eventClick: function (info) { if (info.event.display !== "background") open(info.event.extendedProps.appt); }
  });

  function byId(id) { return APPTS.filter(function (a) { return a.id === id; })[0]; }
  function refresh() { ec.setOption("events", events()); }

  var dlg = el("appt-dialog");
  var current = null;
  function open(id) {
    current = byId(id);
    el("appt-patient").textContent = current.patient;
    el("appt-motif").textContent = current.motif;
    el("appt-time").textContent = fmtDay(current.day) + ", " + current.start + " – " + current.end;
    el("appt-pract").textContent = PRACT[current.pract].title;
    var s = el("appt-status");
    s.textContent = LABEL[current.status];
    s.className = "badge " + BADGE[current.status];
    dlg.showModal();
  }
  document.querySelectorAll("[data-set-status]").forEach(function (b) {
    b.addEventListener("click", function () {
      if (!current) return;
      current.status = b.getAttribute("data-set-status");
      refresh();
      dlg.close();
    });
  });
  el("filter-pract").addEventListener("change", function (e) { filter = e.target.value; refresh(); });
})();
