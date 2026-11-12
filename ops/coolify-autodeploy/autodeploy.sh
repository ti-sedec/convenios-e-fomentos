#!/usr/bin/env bash
# Dispara o deploy no Coolify quando o GitHub Actions termina de publicar as imagens de uma branch.
#
# Por que existe: o build das imagens roda no GitHub (.github/workflows/build-images.yml) e o firewall da rede do servidor
# só deixa o GitHub chegar na porta 80, então o workflow não consegue chamar a API do Coolify (portas 8000/443). Aqui o
# servidor é quem pergunta: a cada poucos minutos consulta a API pública do GitHub e, se a última execução bem-sucedida
# do workflow na branch é de um commit novo, chama a API LOCAL do Coolify. Nada é exposto na internet.
#
# Configuração em /etc/convenios-autodeploy.env (veja docs/build-imagens.md):
#   COOLIFY_TOKEN     token de API do Coolify (permissão "deploy")
#   COOLIFY_APP_UUID  uuid da aplicação no Coolify
#   BRANCH            develop (hml) ou main
# Opcionais: GITHUB_REPO, WORKFLOW, COOLIFY_URL, STATE_FILE.
set -euo pipefail

: "${COOLIFY_TOKEN:?Defina COOLIFY_TOKEN}"
: "${COOLIFY_APP_UUID:?Defina COOLIFY_APP_UUID}"
: "${BRANCH:?Defina BRANCH}"
GITHUB_REPO="${GITHUB_REPO:-ti-sedec/convenios-e-fomentos}"
WORKFLOW="${WORKFLOW:-build-images.yml}"
COOLIFY_URL="${COOLIFY_URL:-http://127.0.0.1:8000}"
STATE_FILE="${STATE_FILE:-/var/lib/convenios-autodeploy/last-sha}"

mkdir -p "$(dirname "$STATE_FILE")"
exec 9>"$STATE_FILE.lock"
flock -n 9 || exit 0   # uma execução por vez

runs=$(curl -fsS --max-time 20 -H 'Accept: application/vnd.github+json' \
  "https://api.github.com/repos/${GITHUB_REPO}/actions/workflows/${WORKFLOW}/runs?branch=${BRANCH}&status=success&per_page=1")
sha=$(jq -r '.workflow_runs[0].head_sha // empty' <<<"$runs")
[ -n "$sha" ] || { echo "nenhuma execução bem-sucedida de ${WORKFLOW} em ${BRANCH}"; exit 0; }

last=$(cat "$STATE_FILE" 2>/dev/null || true)
[ "$sha" != "$last" ] || exit 0   # nada novo

echo "imagens novas em ${BRANCH} (${sha:0:7}); disparando deploy no Coolify"
curl -fsS --max-time 30 -X POST -H "Authorization: Bearer ${COOLIFY_TOKEN}" \
  "${COOLIFY_URL}/api/v1/deploy?uuid=${COOLIFY_APP_UUID}"
echo
echo "$sha" > "$STATE_FILE"
