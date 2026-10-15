#!/bin/sh
# Sauvegarde chiffrée de la base : pg_dump (format custom) | age -r <clé publique>.
# Le dump en clair n'est JAMAIS écrit sur disque (tube direct vers age).
#
# Variables :
#   BACKUP_DATABASE_URL             DSN PostgreSQL (ou DOCTOLIBRE_DATABASE_URL)               [obligatoire]
#   DOCTOLIBRE_BACKUP_AGE_RECIPIENT clé publique age (age1...), plusieurs séparées par des espaces [obligatoire]
#   BACKUP_DIR                      dossier de sortie (défaut /backups)
#   BACKUP_KEEP_DAILY / _WEEKLY     rétention (défaut 7 / 4)
#   DOCTOLIBRE_BACKUP_S3_*          envoi S3 optionnel (voir docs/backup.md)
#   BACKUP_STAMP, BACKUP_FORCE_WEEKLY   tests uniquement
# Usage : backup.sh [--out fichier]   (--out : écrit ce fichier seul, sans rétention ni envoi)
# Ne jamais activer `set -x` : l'URL contient le mot de passe.
set -eu
set -o pipefail

log() { printf '%s backup: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
die() { log "erreur: $*"; exit 1; }

out=""
if [ "${1:-}" = "--out" ]; then
  out="${2:-}"
  [ -n "$out" ] || die "--out demande un fichier"
fi

url="${BACKUP_DATABASE_URL:-${DOCTOLIBRE_DATABASE_URL:-}}"
[ -n "$url" ] || die "BACKUP_DATABASE_URL (ou DOCTOLIBRE_DATABASE_URL) est obligatoire"
recipients="${DOCTOLIBRE_BACKUP_AGE_RECIPIENT:-}"
[ -n "$recipients" ] || die "DOCTOLIBRE_BACKUP_AGE_RECIPIENT est obligatoire (clé publique age, voir docs/backup.md)"

age_args=""
for r in $recipients; do
  case "$r" in
    age1*) age_args="$age_args -r $r" ;;
    *) die "destinataire age invalide (attendu: age1...)" ;;
  esac
done

# dump_to FICHIER : écrit un dump chiffré de façon atomique (fichier .tmp puis rename).
dump_to() {
  dest="$1"
  tmp="$dest.tmp"
  rm -f "$tmp"
  # shellcheck disable=SC2086
  if ! pg_dump --format=custom --no-owner --dbname="$url" | age $age_args -o "$tmp"; then
    rm -f "$tmp"
    die "pg_dump ou chiffrement échoué"
  fi
  [ -s "$tmp" ] || { rm -f "$tmp"; die "sauvegarde vide"; }
  chmod 0600 "$tmp"
  mv "$tmp" "$dest"
}

if [ -n "$out" ]; then
  dump_to "$out"
  log "sauvegarde écrite ($(wc -c <"$out" | tr -d ' ') octets)"
  exit 0
fi

dir="${BACKUP_DIR:-/backups}"
keep_daily="${BACKUP_KEEP_DAILY:-7}"
keep_weekly="${BACKUP_KEEP_WEEKLY:-4}"
stamp="${BACKUP_STAMP:-$(date -u +%Y%m%d-%H%M%S)}"
name="doctolibre-$stamp.dump.age"
mkdir -p "$dir/daily" "$dir/weekly"

dump_to "$dir/daily/$name"
log "quotidienne $name ($(wc -c <"$dir/daily/$name" | tr -d ' ') octets)"

# Hebdomadaire : la sauvegarde du dimanche est conservée à part (lien dur, pas de copie).
if [ "${BACKUP_FORCE_WEEKLY:-}" = "1" ] || [ "$(date -u +%u)" = "7" ]; then
  ln -f "$dir/daily/$name" "$dir/weekly/$name"
  log "hebdomadaire $name"
fi

# prune DOSSIER N : garde les N fichiers les plus récents (le nom contient l'horodatage).
prune() {
  { ls -1 "$1" | grep '^doctolibre-.*\.dump\.age$' || true; } | sort -r | tail -n +"$(($2 + 1))" | while read -r f; do
    rm -f "$1/$f"
    log "supprimé $f"
  done
}
prune "$dir/daily" "$keep_daily"
prune "$dir/weekly" "$keep_weekly"
date -u +%Y-%m-%dT%H:%M:%SZ >"$dir/.last_success"

# Envoi optionnel S3-compatible via rclone (config par variables d'environnement, rien sur disque).
if [ -n "${DOCTOLIBRE_BACKUP_S3_BUCKET:-}" ]; then
  [ -n "${DOCTOLIBRE_BACKUP_S3_ENDPOINT:-}" ] || die "DOCTOLIBRE_BACKUP_S3_ENDPOINT est obligatoire avec un bucket"
  if [ -z "${DOCTOLIBRE_BACKUP_S3_ACCESS_KEY:-}" ] || [ -z "${DOCTOLIBRE_BACKUP_S3_SECRET_KEY:-}" ]; then
    die "DOCTOLIBRE_BACKUP_S3_ACCESS_KEY et _SECRET_KEY sont obligatoires avec un bucket"
  fi
  export RCLONE_CONFIG_OFFSITE_TYPE=s3
  export RCLONE_CONFIG_OFFSITE_PROVIDER=Other
  export RCLONE_CONFIG_OFFSITE_ENDPOINT="$DOCTOLIBRE_BACKUP_S3_ENDPOINT"
  export RCLONE_CONFIG_OFFSITE_ACCESS_KEY_ID="$DOCTOLIBRE_BACKUP_S3_ACCESS_KEY"
  export RCLONE_CONFIG_OFFSITE_SECRET_ACCESS_KEY="$DOCTOLIBRE_BACKUP_S3_SECRET_KEY"
  export RCLONE_CONFIG_OFFSITE_REGION="${DOCTOLIBRE_BACKUP_S3_REGION:-}"
  prefix="${DOCTOLIBRE_BACKUP_S3_PREFIX:-doctolibre}"
  if rclone copyto "$dir/daily/$name" "offsite:$DOCTOLIBRE_BACKUP_S3_BUCKET/$prefix/$name" --s3-no-check-bucket; then
    log "envoyé vers le stockage S3"
  else
    # La copie locale existe déjà : on signale l'échec sans l'effacer.
    die "envoi S3 échoué (la sauvegarde locale est conservée)"
  fi
fi
