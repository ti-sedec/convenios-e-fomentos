# Convênios e Fomentos

Sistema de controle de **convênios, termos de fomento e prestação de contas**. O projeto tem uma API em Go com
controle de permissões granular (RBAC) e um painel administrativo em Next.js que a consome.

> **Estado atual:** a fundação está pronta (autenticação, administradores, roles e permissões, usuários finais, tokens de
> API, auditoria e um recurso de exemplo, *Documents*). As regras de negócio de convênios e fomentos ainda não foram
> implementadas; a legislação de referência está em [`docs/`](docs/).

## Sumário

1. [Visão geral](#1-visão-geral)
2. [Pré-requisitos](#2-pré-requisitos)
3. [Início rápido (Docker, recomendado)](#3-início-rápido-docker-recomendado)
4. [Primeiro acesso](#4-primeiro-acesso)
5. [Como o ambiente de desenvolvimento funciona](#5-como-o-ambiente-de-desenvolvimento-funciona)
6. [Rotina de trabalho](#6-rotina-de-trabalho)
7. [Rodando sem Docker (no host)](#7-rodando-sem-docker-no-host)
8. [Variáveis de ambiente](#8-variáveis-de-ambiente)
9. [Autenticação e e-mails em desenvolvimento](#9-autenticação-e-e-mails-em-desenvolvimento)
10. [Testes, lint e formatação](#10-testes-lint-e-formatação)
11. [Solução de problemas](#11-solução-de-problemas)
12. [Estrutura do repositório](#12-estrutura-do-repositório)
13. [Documentação adicional](#13-documentação-adicional)

---

## 1. Visão geral

| Parte | Tecnologia | Porta | Pasta |
|---|---|---|---|
| Proxy reverso | HAProxy (`/` → frontend, `/api` → API) | 80 (host) | [`haproxy/`](haproxy/) |
| Frontend | Next.js 16, React 19, TypeScript, Tailwind v4, shadcn/ui, TanStack Query/Form, Bun | 3000 (interna) | [`frontend/`](frontend/) |
| API | Go 1.27, PostgreSQL 16, sqlc, goose, JWT | 8080 (interna) | [`backend/`](backend/) |
| Banco | PostgreSQL 16 | 5432 | volume Docker |

```
                    ┌─ /      ──► Next.js (:3000) ──► API Go (:8080) ──► PostgreSQL
Navegador ─► HAProxy│              │  cookies httpOnly      │  JWT + RBAC
              (:80) │              └─ proxy /bff/*          └─ /api/admin/*  /api/auth/*  /api/docs
                    └─ /api/* ──► API Go (:8080)
```

O painel **nunca** fala direto com a API nem enxerga os tokens (a API em `/api` fica exposta para clientes externos): o login é feito pelo **MT Login** (Keycloak, OIDC com
Authorization Code + PKCE) e conduzido pelo servidor do Next, que guarda o access token e o refresh token em cookies `httpOnly`; as chamadas autenticadas passam pelo proxy `/bff/*`, que
injeta o `Authorization: Bearer` e renova o par de tokens quando necessário. A API só **valida** o token do MT Login
(assinatura via JWKS); quem é o administrador é decidido pelo **CPF** do token. Detalhes na
[seção 9](#9-autenticação-e-e-mails-em-desenvolvimento).

## 2. Pré-requisitos

Para o fluxo recomendado (Docker) basta:

- **Docker** com **Docker Compose v2** (`docker compose version`).
- Portas livres no host: **80** (HAProxy) e **5432** (ou outra, via `DB_PORT`; veja a [seção 11](#11-solução-de-problemas)).

Opcional, só para rodar fora do Docker ou executar os testes do backend:

- **Go 1.27+** (o `go.mod` fixa a versão; o Go baixa o toolchain correto sozinho).
- **Bun 1.x** (gerenciador e runtime do frontend; `npm`/`pnpm` também funcionam, mas o lockfile é do Bun).
- Ferramentas Go: `sqlc`, `goose` e `golangci-lint` (veja [`backend/README.md`](backend/README.md#2-subindo-o-ambiente-do-zero)).

## 3. Início rápido (Docker, recomendado)

Da raiz do repositório:

```bash
cp .env.example .env     # opcional: sem .env valem os padrões de desenvolvimento
docker compose -f docker-compose.dev.yml up --build
```

Toda a configuração do compose vem de variáveis (portas, credenciais do banco, URLs, segredos, SMTP...). Elas ficam no
`.env` da raiz, que o git ignora; o [`.env.example`](.env.example) lista todas, com comentários.

Na primeira vez isso constrói as imagens e instala as dependências (alguns minutos). Quando os logs mostrarem
`servidor iniciado` (API) e `Ready` (Next), acesse:

| O quê | URL |
|---|---|
| Painel (frontend) | <http://localhost> |
| API (base) | <http://localhost/api> |
| Documentação interativa da API (OpenAPI) | <http://localhost/api/docs> |
| Liveness / readiness da API | <http://localhost/api/health> · <http://localhost/api/ready> |

Dicas:

```bash
# Em segundo plano, e acompanhar os logs de um serviço
docker compose -f docker-compose.dev.yml up -d --build
docker compose -f docker-compose.dev.yml logs -f web api

# Evite repetir -f em todo comando
export COMPOSE_FILE=docker-compose.dev.yml
docker compose up -d

# Parar (mantém o banco) · parar e apagar banco e caches
docker compose down
docker compose down -v
```

## 4. Primeiro acesso

O login é sempre pelo **MT Login**; não há senha local. A identidade do administrador é o **CPF** (nome e e-mail são copiados
do MT Login a cada acesso). Quem entra pela primeira vez ganha uma conta ativa e **sem perfil**, e vê "Aguardando liberação"
até um administrador atribuir roles. Por isso o primeiro Super Admin precisa ser criado antes:

### Pré-requisitos (uma vez)

1. No `.env` da raiz, as variáveis `MTLOGIN_*`, `JWKS_URI` e `OIDC_ISSUER` (veja o [`.env.example`](.env.example)). Sem elas a
   API não sobe.
2. No client do Keycloak (`MTLOGIN_CLIENT_ID`), cadastrar a URL de retorno (`MTLOGIN_REDIRECT_URI`, por padrão
   `http://localhost/auth/callback`) e a de pós-logout (`http://localhost/auth/sign-in`).
3. Ter um usuário no MT Login **de homologação**, liberado pelo time do MT Login (CPF fora das bases oficiais precisa de liberação
   manual). O CPF vem no claim `preferred_username` (também há `cpf`); é o padrão de `OIDC_CPF_CLAIM`.

O `projeto-template-integracao` é o client de exemplo do MT Login, para testes. Cada aplicação deve ter o **seu** client, por ambiente
(HOMOL e PROD), solicitado à SEPLAG (`governancadigital@seplag.mt.gov.br`); veja
[`docs/documentacao-mti-app-cloud-mti-mt-gov-br-mt-login.md`](docs/documentacao-mti-app-cloud-mti-mt-gov-br-mt-login.md).

### Criar o primeiro Super Admin

Escolha **uma** das opções, com o seu CPF:

```bash
# CLI (idempotente; também promove quem já entrou como usuário comum)
docker compose exec api go run ./cmd/cli admin:create --cpf 000.000.000-00

# ou pelo seed: defina SEED_SUPER_ADMIN_CPF=000.000.000-00 (lista separada por vírgulas) no .env e rode
docker compose up -d api && docker compose exec api go run ./cmd/cli seed
```

Depois abra <http://localhost>, clique em **Entrar com MT Login** e autentique. No primeiro acesso o nome e o e-mail são
preenchidos a partir do MT Login e você já entra como Super Admin.

### Os demais administradores

Tudo isso é feito no painel, em **Administração → Usuários** (`/dashboard/administracao/usuarios`):

- **Pré-cadastro:** um administrador com a permissão `admin::settings.users.create` cadastra o **CPF** e as roles
  (botão **Pré-cadastrar**, ou `POST /api/admin/users`); no primeiro acesso da pessoa o registro é completado e já vem com as roles.
  Até lá o status é *Pré-cadastrado* (`pending`).
- **Aguardando liberação:** quem entra sem pré-cadastro vira administrador ativo sem roles (status `awaiting`). Eles aparecem num aviso
  no topo da lista, com o botão **Liberar acesso** (define as roles e o departamento de uma vez, `PUT /api/admin/users/{id}/roles`).
- **Perfis e permissões:** **Administração → Perfis e permissões** monta as roles marcando ações, com restrição por campos e por
  condições (é o criador, mesma role do criador, mesmo departamento). Quem não é Super Admin só concede permissões que possui, não altera
  as roles que tem nem a própria conta, e não age sobre quem tem mais poder (veja [`backend/SEGURANCA.md`](backend/SEGURANCA.md)).
- **Perfis criados por migração:** as migrações `00022`, `00023` e `00024` criam e ajustam os perfis abaixo (idempotentes: se o seed já criou a role,
  só entram as permissões que faltam). Os demais perfis (Super Admin, Editor, Author) vêm do seed. **A equipe só enxerga projeto enviado ou
  arquivado, nunca rascunho** (condições `admin::project-submitted` e `admin::project-submitted-same-department`); um perfil personalizado
  que dê leitura de projeto precisa incluir uma delas, senão reabre o acesso a rascunhos.

  | Código | Nome | Sistema | Descrição | Principais permissões |
  |---|---|:---:|---|---|
  | `proponent` | Proponente | sim | Quem entra pelo MT Login e cadastra projetos de convênio ou fomento. Recebe esta role automaticamente no primeiro acesso. | Cria projetos; lê, edita, envia, retira, compartilha e exclui os seus (criador ou membro/editor do projeto); edita o próprio perfil de proponente; lê catálogos e parâmetros de regra. |
  | `department-reviewer` | Revisor do Departamento | não | Vê os projetos enviados do próprio departamento. | Lê os projetos enviados do próprio departamento (sem departamento no cadastro do usuário, não vê nenhum); lê catálogos e parâmetros de regra. |
  | `administrative-supervisor` | Supervisor Administrador | sim | Acompanha todos os projetos enviados, somente leitura. Recebe e-mails sobre documentação regular, enquadramento recusado e arquivamento. | Lê os projetos enviados; lê catálogos e parâmetros de regra. Não decide nada. |
  | `analyst` | Analista | sim | Assume a análise dos projetos enviados, confere documentos e informações, devolve para complementação, arquiva por falta de documentos obrigatórios e administra os departamentos. | Lê os projetos enviados; pede complementação e arquiva (não nega); lê, edita e exclui departamentos (não cria); lê catálogos e parâmetros de regra. |
  | `state-secretary` | Secretário de Estado | sim | Vê todos os projetos enviados e pode negá-los a qualquer momento, arquivando-os na hora. | Lê os projetos enviados e nega; lê catálogos e parâmetros de regra. |

- A **Visão geral** do Proponente mostra o que pede ação dele (correções com prazo, rascunhos, convites), os projetos por situação, os recentes e a atividade, com o filtro "Todos / Meus / Compartilhados comigo" ([planejamento](docs/planejamento-visao-geral-proponente.md)).
- A **Visão geral** do Analista prioriza o trabalho (recusados pelo Revisor, reenviados, aguardando assumir, tudo aprovado sem departamento, parados além do limite por etapa e prazos a vencer), mostra a fila por etapa, a atividade da equipe e o resumo pessoal dele, com o filtro "Toda a equipe / Só os meus" ([planejamento](docs/planejamento-visao-geral-analista.md)).
- A **Visão geral** do Revisor do Departamento mostra os enquadramentos que esperam a decisão dele (os mais antigos primeiro, com o limite de "parado"), os números do departamento, a atividade do departamento e as decisões da própria pessoa; sem departamento no cadastro, a tela explica o motivo ([planejamento](docs/planejamento-visao-geral-revisor.md)).
- Também no painel: **Departamentos**, **Auditoria** (com o antes/depois de cada alteração), **Tokens de API** e o grupo **Portal público**
  (usuários finais, perfis públicos, configurações e templates de e-mail).
- O seed ainda cria, **só em desenvolvimento**, um administrador de exemplo por role (`super@`, `editor@`, `author@`,
  `revisor@` (Financeiro), `revisor.juridico@`, `analista@`, `supervisor@`, `secretario@dev.local`) com **CPFs fictícios**: servem para ver as listagens, mas não conseguem entrar (não existem no MT Login).
  O usuário final `usuario@dev.local` continua tendo senha, que aparece no log do seed apenas na primeira execução.

## 5. Como o ambiente de desenvolvimento funciona

O arquivo [`docker-compose.dev.yml`](docker-compose.dev.yml) define cinco serviços:

| Serviço | Imagem / build | O que faz |
|---|---|---|
| `db` | `postgres:16-alpine` | Banco `convenios_e_fomento` (usuário `sedec`, senha `adminadmin`), dados no volume `pgdata`. Credenciais por `POSTGRES_*`; porta publicada em `DB_PORT` (padrão 5432). |
| `migrate` | [`backend/Dockerfile.dev`](backend/Dockerfile.dev) | Aplica as migrações (`migrate:up`) e termina. A API só sobe depois que ele conclui. |
| `api` | [`backend/Dockerfile.dev`](backend/Dockerfile.dev) | Go com [air](https://github.com/air-verse/air): recompila e reinicia a cada alteração em `.go`, `.sql` ou `.yaml`. |
| `web` | [`frontend/Dockerfile.dev`](frontend/Dockerfile.dev) | `bun install` + `next dev` (Turbopack) com HMR. |
| `haproxy` | `haproxy:3.4.7-alpine` | Único ponto de entrada (porta `HAPROXY_PORT`, padrão 80): `/api/*` → `api`, `/storage/*` → MinIO (API S3), `/minio-console/*` → console do MinIO (só dev), o resto → `web`. Config em [`haproxy/haproxy.cfg`](haproxy/haproxy.cfg). |
| `minio` / `minio-init` | `chainguard/minio` / `chainguard/minio-client` | Armazenamento S3 dos arquivos dos documentos; o `minio-init` cria o bucket privado. As imagens `minio/*` deixaram de ser publicadas. O navegador envia e baixa por URLs pré-assinadas em `/storage` (mesmo domínio). **`APP_URL` (ou `STORAGE_PUBLIC_URL`) precisa ter a mesma origem, com porta, que o navegador usa**: a assinatura cobre o `Host`. |

**Live reload.** O código do host é montado nos contêineres (`./backend:/app`, `./frontend:/app`), então basta salvar o
arquivo: a API é recompilada em segundos e o Next atualiza a página sem recarregar. Não há `COPY` do código nas imagens de
desenvolvimento; elas só trazem as ferramentas.

**Volumes nomeados:** `pgdata` (banco), `go-mod` e `go-build` (caches do Go), `web-node-modules` e `web-next` (dependências e
cache do Next, mantidos dentro do Docker para não misturar com o host).

**Permissões de arquivo.** Os contêineres rodam com o UID/GID `1000` por padrão (o de um primeiro usuário Linux); assim
`backend/tmp`, `frontend/.next` e alterações em `go.sum` ficam com o seu usuário. Se o seu for outro (`id -u`, `id -g`), crie um
`.env` na raiz com `UID=1001` e `GID=1001` (o Compose o lê sozinho; o Bash não deixa sobrescrever `UID` na linha de comando).

**Configuração.** Tudo é parametrizável por variáveis (`.env` na raiz ou shell; o shell tem precedência) e tem um padrão de
desenvolvimento no compose, então nada é obrigatório. Os padrões que importam:

- `APP_URL` (`http://localhost`): origem pública da aplicação; dela derivam `PUBLIC_URL`, `CORS_ALLOWED_ORIGINS`,
  `NEXT_PUBLIC_APP_URL` e `MTLOGIN_REDIRECT_URI`. Os links dos e-mails dos usuários finais abrem o frontend.
- `TRUSTED_PROXIES=172.16.0.0/12`: o backend confia no `X-Forwarded-For` enviado pelo HAProxy e pelo Next (rede padrão do
  Docker), para que o rate limit de login considere o IP do usuário e não o do proxy.
- `DATABASE_URL`: montada a partir de `POSTGRES_USER`, `POSTGRES_PASSWORD` e `POSTGRES_DB` (host `db`, na rede do Docker).
  Defina-a explicitamente apenas para usar outro banco. **Atenção:** uma `DATABASE_URL` exportada no seu shell (modo sem Docker,
  apontando para `localhost`) sobrescreve a do `.env` e quebra os contêineres.
- `API_URL=http://api:8080` no frontend: o servidor do Next fala com a API direto pela rede do Docker, sem passar pelo HAProxy.
- Segredos (`JWT_*`, `API_TOKEN_SALT`): valores fixos de desenvolvimento, **nunca** use em produção.

## 6. Rotina de trabalho

Todos os comandos abaixo assumem `export COMPOSE_FILE=docker-compose.dev.yml`.

**Backend (Go)**

```bash
docker compose logs -f api                              # logs (JSON) da API
docker compose exec api go run ./cmd/cli migrate:status # situação das migrações
docker compose run --rm migrate                         # aplicar novas migrações
docker compose exec api go run ./cmd/cli migrate:down   # desfazer a última migração
docker compose exec api go run ./cmd/cli cleanup        # limpar tokens e sessões expirados
docker compose exec api go build ./...                  # checar compilação
docker compose restart api                              # reiniciar (raramente necessário)
```

- Criar uma migração: `goose -dir backend/internal/database/migrations create nome_da_migracao sql` (no host) e depois
  `docker compose run --rm migrate`. A API **não** aplica migrações sozinha.
- Alterou `internal/database/queries/*.sql` ou `sqlc.yaml`? Rode `make sqlc` em `backend/` (precisa do `sqlc` no host) e
  commite o código gerado.
- A especificação [`backend/api/openapi.yaml`](backend/api/openapi.yaml) é embutida na API; ao mudar rotas, atualize-a
  (há um teste que garante que ela cobre as rotas registradas).

**Frontend (Next)**

```bash
docker compose logs -f web
docker compose exec web bun add <pacote>        # adicionar dependência (atualiza package.json e bun.lock no host)
docker compose exec web bun run typecheck
docker compose exec web bun run lint
docker compose exec web bun run format
```

- A cada `up`, o `web` roda `bun install --frozen-lockfile`. Se alguém atualizou o `bun.lock`, basta
  `docker compose restart web`.
- Padrões do projeto (formulários, camada de API, ícones, nuqs) em [`frontend/CLAUDE.md`](frontend/CLAUDE.md) e
  [`frontend/AGENTS.md`](frontend/AGENTS.md).

**Banco de dados**

```bash
docker compose exec db psql -U sedec -d convenios_e_fomento           # console SQL
# ou, do host, com a porta publicada:
psql "postgres://sedec:adminadmin@localhost:5432/convenios_e_fomento"
docker compose down -v                                # recomeçar do zero (apaga TODOS os dados locais)
```

## 7. Rodando sem Docker (no host)

Útil para depurar com IDE. Mantenha só o PostgreSQL no Docker:

```bash
# 1. Banco
docker compose -f docker-compose.dev.yml up -d db     # DB_PORT=5433 no .env se a 5432 estiver ocupada

# 2. API (em backend/)
cd backend
cp .env.example .env                                  # ajuste DATABASE_URL se mudou a porta
make migrate-up
make seed                                             # opcional
make dev                                              # air (live reload); ou: make run

# 3. Frontend (em frontend/, outro terminal)
cd frontend
cp env.example.txt .env.local                         # API_URL=http://localhost:8080
bun install
bun run dev
```

Neste modo, aponte `PUBLIC_URL` do backend para `http://localhost:3000` (sem o HAProxy, o Next fica direto na 3000) no `backend/.env` para que os links dos e-mails
abram o frontend. Comandos do `Makefile` do backend: `run`, `dev`, `build`, `test`, `test-unit`, `cover-authz`, `lint`,
`sqlc`, `migrate-up`, `migrate-down`, `migrate-create name=...`, `seed`, `cleanup`, `docker-up`, `docker-down`.

> O [`backend/docker-compose.yml`](backend/docker-compose.yml) é uma stack **só do backend**, sem live reload (constrói a
> imagem de produção). Para o dia a dia, prefira o `docker-compose.dev.yml` da raiz.

## 8. Variáveis de ambiente

**Frontend** (`frontend/.env.local`; modelo em [`frontend/env.example.txt`](frontend/env.example.txt)). No Docker, o compose já define o necessário.

| Variável | Padrão | Descrição |
|---|---|---|
| `API_URL` | `http://localhost:8080` | URL da API usada **pelo servidor** do Next (proxy, server actions, `proxy.ts`). |
| `NEXT_PUBLIC_APP_URL` | — | URL pública do frontend (metadata). |
| `NEXT_PUBLIC_SENTRY_DISABLED` | `true` no compose | Desliga o Sentry em desenvolvimento. |
| `WATCHPACK_POLLING` | `false` | `true` força *polling* de arquivos (macOS/Windows, se o HMR não detectar mudanças). |

**Backend** (`backend/.env`; modelo em [`backend/.env.example`](backend/.env.example), lista completa na
[seção 6 do README do backend](backend/README.md#6-variáveis-de-ambiente)). As principais:

| Variável | Descrição |
|---|---|
| `APP_ENV` | `development` \| `test` \| `production`. Em produção os segredos viram obrigatórios. |
| `DATABASE_URL` | String de conexão do PostgreSQL. |
| `JWKS_URI`, `OIDC_ISSUER`, `OIDC_CLIENT_ID` | **Obrigatórias.** MT Login: chaves públicas, emissor e client que a API aceita nos tokens dos administradores. |
| `OIDC_CPF_CLAIM` | Claim do access token que traz o CPF (padrão `preferred_username`). |
| `SEED_SUPER_ADMIN_CPF` | CPFs (vírgula) que o `seed` torna Super Admin. |
| `JWT_API_SECRET`, `API_TOKEN_SALT` | Segredos (≥ 32 caracteres e distintos em produção). Os valores de exemplo são só para desenvolvimento. |
| `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_*` | Duração dos tokens (15 min / 7 dias de inatividade / 30 dias no máximo). |
| `CORS_ALLOWED_ORIGINS`, `TRUSTED_PROXIES`, `PUBLIC_URL` | CORS, proxies confiáveis (IP real) e URL base dos links de e-mail. |
| `SMTP_*` | Envio de e-mail; sem `SMTP_HOST` os e-mails são apenas registrados no log. |

**Compose de desenvolvimento** (`.env` na raiz; modelo em [`.env.example`](.env.example)):

| Grupo | Variáveis |
|---|---|
| Portas no host | `HAPROXY_PORT` (80), `DB_PORT` (5432); com outra porta, defina também `APP_URL` |
| Usuário do host | `UID`, `GID` (1000) |
| PostgreSQL | `POSTGRES_IMAGE`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `DATABASE_URL` (opcional) |
| Backend | `APP_ENV`, `LOG_LEVEL`, `PUBLIC_URL`, `CORS_ALLOWED_ORIGINS`, `TRUSTED_PROXIES`, `JWKS_URI`, `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CPF_CLAIM`, `SEED_SUPER_ADMIN_CPF`, `JWT_API_SECRET`, `API_TOKEN_SALT`, `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_MAX_TTL`, `REFRESH_TOKEN_IDLE_TTL`, `RATE_LIMIT_AUTH_PER_MINUTE`, `RATE_LIMIT_GLOBAL_PER_MINUTE`, `SMTP_*` |
| Frontend | `MTLOGIN_AUTHORIZATION_URI`, `MTLOGIN_TOKEN_URI`, `MTLOGIN_END_SESSION_URI`, `MTLOGIN_CLIENT_ID`, `MTLOGIN_CLIENT_SECRET` (só client confidencial), `MTLOGIN_REDIRECT_URI`, `API_URL`, `NEXT_PUBLIC_APP_URL`, `NEXT_PUBLIC_SENTRY_DISABLED`, `NEXT_TELEMETRY_DISABLED`, `WATCHPACK_POLLING` |

## 9. Autenticação e e-mails em desenvolvimento

**Sessão.** O login é Authorization Code + PKCE com o MT Login, conduzido por route handlers do Next:

1. `GET /auth/login` ([`route.ts`](frontend/src/app/auth/login/route.ts)) gera `state`, `nonce` e o verificador PKCE (cookie `httpOnly`
   de 10 min) e redireciona ao MT Login.
2. `GET /auth/callback` ([`route.ts`](frontend/src/app/auth/callback/route.ts)) confere o `state`, troca o `code` pelos tokens e os
   grava em cookies `httpOnly` + `SameSite=Lax` (`Secure` em produção): o access token (`cf_access`) e o refresh token (`cf_refresh`).
   Os tokens do Keycloak são grandes; acima de ~3,8 KB o login falha com a mensagem "excede o limite de armazenamento".
3. A primeira chamada autenticada à API cria (ou atualiza) o administrador pelo CPF do token.

- [`frontend/src/proxy.ts`](frontend/src/proxy.ts) protege `/dashboard/*`: sem cookie redireciona ao login; com o access token
  expirado, renova o par de tokens antes de renderizar.
- [`frontend/src/app/bff/[...path]/route.ts`](frontend/src/app/bff/[...path]/route.ts) é o proxy autenticado
  usado pelo navegador (`apiClient` em [`src/lib/api-client.ts`](frontend/src/lib/api-client.ts)). Só repassa `/bff/admin/*` para `/api/admin/*` e renova
  automaticamente em caso de 401.
- A renovação usa o refresh token no endpoint de token do MT Login e é **serializada por refresh token**, porque o Keycloak pode
  rotacionar o token a cada uso e revogar a sessão se um token já usado for reapresentado
  ([`src/lib/auth/refresh.ts`](frontend/src/lib/auth/refresh.ts)). Vale para um único processo do Next; várias instâncias
  exigiriam um lock distribuído.
- **Sair** limpa os cookies e vai ao `MTLOGIN_END_SESSION_URI`, encerrando também o SSO (o Keycloak pode pedir uma confirmação,
  porque não guardamos o `id_token` para enviar como `id_token_hint`).
- A documentação do MT Login mostra a resposta do `/token` **sem** `refresh_token` e `expires_in`. O frontend tolera isso: usa o `exp`
  do JWT e, sem refresh token, ao expirar o access token pede novo login (transparente enquanto o SSO estiver ativo).
- Conta desativada/bloqueada (ou token sem CPF válido) leva a `/auth/denied`; conta sem roles vê "Aguardando liberação".
- Permissões na interface: `useCan()` e `<Can>` leem as permissões do `/api/admin/auth/me`. Servem só para esconder elementos; quem
  autoriza de fato é sempre o backend.

**E-mails.** Os e-mails existem só para os **usuários finais** (confirmação de e-mail e redefinição de senha). Sem `SMTP_HOST`,
a API não envia nada: registra o e-mail no log, com o link completo (`docker compose logs api | grep "e-mail (modo desenvolvimento)"`).
Os tokens são de uso único. Administradores não recebem e-mails: não há senha nem convite.

## 10. Testes, lint e formatação

**Backend** (rodam no host; os testes de integração sobem um PostgreSQL próprio via *testcontainers*, então exigem Docker
acessível e Go instalado):

```bash
cd backend
make test          # tudo, com -race
make test-unit     # só testes unitários (-short, sem Docker)
make cover-authz   # exige >= 80% de cobertura no pacote authz
make lint          # golangci-lint
```

**Frontend** (no host ou via `docker compose exec web ...`):

```bash
cd frontend
bun run typecheck
bun run lint            # oxlint (avisos não quebram; erros sim)
bun run format:check    # oxfmt; use `bun run format` para corrigir
bun run build           # build de produção (valida rotas e tipos)
```

O frontend ainda não tem suíte de testes automatizados; a verificação de ponta a ponta é feita manualmente no navegador.

## 11. Solução de problemas

| Sintoma | Causa / solução |
|---|---|
| `port is already allocated` / `address already in use` na **5432** | Há um PostgreSQL local. Defina `DB_PORT=5433` no `.env` (a API e o `web` falam com o `db` pela rede interna, então nada mais muda). |
| `80` ocupada | Pare o processo que a usa (`ss -ltnp \| grep ':80 '`) ou defina `HAPROXY_PORT=8000` e `APP_URL=http://localhost:8000` no `.env` (e cadastre as novas URLs no Keycloak). |
| Alterações em arquivos não recarregam (macOS/Windows) | `WATCHPACK_POLLING=true` para o frontend; para a API, defina `poll = true` em `[build]` do `backend/.air.toml`. |
| `permission denied` em `tmp/`, `.next/` ou `go.sum` | Arquivos criados como root por uma execução anterior. `sudo chown -R $USER backend/tmp frontend/.next` e confira `UID`/`GID`. |
| API não sobe: `JWKS_URI é obrigatório...` | Faltam as variáveis do MT Login no `.env` (veja a seção 4). |
| "Entrar" mostra "não está configurado" | Faltam `MTLOGIN_*` no `.env`; depois `docker compose up -d web`. |
| Erro do Keycloak sobre `redirect_uri` | A URL de `MTLOGIN_REDIRECT_URI` (ou a de pós-logout) não está cadastrada no client. |
| Entra no MT Login mas cai em `/auth/denied` (`invalid_identity`) | O claim de `OIDC_CPF_CLAIM` não traz um CPF válido; confira o token. |
| Entra e vê "Aguardando liberação" | Conta nova sem roles: peça a um Super Admin (ou use `admin:create --cpf`, seção 4). |
| API sobe e cai com erro de conexão ao banco | O banco ainda iniciava; `docker compose logs db api`. O `migrate` só libera a API com o `db` saudável. |
| `429 rate_limited` no login de usuário final | Muitas tentativas (limite por IP+e-mail). Aguarde um minuto. |
| Erros de tipos estranhos referindo `.next/types` | Cache gerado antigo: `docker compose rm -sf web && docker volume rm convenios-dev_web-next`, ou `rm -rf frontend/.next` no modo host. |
| Mudou dependências e o `web` não enxerga | `docker compose restart web` (roda `bun install` de novo). Em último caso, `docker volume rm convenios-dev_web-node-modules`. |
| Logout inesperado ao usar várias instâncias do Next | A renovação de tokens é serializada por processo; veja a [seção 9](#9-autenticação-e-e-mails-em-desenvolvimento). |

## 12. Estrutura do repositório

```
.
├── docker-compose.dev.yml      # ambiente completo de desenvolvimento (live reload)
├── .dockerignore
├── backend/                    # API em Go
│   ├── cmd/api, cmd/cli        # servidor HTTP e CLI (migrate, seed, admin:create --cpf, cleanup...)
│   ├── internal/
│   │   ├── app/                # montagem do grafo de dependências + testes de integração
│   │   ├── auth/ admin/        # autenticação e gestão de administradores
│   │   ├── authz/              # motor de permissões (ações, campos, condições)
│   │   ├── enduser/ apitoken/  # usuários finais e tokens de API
│   │   ├── document/           # recurso de exemplo
│   │   ├── audit/ httpapi/ ... # auditoria, roteador, middlewares, config, mailer
│   │   └── database/           # migrações (goose) e queries (sqlc)
│   ├── api/openapi.yaml        # contrato da API (servido em /api/docs)
│   ├── Dockerfile, Dockerfile.dev, docker-compose.yml, .air.toml, Makefile
│   └── README.md, SEGURANCA.md
├── frontend/                   # painel Next.js
│   ├── src/app/                # rotas: /auth/*, /dashboard/*, /bff/[...path]
│   ├── src/features/auth/      # logout, sessão, permissões no cliente, telas de acesso
│   ├── src/lib/auth, src/lib/api, src/lib/api-client.ts
│   ├── src/proxy.ts            # guarda de rotas e renovação de tokens
│   ├── src/components/         # UI (shadcn/ui), layout, formulários, data-table
│   └── Dockerfile.dev, Dockerfile, Dockerfile.bun, env.example.txt
└── docs/                       # legislação e material de referência (convênios e termos de fomento)
```

## 13. Documentação adicional

- API e controle de acesso: [`backend/README.md`](backend/README.md) (arquitetura, receitas para nova ação/condição/policy/recurso,
  decisões de projeto) e [`backend/SEGURANCA.md`](backend/SEGURANCA.md) (revisão de segurança e pendências).
- Contrato da API: <http://localhost/api/docs> com o ambiente no ar, ou [`backend/api/openapi.yaml`](backend/api/openapi.yaml).
- Frontend: [`frontend/CLAUDE.md`](frontend/CLAUDE.md), [`frontend/AGENTS.md`](frontend/AGENTS.md) e [`frontend/docs/`](frontend/docs/)
  (formulários e temas). O `frontend/Dockerfile` (produção) já é o do projeto; o `frontend/Dockerfile.bun` e o
  `frontend/docs/deployment.md` ainda são do template (Clerk) e não valem para este deploy.
- **Produção (Portainer)**: [`docker-compose.prod.yml`](docker-compose.prod.yml), [`.env.prod.example`](.env.prod.example) e o passo a passo em
  [`docs/deploy-producao.md`](docs/deploy-producao.md) (planejamento e testes em [`docs/planejamento-deploy-producao.md`](docs/planejamento-deploy-producao.md)).
- Legislação e referências do domínio: [`docs/`](docs/).
- O frontend parte do template [Kiranism/next-shadcn-dashboard-starter](https://github.com/Kiranism/next-shadcn-dashboard-starter)
  (licença MIT).
