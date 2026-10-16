// Agenda du personnel : EventCalendar (static/vendor) alimenté par /admin/agenda/events.
// Sans ce script (ou sans JavaScript), la liste du jour rendue par le serveur reste visible.
// Aucun style inline : la CSP (default-src 'self') l'interdit ; les couleurs viennent de input.css.
(function () {
  "use strict";
  var root = document.getElementById("agenda");
  var mount = document.getElementById("ec");
  var wrap = document.getElementById("agenda-calendar");
  var fallback = document.getElementById("agenda-fallback");
  var dialog = document.getElementById("appt-dialog");
  var body = document.getElementById("appt-dialog-body");
  var errBox = document.getElementById("agenda-error");
  if (!root || !mount || !wrap || typeof EventCalendar === "undefined") return;

  var resources = [];
  try {
    resources = JSON.parse(root.dataset.resources || "[]");
  } catch (e) {
    resources = [];
  }
  var VIEW_TO_EC = { week: "timeGridWeek", day: "timeGridDay", practitioners: "resourceTimeGridDay" };
  var EC_TO_VIEW = { timeGridWeek: "week", timeGridDay: "day", resourceTimeGridDay: "practitioners" };

  function initialView() {
    var v = VIEW_TO_EC[root.dataset.view];
    if (v === "resourceTimeGridDay" && resources.length < 2) v = "timeGridDay";
    if (v) return v;
    return window.matchMedia && matchMedia("(max-width: 767px)").matches ? "timeGridDay" : "timeGridWeek";
  }

  // Le serveur envoie des dates avec le décalage du fuseau du cabinet. EventCalendar est utilisé en mode « UTC » sur
  // l'heure murale du cabinet : on retire le décalage (« 09:00+01:00 » devient « 09:00 »), et la grille ne subit ni le
  // fuseau du navigateur ni les glissements d'une heure des jours de changement d'heure (la grille locale du
  // navigateur est fausse d'une heure après 03:00 le dernier dimanche d'octobre). Dans l'autre sens, on envoie
  // les chaînes murales d'EventCalendar (startStr, dateStr : sans décalage), que le serveur lit dans le fuseau du
  // cabinet. On n'utilise jamais ses objets Date (ce sont des dates locales du navigateur).
  function wall(iso) {
    return iso.replace(/(Z|[+-]\d\d:\d\d)$/, "");
  }
  function showError(on) {
    if (errBox) errBox.hidden = !on;
  }

  var ec;
  function currentView() {
    return ec ? ec.getOption("view") : initialView();
  }

  // Un seul praticien en vue semaine ou jour (le premier) ; tous en vue « par praticien ».
  function wantedIds() {
    if (!resources.length) return [];
    if (currentView() === "resourceTimeGridDay") return resources.map(function (r) { return r.id; });
    return [resources[0].id];
  }

  function toEvents(list) {
    var out = [];
    list.forEach(function (e) {
      e.start = wall(e.start);
      e.end = wall(e.end);
      out.push(e);
      var p = e.extendedProps || {};
      if (e.display === "background" || !p.blocked_start) return;
      var bs = wall(p.blocked_start);
      var be = wall(p.blocked_end);
      if (bs !== e.start) {
        out.push({ id: "pre-" + e.id, resourceId: e.resourceId, start: bs, end: e.start, display: "background", classNames: ["agenda-buffer"] });
      }
      if (be !== e.end) {
        out.push({ id: "post-" + e.id, resourceId: e.resourceId, start: e.end, end: be, display: "background", classNames: ["agenda-buffer"] });
      }
    });
    return out;
  }

  function fetchEvents(info) {
    var q = new URLSearchParams();
    q.set("start", info.startStr);
    q.set("end", info.endStr);
    wantedIds().forEach(function (id) { q.append("practitioner", id); });
    return fetch(root.dataset.eventsUrl + "?" + q.toString(), { credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then(function (list) {
        showError(false);
        return toEvents(list);
      })
      .catch(function (e) {
        showError(true);
        throw e;
      });
  }

  // Ouvre une page de l'application dans la boîte de dialogue (htmx), ou navigue si htmx est absent.
  function openInDialog(url) {
    if (window.htmx && typeof window.htmx.ajax === "function") {
      window.htmx.ajax("GET", url, { target: "#appt-dialog-body", swap: "innerHTML" });
    } else {
      window.location.href = url;
    }
  }
  function openDetail(id) {
    openInDialog(root.dataset.detailUrl + "/" + encodeURIComponent(id));
  }
  function openNew(dateStr, resourceId) {
    var q = new URLSearchParams();
    q.set("start", dateStr.replace(" ", "T").slice(0, 16));
    var pid = resourceId || (resources.length ? resources[0].id : "");
    if (pid) q.set("practitioner", pid);
    q.set("view", EC_TO_VIEW[currentView()] || "week");
    openInDialog(root.dataset.newUrl + "?" + q.toString());
  }

  function syncURL(info) {
    var day = info.startStr.slice(0, 10);
    var view = EC_TO_VIEW[currentView()] || "week";
    var params = new URLSearchParams(window.location.search);
    params.set("view", view);
    params.set("date", day);
    try {
      history.replaceState(null, "", window.location.pathname + "?" + params.toString());
    } catch (e) {}
    var picker = document.getElementById("agenda-picker");
    if (picker) {
      var v = picker.querySelector('input[name="view"]');
      var dt = picker.querySelector('input[name="date"]');
      if (v) v.value = view;
      if (dt) dt.value = day;
    }
  }

  function box(cls, text) {
    var d = document.createElement("div");
    d.className = cls;
    d.textContent = text;
    return d;
  }

  fallback.hidden = true;
  wrap.hidden = false;

  var views = ["timeGridWeek", "timeGridDay"];
  if (resources.length > 1) views.push("resourceTimeGridDay");

  ec = EventCalendar.create(mount, {
    view: initialView(),
    date: root.dataset.date || undefined,
    timeZone: "UTC",
    locale: "fr",
    firstDay: 1,
    height: Math.max(480, window.innerHeight - 240) + "px",
    slotMinTime: "07:00:00",
    slotMaxTime: "21:00:00",
    flexibleSlotTimeLimits: true,
    slotDuration: "00:30:00",
    scrollTime: "08:00:00",
    allDaySlot: false,
    nowIndicator: false, // « maintenant » serait en UTC, donc faux ; voir le commentaire sur les dates
    editable: false,
    eventStartEditable: false,
    eventDurationEditable: false,
    lazyFetching: false,
    resources: resources,
    headerToolbar: { start: "prev,next today", center: "title", end: views.join(",") },
    buttonText: { today: "Aujourd’hui", timeGridWeek: "Semaine", timeGridDay: "Jour", resourceTimeGridDay: "Par praticien" },
    views: {
      timeGridWeek: { titleFormat: { year: "numeric", month: "long", day: "numeric" } },
      timeGridDay: { titleFormat: { weekday: "long", year: "numeric", month: "long", day: "numeric" } },
      resourceTimeGridDay: { titleFormat: { weekday: "long", year: "numeric", month: "long", day: "numeric" } }
    },
    eventSources: [{ events: fetchEvents }],
    eventContent: function (info) {
      var ev = info.event;
      if (ev.display === "background") return { domNodes: [] };
      var p = ev.extendedProps || {};
      var wrapEl = document.createElement("div");
      wrapEl.appendChild(box("font-semibold truncate", ev.title));
      var sub = p.type || "";
      if (p.home) sub += " · domicile";
      if (p.status && p.status !== "booked") sub += " · " + p.status_label;
      wrapEl.appendChild(box("truncate opacity-80", sub));
      return { domNodes: [wrapEl] };
    },
    eventDidMount: function (info) {
      var ev = info.event;
      if (ev.display === "background") return;
      var p = ev.extendedProps || {};
      var el = info.el;
      el.setAttribute("tabindex", "0");
      el.setAttribute("role", "button");
      el.setAttribute("aria-label", [info.timeText, ev.title, p.type, p.status_label].filter(Boolean).join(", "));
      el.addEventListener("keydown", function (k) {
        if (k.key === "Enter" || k.key === " ") {
          k.preventDefault();
          openDetail(ev.id);
        }
      });
    },
    eventClick: function (info) {
      if (info.event.display !== "background") openDetail(info.event.id);
    },
    dateClick: function (info) {
      openNew(info.dateStr, info.resource ? info.resource.id : "");
    },
    datesSet: function (info) {
      syncURL(info);
    },
    loading: function (isLoading) {
      wrap.setAttribute("aria-busy", isLoading ? "true" : "false");
    }
  });

  // Une action de la boîte de dialogue a réussi (en-tête HX-Trigger) : on ferme et on recharge.
  document.addEventListener("agenda:changed", function (e) {
    var date = e.detail && e.detail.date;
    if (dialog && dialog.open) dialog.close();
    if (date) ec.setOption("date", date);
    ec.refetchEvents();
  });

  if (dialog && body) {
    dialog.addEventListener("close", function () {
      body.replaceChildren();
    });
  }
})();
