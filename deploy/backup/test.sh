#!/bin/sh
# Test de bout en bout (nécessite Docker) : dump chiffré -> restauration dans une base vierge ->
# mêmes nombres de lignes ; illisible sans la bonne clé ; rétention 7 quotidiennes + 4 hebdo.
# Lancé par `make backup-test`. Tout est jetable (réseau, conteneurs, dossier temporaire).
set -eu
[ -z "${TEST_TRACE:-}" ] || set -x

here=$(cd "$(dirname "$0")" && pwd)
id="dlbtest$$"
img="doctolibre-backup:test"
net="$id-net"
pg="$id-pg"
work=$(mktemp -d)
pgpass=testpass

cleanup() {
  docker rm -f "$pg" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT INT TERM
fail() { echo "ECHEC: $*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 || fail "Docker est requis (démarrer Docker puis relancer)"

echo "== build de l'image de sauvegarde"
docker build -q -t "$img" "$here" >/dev/null

echo "== base de test"
docker network create "$net" >/dev/null
docker run -d --name "$pg" --network "$net" -e POSTGRES_PASSWORD=$pgpass -e POSTGRES_DB=src --tmpfs /var/lib/postgresql/data postgres:17-alpine >/dev/null
i=0
until docker exec "$pg" psql -U postgres -d src -Atc 'select 1' >/dev/null 2>&1; do
  i=$((i + 1)); [ "$i" -lt 60 ] || fail "postgres ne démarre pas"; sleep 1
done
sleep 1
psql_src() { docker exec -i "$pg" psql -U postgres -d "$1" -v ON_ERROR_STOP=1 -Atq; }
psql_src src <<'SQL'
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE TABLE patient (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE appointment (
  id serial PRIMARY KEY, patient_id uuid NOT NULL REFERENCES patient(id), practitioner int NOT NULL,
  during_blocked tstzrange NOT NULL, payload bytea,
  EXCLUDE USING gist (practitioner WITH =, during_blocked WITH &&));
INSERT INTO patient (name) SELECT 'SECRET-MARKER-' || g FROM generate_series(1, 50) g;
INSERT INTO appointment (patient_id, practitioner, during_blocked, payload)
  SELECT p.id, 1, tstzrange('2026-01-01'::timestamptz + (row_number() OVER ())::int * interval '1 hour',
                            '2026-01-01'::timestamptz + (row_number() OVER ())::int * interval '1 hour' + interval '30 minutes'), '\xdeadbeef'
  FROM patient p;
SQL

echo "== clés age"
docker run --rm "$img" age-keygen >"$work/key.txt" 2>/dev/null
docker run --rm "$img" age-keygen >"$work/wrong.txt" 2>/dev/null
recipient=$(grep -i 'public key' "$work/key.txt" | sed 's/.*: //')
case "$recipient" in age1*) ;; *) fail "clé publique introuvable" ;; esac
mkdir "$work/backups"
chmod 777 "$work/backups"
chmod 644 "$work/key.txt" "$work/wrong.txt"

run() { # run COMMANDE... : exécute dans l'image, sur le réseau de test
  docker run --rm --network "$net" -v "$work/backups:/backups" -v "$work/key.txt:/k/key.txt:ro" -v "$work/wrong.txt:/k/wrong.txt:ro" \
    -e BACKUP_DATABASE_URL="postgres://postgres:$pgpass@$pg:5432/src" -e DOCTOLIBRE_BACKUP_AGE_RECIPIENT="$recipient" \
    -e BACKUP_STAMP="${STAMP:-}" -e BACKUP_FORCE_WEEKLY="${WEEKLY:-}" "$img" "$@"
}

echo "== sauvegarde"
STAMP=20260101-030000 run /usr/local/bin/backup.sh
file="$work/backups/daily/doctolibre-20260101-030000.dump.age"
[ -s "$file" ] || fail "fichier de sauvegarde absent"
if grep -aq 'SECRET-MARKER' "$file" || grep -aq 'PGDMP' "$file"; then fail "la sauvegarde est lisible en clair"; fi
echo "ok: aucun contenu en clair"

echo "== illisible sans la clé"
if run /bin/sh -c 'age -d -i /k/wrong.txt /backups/daily/doctolibre-20260101-030000.dump.age >/dev/null 2>&1'; then
  fail "déchiffrement réussi avec une mauvaise clé"
fi
echo "ok: mauvaise clé refusée"

echo "== restauration dans une base vierge"
echo 'CREATE DATABASE restored;' | psql_src postgres
run /usr/local/bin/restore.sh --in /backups/daily/doctolibre-20260101-030000.dump.age --identity /k/key.txt \
  --target "postgres://postgres:$pgpass@$pg:5432/restored"
for t in patient appointment; do
  a=$(echo "select count(*) from $t" | psql_src src)
  b=$(echo "select count(*) from $t" | psql_src restored)
  [ "$a" = "$b" ] && [ "$a" -gt 0 ] || fail "table $t : source=$a restaurée=$b"
  echo "ok: $t = $a lignes"
done
m=$(echo "select count(*) from patient where name like 'SECRET-MARKER-%'" | psql_src restored)
[ "$m" = 50 ] || fail "contenu restauré différent"
c=$(echo "select count(*) from pg_constraint where contype='x'" | psql_src restored)
[ "$c" = 1 ] || fail "contrainte d'exclusion non restaurée"
# Rejouable sur une base déjà restaurée (--clean --if-exists).
run /usr/local/bin/restore.sh --in /backups/daily/doctolibre-20260101-030000.dump.age --identity /k/key.txt \
  --target "postgres://postgres:$pgpass@$pg:5432/restored"
echo "ok: restauration rejouable"

echo "== rétention (7 quotidiennes, 4 hebdo)"
for d in 02 03 04 05 06 07 08 09 10 11; do
  STAMP=202601$d-030000 WEEKLY=1 run /usr/local/bin/backup.sh
done
nd=$(ls "$work/backups/daily" | wc -l | tr -d ' ')
nw=$(ls "$work/backups/weekly" | wc -l | tr -d ' ')
[ "$nd" = 7 ] || fail "quotidiennes: $nd (attendu 7)"
[ "$nw" = 4 ] || fail "hebdomadaires: $nw (attendu 4)"
[ ! -e "$work/backups/daily/doctolibre-20260101-030000.dump.age" ] || fail "la plus ancienne aurait dû être supprimée"
echo "ok: $nd quotidiennes, $nw hebdomadaires"

echo "backup-test: OK"
