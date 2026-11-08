# Backup diário do PostgreSQL (pg_dump). A versão do cliente precisa acompanhar a do servidor (16).
FROM postgres:16-alpine
COPY backup.sh /usr/local/bin/backup.sh
RUN chmod 0755 /usr/local/bin/backup.sh && mkdir -p /backups && chown postgres:postgres /backups
USER postgres
ENTRYPOINT ["/usr/local/bin/backup.sh"]
