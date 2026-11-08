#!/bin/sh
# Gera um dump comprimido do PostgreSQL uma vez por dia e apaga os mais antigos que BACKUP_RETENTION_DAYS.
#
# Variáveis: PGHOST, PGUSER, PGPASSWORD, PGDATABASE (padrão do libpq),
#            BACKUP_HOUR_UTC (0-23, padrão 6 = 02h em Cuiabá), BACKUP_RETENTION_DAYS (padrão 14),
#            BACKUP_DIR (padrão /backups), BACKUP_ON_START ("true" faz um backup logo ao subir).
set -eu
# Sem pipefail, uma falha do pg_dump passaria despercebida (o gzip do dump vazio teria sucesso).
set -o pipefail

DIR="${BACKUP_DIR:-/backups}"
HOUR="${BACKUP_HOUR_UTC:-6}"
KEEP="${BACKUP_RETENTION_DAYS:-14}"

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) backup: $*"; }

backup_once() {
  name="$PGDATABASE-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
  tmp="$DIR/.$name.partial"
  # pg_dump em formato SQL puro: restaurável com psql, em qualquer versão >= 16. Grava num arquivo
  # temporário e só então renomeia: um dump interrompido nunca parece um backup completo.
  if pg_dump --no-owner --no-privileges | gzip -9 > "$tmp" && gzip -t "$tmp"; then
    mv "$tmp" "$DIR/$name"
    log "criado $name ($(wc -c < "$DIR/$name") bytes)"
  else
    rm -f "$tmp"
    log "ERRO: falha ao gerar $name"
    return 1
  fi
  find "$DIR" -maxdepth 1 -name "$PGDATABASE-*.sql.gz" -mtime +"$KEEP" -print -delete | sed 's/^/backup: removido /'
}

# Segundos até a próxima ocorrência de HOUR:00 UTC.
seconds_until_next() {
  now=$(date -u +%s)
  sod=$((now % 86400))
  target=$((HOUR * 3600))
  secs=$((target - sod))
  [ "$secs" -le 0 ] && secs=$((secs + 86400))
  echo "$secs"
}

trap 'log "encerrando"; exit 0' TERM INT

# Espera o banco aceitar conexões (o compose já garante, isto cobre reinícios).
until pg_isready -q; do sleep 2; done

if [ "${BACKUP_ON_START:-false}" = "true" ]; then backup_once || true; fi

while true; do
  secs=$(seconds_until_next)
  log "próximo backup em ${secs}s (${HOUR}h UTC)"
  # "sleep" em segundo plano + wait: o sinal de parada interrompe a espera na hora.
  sleep "$secs" &
  wait $!
  backup_once || true
done
