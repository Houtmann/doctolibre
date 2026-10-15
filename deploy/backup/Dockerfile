# Service de sauvegarde : pg_dump (même version majeure que le serveur) + age + rclone.
FROM postgres:17-alpine
RUN apk add --no-cache age rclone
COPY backup.sh restore.sh entrypoint.sh /usr/local/bin/
RUN chmod 0755 /usr/local/bin/backup.sh /usr/local/bin/restore.sh /usr/local/bin/entrypoint.sh \
    && mkdir -p /backups && chown postgres:postgres /backups
USER postgres
VOLUME /backups
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
