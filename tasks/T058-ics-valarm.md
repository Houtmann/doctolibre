# T058 — Alarmes dans les fichiers ICS patient

- **Phase** : 5
- **Dépend de** : T042
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le fichier `.ics` téléchargé par le patient déclenche deux rappels natifs de son agenda (la veille et deux heures avant).

## Contexte à lire
`internal/ics/ics.go` (`Event`, `Calendar`, `Feed` ; aucun `VALARM` aujourd'hui), `internal/ics/ics_test.go` et `internal/ics/testdata/`, `internal/web/public.go` (`calendarFile`, route `GET /r/{token}/ics`), `internal/service/calendar_feed.go` (flux praticien), T055 si déjà faite (ICS de série).

## Spécification
- `ics.Event` gagne une option (ex. `Alarms []time.Duration`) ; `ics.Feed`/`Calendar` écrivent pour chacune un bloc `BEGIN:VALARM` / `ACTION:DISPLAY` / `DESCRIPTION:` (texte du rendez-vous, échappé) / `TRIGGER:-P1D` ou `-PT2H` / `END:VALARM`, à l'intérieur du `VEVENT`.
- `/r/{token}/ics` (et l'ICS de série de T055 si elle existe) demande `-P1D` et `-PT2H`. Le **flux praticien** (`calendar_feed.go`) n'en a pas.
- Golden files mis à jour (`go test ./internal/ics/ -update`), diff relu.
- Dans Suivi : noter que Google Agenda ignore souvent les alarmes importées alors qu'Apple Calendar les respecte ; cela ne remplace pas le SMS de rappel.

## Critères d'acceptation
- [ ] Le `.ics` patient contient deux `VALARM` valides (lignes pliées à 75 octets, CRLF) ; le flux praticien n'en contient aucun.
- [ ] Tests unitaires `internal/ics` et golden à jour ; test web sur `/r/{token}/ics`.
- [ ] Note Google/Apple dans Suivi.

## Vérification
```bash
make check
```

## Hors scope
Web Push, manifest PWA, Wallet (`.pkpass`) — écartés.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
