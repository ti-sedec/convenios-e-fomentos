# Convênios e Fomentos — Backend

API em Go cujo centro é um **controle de permissões robusto, granular e configurável em tempo de execução**, inspirado no
Strapi 5: RBAC completo para administradores, *Users & Permissions* para usuários finais e tokens de API para integrações.
Não há frontend: tudo o que o painel do Strapi faz está disponível por endpoints `/api/admin/...`, para um frontend futuro apenas consumir.

- Documentação interativa da API: `GET /api/docs` (especificação em [`api/openapi.yaml`](api/openapi.yaml), OpenAPI 3.1).
- Revisão de segurança e pendências: [`SEGURANCA.md`](SEGURANCA.md).

## Sumário

1. [Visão geral da arquitetura](#1-visão-geral-da-arquitetura)
2. [Subindo o ambiente do zero](#2-subindo-o-ambiente-do-zero)
3. [Criando o primeiro Super Admin](#3-criando-o-primeiro-super-admin)
4. [Como o controle de acesso funciona](#4-como-o-controle-de-acesso-funciona)
5. [Receitas: nova ação, condição, policy e recurso](#5-receitas-nova-ação-condição-policy-e-recurso)
6. [Variáveis de ambiente](#6-variáveis-de-ambiente)
7. [Dependências](#7-dependências)
8. [Estrutura de pastas](#8-estrutura-de-pastas)
9. [Testes e qualidade](#9-testes-e-qualidade)
10. [Operação](#10-operação)
11. [Decisões de projeto](#11-decisões-de-projeto)
12. [Próximos passos](#12-próximos-passos)

---

## 1. Visão geral da arquitetura

Três tipos de sujeito, cada um com seu domínio de permissões:

| Sujeito | Entra por | Autenticação | Permissões |
|---|---|---|---|
| **Administrador** | `/api/admin/...` | Access token do **MT Login** (Keycloak/OIDC, RS256, validado pelo JWKS); identidade = **CPF** | RBAC completo: **ação + recurso + campos + condições**, união de várias roles |
| **Usuário final** | `/api/...` | JWT `aud=api` + refresh token; sem token = anônimo (role **Public**) | Por **ação** (`api::recurso.acao`), uma única role; propriedade via **policies** |
| **Token de API** | `/api/...` | `Authorization: Bearer gat_...` | Tipo `read-only`, `full-access` ou `custom` (lista de ações) |

```
Requisição
 └─ httpapi: ID da requisição → IP real (proxies confiáveis) → limite por IP → logs → recover
             → cabeçalhos de segurança → limite de corpo → CORS
     ├─ /api/admin/*  auth.RequireAdmin  (token do MT Login + administrador ativo no banco)
     │            admin.RequirePermission  → authz.Engine.Can(ação) → Decision no contexto
     │            handler fino → service (regra de negócio, aplica filtro/campos) → sqlc
     └─ /api/*    enduser.Identifier.Identify  (anônimo/Public | JWT aud=api | gat_...)
                  Identifier.RequireAction   → authz.Engine.Can(ação)
                  policy.Require(...)        → regras em código (ex.: is-owner)
                  handler fino → service → sqlc
```

Regras de arquitetura: handlers finos → services com a regra de negócio → repositórios (sqlc, SQL puro); injeção de
dependências manual (`internal/app` monta o grafo e é usado por `cmd/api`, pela CLI e pelos testes); interfaces só onde há
necessidade real (`Store`, `Broadcaster`, `Mailer`, `Provider`); `context.Context` em todas as camadas; *graceful shutdown*.

### Formato das respostas

- Sucesso: `{ "data": ..., "meta": { "page", "pageSize", "total", "pageCount" } }`.
- Erro: `{ "error": { "status": 403, "code": "forbidden", "message": "Você não tem permissão...", "details": {...} } }`.
  O `code` é estável e em inglês; a `message`, em português.
- Códigos: 400, 401, 403, 404, 409, 422 (com a lista de campos), 429 e 500. O 500 nunca vaza detalhes: devolve o `requestId`
  (cabeçalho `X-Request-ID`) e o erro real fica só no log.

---

## 2. Subindo o ambiente do zero

Pré-requisitos: Go (o `go.mod` fixa a versão; a especificação exige no mínimo 1.25 e o Go baixa o toolchain correto sozinho), Docker e Docker Compose.

```bash
cd backend
cp .env.example .env                       # valores seguros apenas para desenvolvimento

# Ferramentas (uma vez)
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
# golangci-lint: instalação oficial (binário) — https://golangci-lint.run/welcome/install/
# opcional (hot reload): go install github.com/air-verse/air@latest

# Opção A — tudo em contêineres (PostgreSQL + migração + API)
make docker-up                             # API em http://localhost:8080
docker compose run --rm --entrypoint /cli api seed

# Opção B — API no host, só o PostgreSQL em contêiner
docker compose up -d db
make migrate-up
make seed                                  # roles, permissões, departamentos e, em dev, um usuário por role
make run                                   # ou: make dev (air)
```

Verificações: `GET /api/health` (liveness), `GET /api/ready` (consulta o banco), `GET /api/docs`.
Se a porta 5432 do host estiver ocupada: `DB_PORT=5433 docker compose up -d db` e ajuste a `DATABASE_URL`.

Comandos do `Makefile`: `run`, `dev`, `build`, `test`, `test-unit`, `cover-authz`, `lint`, `sqlc`, `migrate-up`,
`migrate-down`, `migrate-create name=...`, `seed`, `cleanup`, `docker-up`, `docker-down`.

### Seed (`make seed`)

Idempotente e **não destrutivo**: rodar de novo não duplica nada nem desfaz o que os administradores mudaram.

- Roles de admin **Super Admin**, **Editor**, **Author**, **Proponente** (`proponent`), **Revisor do Departamento**
  (`department-reviewer`), **Analista** (`analyst`), **Supervisor Administrador** (`administrative-supervisor`) e **Secretário de
  Estado** (`state-secretary`). As permissões padrão só são aplicadas quando a role é criada; os perfis de equipe vêm das migrações
  `00022` a `00024` (o seed declara os mesmos valores, e um teste confere que batem).
  - Editor: cria, lê, atualiza, exclui e publica qualquer documento.
  - Author: cria e lê; atualiza e exclui só o que criou (`admin::is-creator`); não publica.
  - Revisor: lê só os projetos enviados do próprio departamento (`admin::project-submitted-same-department`) e **confirma ou recusa o
    enquadramento** (`review-framing`; vale a primeira decisão de qualquer Revisor do departamento); sem departamento no cadastro, não lê
    nenhum (403). Não tem mais permissões sobre o módulo genérico de documentos.
  - Analista: lê projetos enviados, inicia a análise (`start-analysis`) e **assume o projeto de outro analista** (`take-over`; só o analista responsável confere, devolve, arquiva e indica, os outros apenas visualizam; `not_responsible_analyst`), confere os itens (`review-item`), **indica o departamento** de enquadramento (`assign-department`, só com tudo aprovado), pede complementação e arquiva; lê, edita e exclui departamentos (não cria). Aprovar um documento com arquivo exige que ele mesmo o tenha aberto (`project_file_views`); aprovações são por item e a devolução zera só os itens devolvidos.
  - Supervisor Administrador: lê projetos enviados, somente leitura.
  - Secretário de Estado: lê projetos enviados e nega (arquiva na hora).
  - A equipe nunca lê rascunho: todas as ações de projeto da equipe levam `admin::project-submitted` (ou a variante com departamento).
  - Proponente: lê os catálogos (tipos de entidade, municípios, parlamentares) e os parâmetros de regras, edita o próprio perfil e
    cria/edita/exclui só os projetos que criou (`admin::is-creator`). Como o conjunto cresce a cada fase, o seed **acrescenta** à role
    existente as permissões padrão que faltam (sem alterar nem remover as que já tem). **Quem entra pelo MT
    Login pela primeira vez recebe esta role automaticamente** (se ela já existir), em vez de ficar sem nenhuma. O seed também a
    dá a quem já tinha entrado e está sem nenhuma role.
- Roles de usuário final **Public** (`find`, `findOne`) e **Authenticated** (também `create`, `update`, `delete`), aplicadas uma
  única vez (marcador `seed.enduser-permissions`). Configurações avançadas e templates de e-mail padrão em português.
- Departamentos de exemplo: Financeiro e Jurídico. Em desenvolvimento nascem como **área finalística** (`is_finalistic`); fora dele nascem sem a marca, e quem administra marca os que podem receber projetos para enquadramento. O seed nunca altera a marca de um departamento que já existe.
- **Cadastros de referência** — tipos de entidade, os 5.571 municípios do IBGE e os parâmetros de regras com os valores das normas —
  são carregados pelo próprio servidor ao subir (`App.Start`), só no que ainda não existe; não precisam do seed.
- Os Super Admins listados em `SEED_SUPER_ADMIN_CPF` (qualquer ambiente).
- **Somente com `APP_ENV=development`**: um administrador de exemplo por role (`super@`, `editor@`, `author@`, `revisor@` (Financeiro), `revisor.juridico@`, `analista@`, `supervisor@`, `secretario@dev.local`)
  com **CPFs fictícios** (não entram pelo MT Login; `seed.DevUsers`) e o usuário final `usuario@dev.local`, com senha aleatória
  **exibida no log só na primeira execução**.
- Avisa as instâncias em execução (LISTEN/NOTIFY) para recarregarem as permissões.

---

## 3. Criando o primeiro Super Admin

Os administradores entram pelo **MT Login**; não há senha local. A identidade é o **CPF** do access token (claim
`OIDC_CPF_CLAIM`). Quem entra sem registro é criado no primeiro acesso, ativo e **sem roles**, então o primeiro Super Admin
precisa ser garantido por fora:

```bash
go run ./cmd/cli admin:create --cpf 000.000.000-00
# no Docker:
docker compose run --rm --entrypoint /cli api admin:create --cpf 000.000.000-00
# ou pelo seed (lista separada por vírgulas): SEED_SUPER_ADMIN_CPF=000.000.000-00 go run ./cmd/cli seed
```

O comando é idempotente: cria o registro (nome e e-mail ficam vazios até o primeiro acesso) ou concede a role a quem já existe.
Outros comandos: `migrate:up|down|status`, `seed`, `cleanup`.

Os demais: `POST /api/admin/users` pré-cadastra por **CPF + roles** (a pessoa completa o registro no primeiro acesso), ou basta a pessoa
entrar e um administrador atribuir as roles depois. Nome, CPF e e-mail vêm do MT Login e são atualizados a cada acesso.

---

## 4. Como o controle de acesso funciona

### Ações

Toda ação protegida é uma string `dominio::recurso.acao` (`admin::content.document.update`, `api::document.find`),
**registrada em código** no `authz.Registry` na inicialização, com nome legível em português, categoria
(`collection-types`, `single-types`, `plugins`, `settings`, `application`), recurso, suporte a campos/condições/locales
e, para `api::`, método HTTP e rota vinculada. **Ação não registrada sempre nega.** `GET /api/admin/catalog/actions` lista tudo
agrupado por categoria (base da futura tela de permissões). Na subida, permissões de ações que saíram do código são removidas
(com aviso no log).

### Permissões de administrador

`action` + `subject` + `fields` (`null` = todos; `[]` = nenhum) + `conditions` + `properties`.

- Um administrador pode ter **várias roles**: a permissão efetiva é a **união** (OR) das permissões de todas as roles.
- Dentro de **uma** permissão, **várias condições combinam com OR** (mesmo comportamento do Strapi). Permissão sem condição é
  incondicional e prevalece sobre as demais no filtro.
- **Super Admin** é curinga (sem linhas de permissão), não tem permissões alteráveis, não pode ser excluído nem duplicado, e
  sempre sobra ao menos um administrador **ativo** com ele.
- `Engine.Can(ctx, sujeito, ação, recurso?)` devolve uma `Decision`: `Allowed`, `Filter` (aplicar no WHERE das listagens),
  `Fields` e `FieldsFor(registro)` — os campos de **um registro específico** (permissões diferentes podem liberar campos
  diferentes conforme o filtro de cada uma casa com o registro). `AllowsRecord(registro)` confere um registro único.

### Condições

Registradas em código (`authz.ConditionRegistry`). O handler recebe o sujeito e devolve **permitir**, **negar** ou um **filtro**.
Padrão: `admin::is-creator`, `admin::has-same-role-as-creator` e a customizada de exemplo `admin::same-department`.
O filtro é tipado (`Eq`, `Neq`, `In`, `And`, `Or`) e só vira SQL por `Filter.ToSQL`: sempre parametrizado (`$n`) e **somente com
colunas declaradas no `Schema` do recurso**; campo fora da lista é erro, nunca SQL. O mesmo filtro avalia em memória
(`Filter.Match`) para registros únicos. Convenção: recursos que usem as condições padrão expõem `created_by_id` e `department_id`.

### Campos

O `authz.Schema` é lido das tags da struct do recurso (`json`, `db`, `authz:"system"`) — sem código por recurso.
**Leitura:** `Schema.Project` remove o que não é permitido (campos de sistema sempre saem; chaves fora do schema nunca saem).
**Escrita:** `Schema.RejectedWrite` lista os campos não permitidos → **422** com a lista; nada é gravado. Campos de sistema
(`id`, datas, autoria) nunca são graváveis pelo cliente, nem pelo Super Admin.

### Cache e múltiplas instâncias

As permissões por role ficam em memória; qualquer mudança chama `Engine.Invalidate`, que limpa o cache local **na hora** e
publica via **LISTEN/NOTIFY** do PostgreSQL (`PGBroadcaster`, atrás da interface `Broadcaster`) para as outras instâncias. A
escuta reconecta sozinha e invalida tudo a cada reconexão. Em instância única basta `LocalBroadcaster`.

### Usuários finais, policies e tokens de API

- Sem `Authorization` → anônimo, com a role **Public**; JWT `aud=api` → usuário final (uma role); `gat_...` → token de API;
  cabeçalho presente porém inválido → 401 (nunca "cai" em anônimo). Sem permissão: anônimo → **401**, autenticado → **403**.
- **Policies** (`internal/policy`): funções executadas depois da permissão, compostas na rota (`policy.Require(...)`,
  `policy.All/Any`). Exemplo pronto: `policy.IsOwner(lookup)`; negação → 403 com `details.policy`.
- **Tokens de API**: `gat_` + 256 bits, exibidos **uma única vez**; no banco, só o HMAC-SHA256 com `API_TOKEN_SALT` (trocar o
  salt invalida todos os tokens). `read-only` = ações GET/HEAD; `full-access` = qualquer ação `api::`; `custom` = lista.
  Duração 7d/30d/90d/ilimitada; regenerar invalida o anterior; revogar é definitivo. Revogação, expiração e edição valem na
  hora (cada requisição consulta o banco). `last_used_at` é gravado fora da requisição, no máximo uma vez por minuto.

### Autenticação (administradores e usuários finais)

**Administradores.** O frontend conduz o login no MT Login (Authorization Code + PKCE) e envia o access token no `Authorization:
Bearer`. A API **só valida**: assinatura RS256 pelas chaves do `JWKS_URI` (cache com releitura ao aparecer um `kid` novo), emissor
(`OIDC_ISSUER`), expiração, tipo `Bearer` (recusa token de ID/refresh) e client (`azp`/`aud` = `OIDC_CLIENT_ID`). Em seguida o CPF
identifica o administrador: se não existe, é criado (ativo, sem roles, com auditoria `admin.auto-provisioned`); se existe, nome e
e-mail são atualizados. **A cada requisição o banco confirma que a conta segue ativa e não foi bloqueada** (efeito imediato; 403
`account_disabled`). Token válido sem CPF válido: 403 `invalid_identity`. A API não guarda sessão de administrador: renovação e
encerramento são do MT Login. O CPF só aparece completo no `/api/admin/auth/me` do próprio usuário; listagens e auditoria o mascaram.

**Usuários finais.** Access token JWT HS256 de 15 min com `aud=api` que carrega o ID da sessão; o banco confirma a cada requisição que
a sessão segue ativa e a conta não foi bloqueada (revogação imediata, sem cache). Refresh token opaco de 32 bytes (guardado como hash
SHA-256) com **rotação** e **detecção de reutilização** (token já usado → revoga toda a família). Expiração por ociosidade e por tempo
máximo. Senhas em Argon2id com comparação em tempo constante e login com mensagem genérica (e verificação feita mesmo quando o e-mail
não existe).

---

## 5. Receitas: nova ação, condição, policy e recurso

Tudo abaixo usa o recurso de exemplo `documents` (`internal/document`) como referência.

### 5.1 Registrar uma nova ação (ex.: arquivar documentos)

1. **Declare e registre** em `internal/document/model.go`:
   ```go
   const ActionAdminArchive = "admin::content.document.archive"
   // dentro de Register(reg):
   reg.MustRegister(authz.Action{
       ID: ActionAdminArchive, Domain: authz.DomainAdmin, Category: authz.CategoryCollectionTypes,
       Name: "Arquivar documentos", Resource: Resource, SupportsConditions: true,
   })
   ```
   O identificador precisa casar com `dominio::recurso.acao`; o `Register` rejeita formato inválido, duplicata ou domínio
   divergente do prefixo (e a aplicação nem sobe).
2. **Proteja a rota** com o middleware da ação (autentica + autoriza e deixa a `Decision` no contexto):
   ```go
   r.With(g(ActionAdminArchive)).Post("/api/admin/content/documents/{id}/archive", h.archive)
   ```
3. **No handler/serviço**, aplique a decisão ao registro (`dec, _ := admin.DecisionFrom(r.Context())`):
   ```go
   if !dec.AllowsRecord(doc.Getter()) { return httpapi.ErrForbidden() }
   ```
4. **Conceda** a quem deve ter: `PUT /api/admin/roles/{id}/permissions` (ou nas permissões padrão do seed, se for o caso). Para o
   front, a ação já aparece em `GET /api/admin/catalog/actions`.
5. **Documente e teste**: adicione a rota ao `api/openapi.yaml` (o teste `TestOpenAPIEstaCoerenteComAsRotas` falha se rota e
   especificação divergirem) e um teste de integração com 401, 403 e o caso permitido.

Para ações do domínio `api::`, use `Domain: authz.DomainAPI` e informe `Method` e `Route` (a "rota vinculada" que o
admin vê ao editar a role) e proteja a rota com `identifier.RequireAction(ActionX)`.

### 5.2 Registrar uma nova condição (ex.: só documentos em rascunho)

Em `internal/app/app.go`, junto de `authz.RegisterStandardConditions(...)`:

```go
conds.MustRegister(authz.Condition{
    ID: "admin::is-draft", Name: "Está em rascunho", Category: "custom",
    Handler: func(ctx context.Context, s authz.Subject) (authz.Result, error) {
        return authz.Restrict(authz.Eq("status", "rascunho")), nil   // ou authz.Allow() / authz.Deny()
    },
})
```

- O handler devolve `Allow()` (sem filtro), `Deny()` ou `Restrict(filtro)`. Para depender do banco (como
  `has-same-role-as-creator`), injete uma interface no construtor da condição.
- Os campos do filtro são **nomes lógicos do `Schema`** do recurso (`status` aqui). Condição que cite um campo que o recurso
  não declara falha de forma segura (erro), nunca vira SQL.
- A condição aparece em `GET /api/admin/catalog/conditions` e passa a ser aceita em permissões de ações com
  `SupportsConditions: true` (várias na mesma permissão = OR).

### 5.3 Criar uma nova policy (ex.: só documentos publicados)

```go
// internal/policy ou no pacote do recurso
isPublished := policy.New("is-published", func(c policy.Context) (bool, error) {
    id, err := uuid.Parse(chi.URLParam(c.Request, "id"))
    if err != nil { return false, httpapi.ErrNotFound() }
    d, err := svc.Get(c.Request.Context(), id)          // devolva *httpapi.APIError para 404 etc.
    return err == nil && d.Status == "publicado", err
})

r.With(
    identifier.RequireAction(ActionAPIUpdate),   // 1º: permissão da ação
    policy.Require(policy.IsOwner(lookup), isPublished), // 2º: policies, em ordem; todas devem permitir
).Put("/api/documents/{id}", h.update)
```

Policies recebem a requisição e o sujeito (`policy.Context`). Combine com `policy.All` / `policy.Any`. Falha → 403 com o nome
da policy em `details.policy`. Em `internal/document/public_handler.go` há o uso real de `is-owner`.

### 5.4 Adicionar um novo recurso protegido

1. **Migração** (`make migrate-create name=criar_tabela_x`) e **queries** em `internal/database/queries/*.sql` → `make sqlc`.
2. **Modelo + Schema**: struct com tags `json`/`db`/`authz:"system"`; `authz.NewSchema(...)`.
3. **Ações**: `Register(reg)` com as ações `admin::` (campos/condições conforme fizer sentido) e `api::` (com rota), mais
   `reg.RegisterSchema(recurso, schema)` — isso habilita a validação de campos em `PUT /api/admin/roles/{id}/permissions`.
4. **Service**: recebe a `Decision` e a aplica: `Filter` na listagem (`Filter.ToSQL(schema.Column, n)` **combinado com AND** aos
   filtros do usuário), `AllowsRecord` em leitura/edição/exclusão, `FieldsFor` + `Schema.Project` na leitura e
   `Schema.RejectedWrite` na escrita. Ordenação só por lista permitida (`httpapi.ParseSort`).
5. **Handlers finos** e rotas com `Guard(ação)` (admin) ou `Identifier.RequireAction` + `policy.Require` (público).
6. **Fiação** em `internal/app/app.go` (`Register`, serviço, handlers, `Routes`).
7. **Permissões padrão** no `internal/seed` (opcional) e **OpenAPI**.
8. **Testes**: unitários do que for puro e de integração (401/403/422, campos, filtro, paginação).
   Para respostas públicas, use uma struct de visão dedicada (como `PublicDocument`): o que não existe na struct não vaza.

---

## 6. Variáveis de ambiente

Documentadas em [`.env.example`](.env.example) (um teste garante que todas as variáveis lidas estão lá, e vice-versa).
Em produção (`APP_ENV=production`) a aplicação **não sobe** com segredos ausentes, com menos de 32 caracteres, com o valor de
desenvolvimento, nem com Argon2 abaixo do mínimo da OWASP. A API também não sobe sem `JWKS_URI`, `OIDC_ISSUER`,
`OIDC_CLIENT_ID` e `OIDC_CPF_CLAIM` (a CLI não exige).

| Variável | Padrão | Descrição |
|---|---|---|
| `APP_ENV` | `development` | `development`, `test` ou `production`. Só fora de produção o `.env` é carregado. |
| `HTTP_PORT` | `8080` | Porta HTTP. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` ou `error`. Logs em JSON com `request_id`. |
| `PUBLIC_URL` | `http://localhost:8080` | URL pública do servidor (links de convite, confirmação e redefinição). |
| `DATABASE_URL` | `postgres://app:app@localhost:5432/app?sslmode=disable` | Conexão PostgreSQL 16+. |
| `JWKS_URI` | *(obrigatório)* | URL do JWKS do MT Login (chaves que assinam os tokens dos administradores). |
| `OIDC_ISSUER` | *(obrigatório)* | Emissor (`iss`) esperado nos tokens. |
| `OIDC_CLIENT_ID` | *(obrigatório)* | Client do Keycloak; o token precisa ter sido emitido para ele. |
| `OIDC_CPF_CLAIM` | `preferred_username` | Claim do access token que traz o CPF. |
| `SEED_SUPER_ADMIN_CPF` | *(vazio)* | CPFs (vírgula) que o `seed` torna Super Admin. |
| `JWT_API_SECRET` | *(dev)* | Segredo do JWT de usuário final. **Obrigatório e forte em produção.** |
| `API_TOKEN_SALT` | *(dev)* | Chave do HMAC dos tokens de API. Trocar invalida todos os tokens. |
| `ACCESS_TOKEN_TTL` | `15m` | Validade do access token. |
| `REFRESH_TOKEN_MAX_TTL` | `720h` | Tempo máximo de uma sessão de usuário final, desde o login. |
| `REFRESH_TOKEN_IDLE_TTL` | `168h` | Inatividade máxima entre renovações (usuário final). |
| `RESET_TOKEN_TTL` | `30m` | Validade do link de redefinição de senha do usuário final. |
| `EMAIL_CONFIRMATION_TTL` | `72h` | Validade do link de confirmação de e-mail. |
| `PASSWORD_MIN_LENGTH` | `8` | Tamanho mínimo das senhas (máximo fixo de 128). |
| `ARGON2_MEMORY_KIB` | `19456` | Memória do Argon2id (padrão OWASP: 19 MiB). |
| `ARGON2_ITERATIONS` | `2` | Iterações do Argon2id. |
| `ARGON2_PARALLELISM` | `1` | Paralelismo do Argon2id. |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000` | Origens permitidas, separadas por vírgula. |
| `RATE_LIMIT_AUTH_PER_MINUTE` | `10` | Login, cadastro, esqueci a senha e refresh, por IP + identificador. |
| `RATE_LIMIT_GLOBAL_PER_MINUTE` | `600` | Limite geral por IP em todas as rotas (`0` desliga). |
| `RATE_LIMIT_UPLOAD_PER_MINUTE` | `120` | Pedidos e confirmações de envio de arquivo de projeto **por usuário** (`0` desliga). Enviar/reenviar/comprovante/duplicar têm limite fixo de 20/min por usuário. Excesso → 429. |
| `CLAMAV_ADDR` | *(vazio)* | `host:porta` do `clamd`. Com ele, todo arquivo de projeto é varrido na confirmação (falha fechada: scanner fora do ar → 503 `scan_unavailable`). Configure o `clamd` com `StreamMaxLength` ≥ 200M. |
| `MAX_BODY_BYTES` | `1048576` | Tamanho máximo do corpo (413 acima disso). |
| `TRUSTED_PROXIES` | *(vazio)* | CIDRs de proxies reversos confiáveis; só neles o `X-Forwarded-For` é considerado. |
| `INITIAL_REGISTRATION_ENABLED` | *(vazio)* | Registro inicial por HTTP. Vazio: ligado fora de produção, desligado em produção. |
| `SMTP_HOST` / `SMTP_PORT` | *(vazio)* / `587` | Servidor SMTP (STARTTLS; TLS implícito na 465). Sem host: log em dev, descarte em produção. Os e-mails de projeto saem pela caixa de saída (`outbound_emails`): a requisição só grava, um processo em segundo plano envia (tentativas em 1 min, 5 min, 30 min, 2 h, 12 h; até 8) e `cleanup` apaga os já resolvidos. |
| `SMTP_USER` / `SMTP_PASSWORD` | *(vazio)* | Credenciais SMTP (opcionais). |
| `SMTP_FROM` | `no-reply@localhost` | Remetente padrão e dos templates criados no seed. |
| `STORAGE_ENDPOINT` | *(vazio)* | Endpoint interno do MinIO/S3 (`host:porta`). Vazio desliga os arquivos dos documentos (rotas respondem 503). |
| `STORAGE_ACCESS_KEY` / `STORAGE_SECRET_KEY` | *(vazio)* | Credenciais do armazenamento (obrigatórias com `STORAGE_ENDPOINT`). |
| `STORAGE_BUCKET` / `STORAGE_REGION` | `documentos` / `us-east-1` | Bucket (privado) e região usada na assinatura. |
| `STORAGE_USE_SSL` | `false` | TLS na conexão **interna** com o armazenamento. |
| `STORAGE_PUBLIC_URL` | `http://localhost/storage` | URL do armazenamento vista pelo navegador (mesmo domínio + prefixo do proxy). As URLs pré-assinadas são calculadas sobre o host dela; em produção deve ser `https`. |
| `STORAGE_UPLOAD_TTL` / `STORAGE_DOWNLOAD_TTL` | `15m` / `2m` | Validade das URLs pré-assinadas de envio e de download. |
| `DB_PORT` | `5432` | Só do `docker-compose.yml`: porta do PostgreSQL publicada no host. |

---

## 7. Dependências

Nenhuma biblioteca fora desta lista foi adicionada.

| Biblioteca | Motivo |
|---|---|
| `go-chi/chi/v5` | Roteador HTTP sobre `net/http` (grupos, middlewares e `chi.Walk`, usado no teste de coerência do OpenAPI). |
| `go-chi/cors` | CORS configurável por ambiente. |
| `go-chi/httprate` | Rate limiting (por IP e por IP + identificador). |
| `jackc/pgx/v5` | Driver e pool PostgreSQL; LISTEN/NOTIFY. |
| `pressly/goose/v3` | Migrações SQL versionadas, embutidas no binário. |
| `golang-jwt/jwt/v5` | JWTs: HS256 dos usuários finais e validação RS256 dos tokens do MT Login. |
| `MicahParks/keyfunc/v3` | Leitura e cache do JWKS do MT Login (releitura ao aparecer um `kid` desconhecido). |
| `golang.org/x/crypto` | Argon2id. |
| `go-playground/validator/v10` | Validação de entrada (mensagens em português). |
| `caarlos0/env/v11`, `joho/godotenv` | Configuração por ambiente; `.env` só fora de produção. |
| `google/uuid` | Identificadores UUID. |
| `stretchr/testify` | Asserções nos testes. |
| `testcontainers-go` (+ módulo `postgres`) | Testes de integração com PostgreSQL real. |
| `gopkg.in/yaml.v3` | **Somente em teste**: lê `api/openapi.yaml` para garantir que a especificação descreve exatamente as rotas implementadas. |

Ferramentas: `sqlc` (código tipado a partir de SQL), `goose` (CLI de migrações), `golangci-lint`, e `air` (opcional).

---

## 8. Estrutura de pastas

```
cmd/api            servidor HTTP
cmd/cli            migrate:*, seed, admin:create --cpf, cleanup
api/openapi.yaml   especificação OpenAPI 3.1 (servida em /api/docs)
internal/config    leitura e validação das variáveis de ambiente
internal/database  pool pgx, transações, migrações (migrations/), queries sqlc (queries/ → db/)
internal/httpapi   roteador (o pacote se chama httpapi para não colidir com net/http), middlewares, respostas, validação
internal/auth      senha (Argon2id), JWT, tokens opacos, validação OIDC e resolução do administrador por CPF
internal/cpf       normalização, validação e máscara de CPF
internal/authz     motor de permissões: ações, condições, Engine.Can, filtros, Schema, cache, LISTEN/NOTIFY
internal/admin     administradores, roles de admin, permissões, catálogo
internal/enduser   usuários finais, roles, configurações avançadas, templates de e-mail, identificação na API pública
internal/apitoken  tokens de API
internal/policy    policies reutilizáveis (is-owner, All, Any)
internal/audit     registro e consulta da auditoria
internal/mailer    interface Mailer + SMTP, log (dev) e descarte
internal/outbox    caixa de saída de e-mails: gravação na transação + envio assíncrono com tentativas
internal/catalog   cadastros de referência: tipos de entidade, municípios (IBGE, embutidos) e parlamentares
internal/rules     parâmetros das regras de negócio (limites, prazos, datas), versionados por vigência
internal/project   cadastro de projetos pelo proponente (rascunho em seções: Proponente, Instituição, Proposta) e perfil do proponente; visão geral do proponente (`overview.go`, `activity.go`) e do Analista (`queue_overview.go`, `queue_activity.go`, `queue_me.go`) e do Revisor (`reviewer_overview.go`, `reviewer_activity.go`, `reviewer_me.go`)
internal/cnpj      normalização, validação e formatação de CNPJ
internal/document  recurso de exemplo "documentos"
internal/seed      dados iniciais (idempotente)
internal/maintenance  limpeza de sessões de usuário final e tokens vencidos
internal/app       injeção manual de dependências
internal/slug, internal/logging, internal/testutil
```

---

## 9. Testes e qualidade

```bash
make test          # tudo, com -race (integração com PostgreSQL real via testcontainers; requer Docker)
make test-unit     # só os testes sem banco (-short)
make cover-authz   # falha se a cobertura do pacote authz ficar abaixo de 80%
make lint          # golangci-lint
```

O pacote `authz` fica acima de 90% de cobertura (unitários + integração). A suíte de integração cobre, entre outros: validação dos tokens do MT Login (provedor OIDC falso em `testutil`),
primeiro acesso e criação por CPF, pré-cadastro; refresh de usuário final; Author editando documento próprio × alheio; revisor de departamento com campo
permitido e proibido; API pública anônima × autenticada; exclusão de role de usuário final movendo usuários para Public;
impedimento de excluir role em uso e de remover o último Super Admin; token de API somente leitura tentando escrever; seed
idempotente; e a coerência entre `openapi.yaml` e as rotas.

---

## 10. Operação

- **Migrações** rodam por `cli migrate:up` (o Compose faz isso antes de subir a API). **Rollback** é manual (`migrate:down`).
- **`cleanup`** (`make cleanup`) remove sessões de usuário final e tokens de uso único vencidos há mais de N dias (`--days`, padrão 7). Agende,
  por exemplo, uma vez por dia. A carência preserva a detecção de reutilização de refresh token. Com `--draft-days N` (padrão 0 = desligado) também
  apaga rascunhos de projeto, e os arquivos deles, sem alteração há mais de N dias (política de retenção; cada exclusão é auditada).
- **Arquivos dos documentos** (MinIO/S3): o navegador envia e baixa direto do armazenamento por URLs pré-assinadas; a API só as emite e confere
  o resultado. Fluxo de envio: `POST .../documents/{id}/files` (autoriza e devolve `upload.url` + `upload.fields`) → POST multipart ao armazenamento →
  `POST .../files/{fileId}/confirm`. Tipos: PDF e DOCX até 200 MB; PNG, JPEG e WebP até 25 MB; até 50 arquivos por documento. O proxy precisa
  expor o MinIO em `STORAGE_PUBLIC_URL` e **remover o prefixo** antes de repassar, mantendo o `Host` (a assinatura SigV4 cobre `Host` e caminho);
  veja `haproxy/haproxy.cfg`. O `cleanup` também remove envios autorizados e nunca confirmados há mais de 24 h.
  Nos arquivos de **projetos** a confirmação calcula e guarda o SHA-256, confere o tamanho e, se houver `CLAMAV_ADDR`, varre o conteúdo; há ainda
  limites por projeto (10 arquivos por documento, 20 envios em andamento, 300 arquivos, 2 GiB).
- **Proxy reverso**: configure `TRUSTED_PROXIES` com o CIDR do proxy; sem isso o IP considerado é o da conexão.
- **Encerramento**: SIGINT/SIGTERM param de aceitar conexões, concluem as requisições (15 s) e gravações pendentes de `last_used_at`.
- **Imagem**: Dockerfile multi-stage, binário estático em `distroless/static:nonroot`.

---

## 11. Decisões de projeto

- **`internal/http` → `httpapi`**: evita conflito de nomes com `net/http` em todo import.
- **`internal/app`**: concentra a fiação manual de dependências para o servidor, a CLI e os testes não a duplicarem.
- **Sem cache de sessão/token**: cada requisição autenticada consulta o banco (chave primária / hash). Revogar, bloquear, desativar,
  regenerar ou editar valem na hora, sem protocolo de invalidação. Se o volume pedir, um cache curto entra depois.
- **Administradores sem senha nem sessão local**: a identidade é o CPF do MT Login. `admins.password_hash` e `admin_sessions`
  ficaram no esquema, sem uso, até uma migração remover depois de validado o novo login. `one_time_tokens` segue para os
  usuários finais (redefinição e confirmação).
- **E-mail de administrador não é único**: é atributo sincronizado do MT Login; a unicidade é do CPF.
- **E-mail de usuário final não é único no banco**: "uma conta por e-mail" é configuração, aplicada pela aplicação com lock por e-mail.
- **Duas colunas de autoria em `documents`**: `created_by_id` (administrador, usado pelas condições) e `owner_user_id`
  (usuário final, usado pela policy `is-owner`).
- **Mudar `status` exige a ação `publish`** mesmo que o campo esteja liberado no `update`; senão um papel com o campo "publicaria" sem poder.
- **Só um Super Admin concede/retira a role Super Admin** (evita escalada de privilégio por quem tem `settings.users.update`).
- **SQL dinâmico só em três listagens** (administradores, usuários finais, documentos): valores sempre em `$n` e colunas de
  uma lista permitida. Todo o resto usa sqlc.
- **Seed não destrutivo**: permissões padrão só na criação da role; o que o administrador mudou depois é preservado.
- **Auditoria** de toda alteração de roles, permissões, administradores, usuários finais, tokens, configurações e templates, com
  `before`/`after` na mesma transação; nunca grava senhas, tokens nem hashes. Conteúdo (documentos) não é auditado.
- **Visão geral do proponente** (`GET /api/admin/projects/overview` e `/activity`): sempre limitada ao que é da pessoa (titular ou membro ativo), mesmo para quem tem leitura ampla (equipe, Super Admin), somada ao filtro da permissão `admin::projects.read`; `GET /api/admin/projects?access=owner|member|all` faz o mesmo na listagem. O feed lê `audit_logs` só pelas ações de `activityKinds` (lista permitida) e monta `kind` e `data` no servidor, nunca repassa o `payload`, porque o log mistura o que é do proponente com o que é interno da equipe (departamento, recusa de enquadramento, conferência). Evento novo só aparece no feed se alguém o incluir lá de propósito. Ver [`docs/planejamento-visao-geral-proponente.md`](../docs/planejamento-visao-geral-proponente.md).
- **Visão geral do Analista** (`GET /api/admin/projects/queue/overview`, `/queue/activity` e `/queue/me`, só com a ação `start-analysis`): o alcance é a condição dessa permissão somada a projeto enviado ou arquivado (rascunho nunca), e `scope=mine` restringe aos projetos que a pessoa assumiu (`analyst_id`). "Parado" usa o parâmetro de regra `stalled_after_days` (dias por etapa de trabalho: `awaiting_checklist`, `in_analysis`, `framing`, `awaiting_opinion`; "aguardando proponente" não tem limite) e mede o **trecho aberto** do histórico de etapas, isto é, o tempo com o responsável atual, diferente do `stageSeconds` da fila (tempo na etapa). O bloco "pronto para indicar" calcula a conferência em Go só para os 30 candidatos mais antigos (`SetReadyCandidates`). O feed da equipe tem lista permitida **própria** (`queueActivityKinds`), separada da do proponente: mostra o interno (departamento, recusa), nomeia só a equipe, esconde a identidade do proponente e nunca leva a justificativa da recusa nem o `payload`. `/queue/me` devolve sempre e somente os números de quem chama, sem parâmetro de pessoa. Ver [`docs/planejamento-visao-geral-analista.md`](../docs/planejamento-visao-geral-analista.md).
- **Visão geral do Revisor** (`GET /api/admin/projects/department/overview`, `/department/activity` e `/department/me`, só com a ação `review-framing`): o alcance é a condição dessa permissão somada a projeto enviado ou arquivado **e** ao departamento do cadastro da pessoa (o Super Admin sem departamento recebe tudo vazio, nunca os projetos de outros; o Revisor sem departamento já leva 403 no guard). Não há `scope`: o trabalho é do departamento, e vale a primeira decisão de qualquer Revisor dele. Os pendentes usam o mesmo tempo com o responsável atual e o limite `framing` de `stalled_after_days`. Como a **recusa limpa o `department_id`** e tira o projeto do alcance do Revisor, as decisões do departamento (contadores e feed) vêm de `project_framings`, não de `projects`; o marco de um projeto recusado vem com `linkable: false`. O feed tem lista permitida **própria** (`framing_indicated`, `framing_confirmed`, `framing_refused`, `archived`), nomeia a equipe do departamento, esconde a identidade do proponente e nunca leva a justificativa da recusa nem o `payload`. `/department/me` devolve sempre e somente os números de quem chama. Ver [`docs/planejamento-visao-geral-revisor.md`](../docs/planejamento-visao-geral-revisor.md).
- **Estrutura pronta para OAuth** (`end_users.provider` e a interface `enduser.Provider`), sem implementação.

---

## 12. Próximos passos

- **OAuth/OIDC**: implementar `enduser.Provider` (callback → `Identity`), criar/associar o usuário com `provider` e adicionar as rotas
  `/api/connect/{provider}`. O modelo de dados e o registro de provedores já existem.
- **i18n/locales**: `SupportsLocales` já existe nas ações e `properties` (JSONB) nas permissões; falta o conteúdo localizado e a
  validação de `properties.locales`.
- **Múltiplas instâncias**: o cache de permissões já sincroniza por LISTEN/NOTIFY. Falta apenas o que é de infraestrutura: o rate
  limit hoje é por instância (mover para Redis/proxy se necessário) e um balanceador com `TRUSTED_PROXIES`.
- **Frontend administrativo**: consome `/api/admin/*`. Pontos prontos: `GET /api/admin/auth/me` (permissões efetivas), catálogo de ações e
  condições, `availableFields` por ação, rota vinculada das ações públicas e `/docs`.
- Ver o checklist completo em [`SEGURANCA.md`](SEGURANCA.md).
