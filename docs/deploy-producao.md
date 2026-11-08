# Deploy em produção (Portainer)

Stack: [`docker-compose.prod.yml`](../docker-compose.prod.yml). Variáveis: [`.env.prod.example`](../.env.prod.example). Decisões, riscos e plano de testes: [planejamento-deploy-producao.md](planejamento-deploy-producao.md).

```
Internet --TLS--> proxy do servidor --(rede "proxy")--> convenios-proxy (HAProxy :80)
                                                          |- /api/*     -> convenios-api   (binário Go, distroless)
                                                          |- /storage/* -> convenios-minio (URLs pré-assinadas)
                                                          '- /          -> convenios-web   (Next.js, build de produção)
db, minio, gotenberg, clamav, cleanup, backup: só na rede interna (sem porta publicada)
```

## 1. Antes do primeiro deploy

1. **DNS** do domínio apontando para o servidor e **proxy do servidor** pronto para ele (seção 3).
2. **Rede do proxy** existente: `docker network create proxy` (ou o nome que o seu proxy já usa, e então `PROXY_NETWORK=<nome>`). Se ela não existir o deploy falha com "network proxy declared as external, but could not be found".
3. **MT Login de produção**: cadastre no client do Keycloak a URL de retorno `https://<domínio>/auth/callback` e a de pós-logout `https://<domínio>/auth/sign-in`. Os endereços de produção do MT Login são pedidos à SEPLAG; **não** use os de homologação (`hml.login...`) no `.env` de produção.
4. **Recursos do servidor**: o build do Next.js precisa de ~2 GB de RAM livres (só durante o build), o ClamAV usa ~1 GB em regime, e as imagens ocupam ~3,5 GB de disco (o Gotenberg sozinho tem 2,5 GB).
5. **Sub-rede**: `APP_SUBNET` (padrão `10.231.0.0/24`) não pode colidir com nenhuma rede Docker existente (`docker network ls` + `docker network inspect`). Se colidir, o deploy falha com "Pool overlaps": escolha outra faixa.
6. **Segredos**: gere com `openssl rand -hex 32` (JWT e salt) e `openssl rand -hex 24` (senhas do banco e do MinIO). Hexadecimal, porque as senhas entram em URLs.

## 2. Portainer: criar a stack

1. **Stacks > Add stack > Repository**.
2. *Repository URL* do projeto (com autenticação, se privado) e *Reference* `refs/heads/main` (ou uma tag, para fixar a versão).
3. *Compose path*: `docker-compose.prod.yml`.
4. *Environment variables*: **Load variables from .env file** com o `.env.prod.example` preenchido. Os obrigatórios que faltarem derrubam o deploy com a mensagem do que falta.
5. **Deploy the stack**. A primeira vez constrói três imagens (Go ~1 min, Next ~2 min) e o ClamAV baixa as assinaturas: conte **5 a 10 minutos**. Enquanto o `clamav` não fica `healthy`, os **envios de arquivo são recusados** (a varredura falha fechada); o restante funciona.
6. Resultado esperado: `migrate`, `seed` e `minio-init` em *exited (0)*; os demais *running*, com `healthy` em db, api, web, proxy, gotenberg e clamav.

> Não há bind mount de arquivos do repositório de propósito: o Portainer clona o Git dentro do contêiner dele, e um caminho relativo apontaria para um diretório vazio no host. As configurações vão dentro das imagens.

> Testei esta stack com `docker compose up -d --build` a partir de uma cópia limpa do repositório, mas **não pela interface do Portainer**. Se a atualização pelo Portainer não reconstruir as imagens (confira a data de criação em *Images*), force com *Pull and redeploy*; em último caso, na linha de comando do servidor: `docker compose -p <nome-da-stack> build` na pasta clonada.

## 3. Proxy do servidor

O proxy termina o TLS e encaminha **tudo** (não só `/api`) para `convenios-proxy:80`, na rede `proxy`. Regras que valem para qualquer um:

- **Preservar o `Host`** original. As URLs pré-assinadas do MinIO são assinadas para o domínio público; com outro `Host` o download dá 403.
- **Enviar `X-Forwarded-For` e `X-Forwarded-Proto: https`** (todos os proxies abaixo fazem por padrão). O IP do cliente (rate limit) e a proteção CSRF dependem deles.
- **Não reescrever o caminho** e permitir **corpo de até 200 MB** sem bufferizar: os envios de arquivo vão por `/storage`.

**Nginx Proxy Manager** — Proxy Host: *Forward Hostname* `convenios-proxy`, porta `80`, *Websockets* e *Block Common Exploits* à vontade, SSL com Let's Encrypt e *Force SSL*. Na aba *Advanced*:

```nginx
client_max_body_size 250m;
proxy_request_buffering off;
proxy_read_timeout 600s;
proxy_send_timeout 600s;
```

**Traefik** — rótulos no serviço `convenios-proxy` (acrescente ao compose; ajuste `websecure` e `letsencrypt` aos nomes do seu Traefik):

```yaml
    labels:
      - traefik.enable=true
      - traefik.docker.network=proxy
      - traefik.http.routers.convenios.rule=Host(`convenios.exemplo.gov.br`)
      - traefik.http.routers.convenios.entrypoints=websecure
      - traefik.http.routers.convenios.tls.certresolver=letsencrypt
      - traefik.http.services.convenios.loadbalancer.server.port=80
```

**Caddy** — `convenios.exemplo.gov.br { reverse_proxy convenios-proxy:80 }` (Caddy no Docker, na rede `proxy`). Caddy no host: `reverse_proxy 127.0.0.1:8090` (`HAPROXY_PORT`).

**Nginx no host** — `proxy_pass http://127.0.0.1:8090;` com `proxy_set_header Host $host;`, `X-Forwarded-For $proxy_add_x_forwarded_for`, `X-Forwarded-Proto $scheme` e as quatro diretivas acima.

HSTS e demais cabeçalhos de segurança de transporte ficam por conta do proxy.

## 4. Primeiro acesso

1. `https://<domínio>/auth/sign-in` > **Entrar** > MT Login. Quem tem o CPF em `SEED_SUPER_ADMIN_CPF` entra como Super Admin (o `seed` roda a cada deploy e é idempotente).
2. Outros administradores: entram pelo MT Login (sem permissões) e o Super Admin libera.
3. **Passo manual de implantação** (débito T12): marque os departamentos finalísticos reais; sem isso o Analista não consegue indicar nenhum.
4. Teste o envio de e-mail (por exemplo, convidando alguém). Sem SMTP o deploy nem sobe, mas credencial errada só aparece no envio.

## 5. Conferências rápidas

```bash
docker ps --filter name=<stack> --format 'table {{.Names}}\t{{.Status}}'
curl -s https://<domínio>/api/health        # {"data":{"status":"ok"}...}
curl -s https://<domínio>/api/ready         # confirma o banco
docker logs <stack>-clamav-1 | tail         # "socket found, clamd started."
docker logs <stack>-convenios-api-1 | tail  # JSON; nível em LOG_LEVEL
```

Comandos da CLI (a imagem da API é distroless: **sem shell**, então o console do Portainer não abre; use `docker exec` direto no binário):

```bash
docker exec <stack>-convenios-api-1 /cli admin:create --cpf 000.000.000-00
docker exec <stack>-convenios-api-1 /cli migrate:status
docker exec <stack>-convenios-api-1 /cli cleanup          # já roda sozinho, 1x por dia, no serviço "cleanup"
```

## 6. Atualizar e voltar atrás

- **Atualizar**: faça o push no Git e, na stack, **Pull and redeploy**. O `migrate` roda sozinho antes da API; o `seed` também (não desfaz o que os administradores alteraram). O banco, os documentos e as assinaturas do ClamAV ficam em volumes e sobrevivem.
- **Mudou o domínio (`APP_URL`)?** Reconstrua a imagem do `web` (o endereço é embutido no build do Next).
- **Voltar atrás**: aponte a *Reference* da stack para a tag/commit anterior e faça *Pull and redeploy*. **Migrações não são desfeitas sozinhas**: se a versão nova trouxe uma, restaure o backup de antes do deploy ou rode `/cli migrate:down` (desfaz uma de cada vez; só se a migração tiver `down` seguro). Por isso, **faça um backup manual antes de cada atualização** (abaixo).
- Não rode `docker system prune -a` no servidor: a imagem do MinIO (Chainguard) só tem a tag `latest` gratuita e uma versão antiga pode não voltar a ser baixável.

## 7. Backup e restauração

**O que existe**: o serviço `backup` grava um `pg_dump` comprimido por dia (`BACKUP_HOUR_UTC`, padrão 06h UTC = 02h em Cuiabá) no volume `<stack>_backups`, guardando `BACKUP_RETENTION_DAYS` (14) dias. Um dump com falha nunca substitui um bom e aparece no log como `ERRO`.

**O que NÃO existe e é seu**:

1. **Cópia para fora do servidor.** O volume está no mesmo disco do banco. Agende (cron do host) algo como:
   `docker run --rm -v <stack>_backups:/b:ro -v /mnt/externo:/o alpine sh -c 'cp -n /b/*.sql.gz /o/'`
2. **Documentos (MinIO)**: nada os copia. Faça cópia do volume `<stack>_minio-data` (de preferência com a stack parada, ou use `mc mirror` para outro bucket).

**Backup manual agora** (antes de atualizar): `docker restart <stack>-backup-1` com `BACKUP_ON_START=true` gera um na hora; ou direto:
`docker exec <stack>-db-1 pg_dump -U convenios convenios | gzip > backup-manual.sql.gz`

**Restaurar** (testado: dump restaurado num banco novo bateu tabela a tabela com o original):

```bash
# 1. pare quem usa o banco (API e jobs); o db continua de pé
docker stop <stack>-convenios-api-1 <stack>-cleanup-1 <stack>-backup-1
# 2. recrie o banco vazio e carregue o dump
docker exec <stack>-db-1 psql -U convenios -d postgres -c 'DROP DATABASE convenios' -c 'CREATE DATABASE convenios'
docker exec <stack>-backup-1 sh -c 'gunzip -c /backups/<arquivo>.sql.gz' | docker exec -i <stack>-db-1 psql -U convenios -d convenios -v ON_ERROR_STOP=1
# 3. suba tudo de novo (pelo Portainer, "Update the stack", ou docker start)
```

## 8. Limites e avisos conhecidos

- **MT-ID (assinatura de declarações)**: o cliente real não existe ([debito-tecnico-mt-id.md](debito-tecnico-mt-id.md)) e a API recusa o simulado em produção. Em produção, **emitir declaração responde 503**; o anexo manual do documento continua funcionando.
- **ClamAV falha fechado**: se o `clamav` cair, os envios de arquivo são recusados até ele voltar.
- **Documentação da API (`/api/docs`)** está bloqueada no HAProxy de produção (404). O console do MinIO está desligado.
- **Fuso**: logs e nomes de backup em UTC.
- **Imagem do `web` (447 MB)** inclui o Node completo (`node:24-slim`); mudar para Alpine economiza espaço mas exige reverificar o `sharp`. Não foi feito.
