# Service de sauvegarde : pg_dump 17 (même version majeure que le serveur) + age + rclone.
# Alpine nu plutôt que postgres:17-alpine (pas de serveur PostgreSQL embarqué). Garder la même
# version d'alpine que postgres:17-alpine (3.24 le 2026-11-19) ; si postgresql17-client disparaît
# de cette version, prendre la plus proche qui le fournit.
FROM alpine:3.24
# Le paquet postgresql17-client crée l'utilisateur postgres (UID/GID 70), comme l'ancienne image :
# mêmes droits sur les fichiers déjà présents dans le volume backups.
RUN apk add --no-cache postgresql17-client age rclone
COPY backup.sh restore.sh entrypoint.sh /usr/local/bin/
RUN chmod 0755 /usr/local/bin/backup.sh /usr/local/bin/restore.sh /usr/local/bin/entrypoint.sh \
    && mkdir -p /backups && chown postgres:postgres /backups
USER postgres
VOLUME /backups
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
