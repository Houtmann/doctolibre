#!/bin/sh
# Boucle de sauvegarde quotidienne (pas de cron : un sleep jusqu'à l'heure suivante).
#   BACKUP_TIME             heure UTC de la sauvegarde, HH:MM (défaut 03:30)
#   BACKUP_RUN_ON_START=1   fait aussi une sauvegarde immédiate au démarrage
# Avec des arguments (ex. `backup.sh --out x`), exécute la commande telle quelle.
set -eu
if [ $# -gt 0 ]; then exec "$@"; fi

log() { printf '%s backup-loop: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
if [ -z "${DOCTOLIBRE_BACKUP_AGE_RECIPIENT:-}" ]; then
  log "DOCTOLIBRE_BACKUP_AGE_RECIPIENT est vide : sauvegardes impossibles (docs/backup.md)"
  exit 1
fi

# secs HH MM SS -> secondes depuis minuit (supprime les zéros de tête, sinon octal).
secs() { echo $(($(echo "$1" | sed 's/^0*//; s/^$/0/') * 3600 + $(echo "$2" | sed 's/^0*//; s/^$/0/') * 60 + $(echo "$3" | sed 's/^0*//; s/^$/0/'))); }

t="${BACKUP_TIME:-03:30}"
case "$t" in [0-2][0-9]:[0-5][0-9]) ;; *) log "BACKUP_TIME invalide (attendu HH:MM)"; exit 1 ;; esac
target=$(secs "${t%%:*}" "${t##*:}" 0)

run() { /usr/local/bin/backup.sh || log "la sauvegarde a échoué (nouvelle tentative au prochain créneau)"; }
if [ "${BACKUP_RUN_ON_START:-}" = "1" ]; then run; fi
while :; do
  now=$(secs "$(date -u +%H)" "$(date -u +%M)" "$(date -u +%S)")
  wait=$(((target - now + 86400) % 86400))
  if [ "$wait" -eq 0 ]; then wait=86400; fi
  log "prochaine sauvegarde dans ${wait}s"
  sleep "$wait"
  run
done
