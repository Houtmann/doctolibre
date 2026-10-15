#!/bin/sh
# Restaure une sauvegarde chiffrée : age -d | pg_restore, dans une base EXISTANTE (idéalement vierge).
# Usage : restore.sh --in fichier.dump.age --identity clé_privée.txt --target postgres://...
#   (ou variables RESTORE_IN, RESTORE_IDENTITY, RESTORE_DATABASE_URL)
# La cible doit être explicite : on ne devine jamais la base de production.
# Ne jamais activer `set -x` : l'URL contient le mot de passe.
set -eu
set -o pipefail

log() { printf '%s restore: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
die() { log "erreur: $*"; exit 1; }

in="${RESTORE_IN:-}"
identity="${RESTORE_IDENTITY:-}"
target="${RESTORE_DATABASE_URL:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --in) in="${2:-}"; shift 2 ;;
    --identity) identity="${2:-}"; shift 2 ;;
    --target) target="${2:-}"; shift 2 ;;
    *) die "option inconnue: $1" ;;
  esac
done
{ [ -n "$in" ] && [ -f "$in" ]; } || die "--in fichier.dump.age (absent ou introuvable)"
{ [ -n "$identity" ] && [ -f "$identity" ]; } || die "--identity clé_privée.txt (absent ou introuvable)"
[ -n "$target" ] || die "--target URL de la base cible (obligatoire)"

# --clean --if-exists : rejouable sur une base déjà partiellement restaurée.
age -d -i "$identity" "$in" | pg_restore --no-owner --no-privileges --clean --if-exists --exit-on-error --dbname="$target"
log "restauration terminée"
