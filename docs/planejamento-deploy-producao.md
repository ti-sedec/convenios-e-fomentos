# Planejamento: stack de produção para o Portainer

Objetivo: um `docker-compose.prod.yml` que o Portainer (Stack a partir de repositório Git) sobe no servidor, com a API em Go como **binário** (imagem distroless) e o Next.js como **build de produção** (`output: standalone`), testado aqui de ponta a ponta antes de ir ao servidor.

**Estado: implementado e testado localmente** (cópia limpa do repositório, build sem cache, `docker compose up -d --build`). **Não testado:** a interface do Portainer em si, o proxy real do servidor, o TLS e o MT Login de produção.

## 1. Decisões (respondidas pelo responsável)

| # | Decisão | Consequência no desenho |
|---|---|---|
| D1 | Já existe proxy no servidor (Traefik / Nginx Proxy Manager / Caddy) | Ele termina o TLS. O HAProxy da stack continua como roteador interno por caminho (`/api`, `/storage`, `/`), **sem TLS**, e entra na rede externa do proxy. |
| D2 | Build no servidor, pelo Portainer, a partir do Git | O compose usa `build:`. **Nada de bind mount de arquivo do repositório** (ver R1): o HAProxy e o script de backup viram imagens próprias. |
| D3 | Entram: Gotenberg, ClamAV, rotina de limpeza, backup do Postgres | Seis serviços além do núcleo (ver seção 3). |

## 2. Riscos conhecidos que o desenho precisa neutralizar

| # | Risco | Tratamento |
|---|---|---|
| R1 | Portainer clona o repositório dentro do contêiner dele: um bind mount relativo (`./haproxy/haproxy.cfg`) vira um diretório vazio no host e o HAProxy não sobe. | Config do HAProxy e script de backup **dentro de imagens** (`build:`). Só volumes nomeados. |
| R2 | `MTID_SIMULATE=true` é recusado em produção, e o cliente real do MT-ID não existe (débito). | `MTID_SIMULATE` fixo em `false`. Emitir declaração responde 503; o anexo manual segue. **Avisar o responsável.** |
| R3 | Sem SMTP em produção os e-mails ficam como "enviados" sem sair (débito T2). | `SMTP_HOST`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` **obrigatórios** (`:?`). |
| R4 | A API só confia no `X-Forwarded-For` de proxies listados em `TRUSTED_PROXIES`. Se a rede do Docker cair em `192.168.x`, `172.16.0.0/12` não a cobre e todo mundo cai no mesmo balde de rate limit (10 logins/min no total). | Sub-rede da rede interna **fixa** (`APP_SUBNET`), e `TRUSTED_PROXIES` derivado dela. |
| R5 | O HAProxy fica na rede do proxy, compartilhada com outras stacks. Um serviço chamado `api` ou `web` em outra stack responderia ao mesmo nome DNS. | Serviços que o HAProxy alcança têm nome único: `convenios-api`, `convenios-web`, `convenios-minio`. |
| R6 | O HAProxy não deve acrescentar o IP do proxy do servidor ao `X-Forwarded-For` (seria visto como "cliente"). | Sem `option forwardfor`; só acrescenta `src` se o cabeçalho não existir. |
| R7 | `NEXT_PUBLIC_*` é embutido no **build** do Next. | `APP_URL` entra como `build.args`; mudar o domínio exige reconstruir a imagem do `web`. |
| R8 | Imagem da API é distroless (sem shell, sem curl): não há healthcheck nem `exec` de shell. | Binário `/healthcheck` minúsculo na imagem; comandos da CLI via `docker exec <ctr> /cli ...` ou serviços de uma execução. |
| R9 | Limite de corpo do proxy do servidor: o upload vai pelo domínio (`/storage`), até 200 MB. Nginx tem 1 MB por padrão. | Documentar o ajuste por proxy no runbook. |
| R10 | Console do MinIO exposto em produção. | `MINIO_BROWSER=off` e nenhuma rota no HAProxy. |
| R11 | Documentação da API pública (`/api/docs`) exposta. | Bloqueada no HAProxy (404). |
| R12 | Deriva de versão das imagens de terceiros. | Tags fixas (`postgres:16-alpine`, `gotenberg:8`, `clamav:1.4`, `haproxy:3.4.7-alpine`); MinIO só tem `latest` gratuito, fixado por digest. |

## 3. Arquitetura

```
Internet ──TLS──> proxy do servidor ──(rede externa "proxy")──> convenios-proxy (HAProxy :80)
                                                                   ├── /api/*     -> convenios-api :8080
                                                                   ├── /storage/* -> convenios-minio :9000 (sem o prefixo)
                                                                   └── /          -> convenios-web :3000
rede "app" (internal, sub-rede fixa, sem saída): db, minio, gotenberg, clamav, api, web, haproxy, jobs
rede "egress" (saída p/ internet): api (MT Login/JWKS, SMTP, ViaCEP, CNPJ), web (MT Login), clamav (freshclam)
```

| Serviço | Imagem | Observação |
|---|---|---|
| `db` | `postgres:16-alpine` | volume `pgdata`; sem porta publicada; healthcheck `pg_isready` |
| `migrate` | imagem da API | `/cli migrate:up`, uma execução |
| `seed` | imagem da API | `/cli seed` (idempotente, não destrutivo) cria roles e os Super Admins de `SEED_SUPER_ADMIN_CPF` |
| `minio` + `minio-init` | chainguard | bucket privado; console desligado |
| `convenios-api` | `backend/Dockerfile` (distroless, nonroot) | `read_only`, `cap_drop: ALL`, healthcheck `/healthcheck` |
| `convenios-web` | `frontend/Dockerfile` (Next standalone, usuário `node`) | `read_only` + tmpfs, healthcheck por `node` |
| `gotenberg` | `gotenberg/gotenberg:8` | só rede interna |
| `clamav` | `clamav/clamav:1.4` | `StreamMaxLength=200M`; volume das assinaturas; a API só sobe com ele saudável (a varredura falha fechada) |
| `cleanup` | alvo `tools` do `backend/Dockerfile` | `cli cleanup` diário (resolve o débito T1) |
| `backup` | `ops/backup/Dockerfile` | `pg_dump` diário, retenção em volume |
| `convenios-proxy` | `haproxy/Dockerfile` | publica `127.0.0.1:${HAPROXY_PORT}`; entra na rede externa do proxy |

Transversal: `restart: unless-stopped`, rotação de logs (`json-file`, 10 MB x 5), `stop_grace_period` de 40 s na API (o encerramento dela leva até 35 s), `no-new-privileges`, limites de memória medidos.

## 4. Variáveis

Obrigatórias (o compose falha com mensagem clara se faltarem): `APP_URL` (https), `POSTGRES_PASSWORD`, `JWT_API_SECRET`, `API_TOKEN_SALT`, `MINIO_ROOT_PASSWORD`, `MTLOGIN_*`, `JWKS_URI`, `OIDC_ISSUER`, `SEED_SUPER_ADMIN_CPF`, `SMTP_*`. Derivadas de `APP_URL`: `PUBLIC_URL`, `CORS_ALLOWED_ORIGINS`, `STORAGE_PUBLIC_URL` (`/storage`), `MTLOGIN_REDIRECT_URI` (`/auth/callback`), `NEXT_PUBLIC_APP_URL`. Modelo: `.env.prod.example`.

## 5. Plano de testes (tudo antes de entregar)

| Fase | O que | Critério de aceite |
|---|---|---|
| T1 | Estático | `docker compose config` sem erro com o `.env` de teste; falha com mensagem clara sem cada variável obrigatória; `haproxy -c` na imagem real |
| T2 | Build | As imagens constroem do zero (sem cache) a partir de um clone limpo do Git (simula o Portainer), com os `.dockerignore` |
| T3 | Subida | Todos os serviços `healthy`; `migrate`, `seed`, `minio-init` saem com 0; nenhum reinício em loop |
| T4 | Roteamento | `/` (Next), `/api/health`, `/api/ready`, `/storage` (MinIO), `/api/docs` = 404, `/minio-console` = 404, Host e `X-Forwarded-*` repassados |
| T5 | Autenticação | `/auth/sign-in` renderiza, `/auth/login` redireciona ao MT Login com `redirect_uri` correto, cookie `Secure` |
| T6 | Armazenamento | Upload e download por URL pré-assinada atrás do HAProxy com `Host` público; arquivo de 150 MB passa |
| T7 | Antivírus | Arquivo de teste EICAR é recusado; arquivo limpo passa; API recusa sem o `clamd` (fail-closed) |
| T8 | Conversão | Word -> PDF pelo Gotenberg a partir da API |
| T9 | IP do cliente | `X-Forwarded-For` simulado: o rate limit conta o IP certo, e um cabeçalho forjado não ganha |
| T10 | Segurança | `read_only` sem erro de escrita; usuários não-root; nenhuma porta publicada além de `127.0.0.1:HAPROXY_PORT`; DB/MinIO/ClamAV inalcançáveis de fora; sem segredo nas imagens (`docker history`) |
| T11 | Resiliência | `docker restart` de cada serviço e `down`/`up` mantêm os dados; derrubar o `db` e subi-lo faz a API se recuperar; `SIGTERM` encerra a API limpo |
| T12 | Backup | O `backup` gera o dump; **restauração** num banco limpo confere tabelas e contagens; retenção apaga os antigos |
| T13 | Limpeza | `cleanup` roda e termina sem erro |
| T14 | Atualização | Mudar o código, reconstruir e reimplantar mantém dados; migração nova roda sozinha |
| T15 | Rede | `api` e `web` alcançam a internet (MT Login/ViaCEP); `gotenberg`/`db`/`minio` não |
| T16 | Medição | `docker stats` sob carga leve para fixar os limites de memória |

## 6. Entregáveis

`docker-compose.prod.yml`, `.env.prod.example`, `backend/Dockerfile` (revisado), `backend/cmd/healthcheck`, `frontend/Dockerfile` (reescrito: o atual era do template Clerk e não construía este projeto), `haproxy/Dockerfile` + `haproxy/haproxy.prod.cfg`, `ops/backup/`, `ops/cleanup`/alvo `tools`, `docs/deploy-producao.md` (runbook: Portainer passo a passo, configuração do proxy, primeiro acesso, atualização, rollback, restauração).

## 7. Fora do escopo (avisos)

- Cliente real do MT-ID (R2) e o cadastro da URL de retorno de produção no client do Keycloak (`<APP_URL>/auth/callback`; pós-logout `<APP_URL>/auth/sign-in`): ações do responsável.
- Backup **externo** do Postgres e do bucket do MinIO: o serviço grava no mesmo servidor; copiar para fora é por conta da infraestrutura.
- Monitoramento/alertas e CI (T3).

## 8. Resultado dos testes (2026-11-08)

Todos os critérios da seção 5 passaram, exceto a ressalva acima. Destaques:

- T1: cada uma das 16 variáveis obrigatórias, removida sozinha, derruba o `config` com mensagem; segredo de desenvolvimento, `API_TOKEN_SALT` curto e `APP_URL` em http são recusados pela API.
- T2/T3: build do zero sem erros (API 77 MB, Next 447 MB, HAProxy 66 MB); subida com `migrate`, `seed` e `minio-init` em `exited (0)` e o resto `healthy`.
- T4/T5: roteamento e login conferidos; `/api/docs` e `/minio-console` não chegam ao backend; o redirecionamento ao MT Login leva o `redirect_uri` https e o cookie sai `Secure`; o CSRF do BFF aceita só a origem pública.
- T6/T7/T8: envio de 150 MB e download íntegro (SHA-256) pelo HAProxy com o `Host` público; URL adulterada e `Host` diferente = 403; EICAR detectado, fluxo de 150 MB varrido, Word convertido em PDF.
- T9: o 11º pedido do mesmo cliente leva 429 e outro cliente não é afetado; cabeçalho esquerdo forjado no `X-Forwarded-For` não escapa do limite.
- T10/T15: API, web, HAProxy e cleanup rodam sem root, com sistema de arquivos somente leitura e `cap_drop: ALL`; única porta publicada é `127.0.0.1:HAPROXY_PORT`; db, MinIO e Gotenberg sem saída para a internet; a rede do proxy alcança só o HAProxy.
- T11: com o banco parado, `/api/ready` dá 503 e a API volta sozinha; `SIGTERM` encerra a API em menos de 1 s com código 0; `down` + `up` preservam banco e backups.
- T12/T13: dump restaurado num banco novo bateu com o original; retenção apaga o antigo; falha de `pg_dump` não deixa arquivo; `cleanup` roda sem erro.
- T16: em regime, API 30 MB, web 126 MB, ClamAV 950 MB, db 31 MB, MinIO 53 MB; nenhum OOM nem reinício.

Correções feitas durante os testes: `seed` e `cleanup` ganharam a rede `egress` (sem ela o JWKS do MT Login gerava erro no log); o `.gitignore` ignorava o `.env.prod.example`.
