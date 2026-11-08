# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Sistema de controle de convênios, termos de fomento e prestação de contas (documentação e UI em pt-BR). Monorepo com `backend/` (API Go) e `frontend/` (painel Next.js), atrás de um HAProxy. READMEs detalhados: [README.md](README.md), [backend/README.md](backend/README.md), [frontend/CLAUDE.md](frontend/CLAUDE.md) e [frontend/AGENTS.md](frontend/AGENTS.md) (este último vem de um template Clerk; a autenticação real é o MT Login, descrito abaixo). A legislação de referência e o planejamento do cadastro de projeto estão em `docs/`.

## Commands

Ambiente completo (Postgres + migrações + API com air + Next com HMR + MinIO + HAProxy em http://localhost):

```bash
cp .env.example .env
docker compose -f docker-compose.dev.yml up --build
docker compose exec api go run ./cmd/cli seed        # cria roles/permissões; SEED_SUPER_ADMIN_CPF define Super Admins
docker compose exec api go run ./cmd/cli admin:create --cpf 000.000.000-00
docker compose exec api go run ./cmd/cli migrate:up|migrate:down|migrate:status|cleanup
```

Backend (`backend/`, roda no host; testes de integração sobem Postgres via testcontainers, então exigem Docker):

```bash
make test                 # go test -race -count=1 ./...
make test-unit            # -short, sem Docker
go test -race -count=1 -run TestName ./internal/app/   # um teste
make cover-authz          # falha se a cobertura de internal/authz < 80%
make lint                 # golangci-lint
make sqlc                 # regenera o código a partir do SQL; commite o código gerado
make migrate-create name=criar_tabela_x
```

Frontend (`frontend/`, Bun): `bun run dev | typecheck | lint | format:check | format | build`. Não há suíte de testes automatizados no frontend. Lint é oxlint e formatação oxfmt (aspas simples, sem vírgula final, 2 espaços).

## Architecture

**Fluxo de requisição.** O navegador nunca fala direto com a API nem vê tokens. O login é pelo **MT Login** (Keycloak, OIDC Authorization Code + PKCE) conduzido pelo servidor do Next (`src/app/auth/login|callback`, `src/lib/auth/`); access/refresh tokens ficam em cookies httpOnly. Chamadas autenticadas passam pelo proxy `src/app/bff/[...path]`, que injeta o `Authorization: Bearer` e renova tokens (`src/proxy.ts`). A API só **valida** o token (JWKS); o administrador é identificado pelo **CPF** do token (claim `OIDC_CPF_CLAIM`). Sem roles, o usuário vê "Aguardando liberação". Exceção: o upload de arquivos vai direto do navegador para `/storage` (MinIO, URL pré-assinada) em 3 passos: `requestUpload` → `uploadToStorage` → `confirmUpload`; as regras de tipo/tamanho em `frontend/src/lib/file-rules.ts` espelham o backend, que é a autoridade.

**Backend** (`backend/internal/*`, um pacote por domínio). Três tipos de sujeito com permissões separadas: administrador (`/api/admin/*`, RBAC com ação + recurso + campos + condições, união de roles), usuário final (`/api/*`, por ação `api::recurso.acao`, uma role, propriedade via `policy`) e token de API (`gat_...`). Camadas: handler fino → service (regra de negócio) → sqlc (SQL puro, migrações goose em `internal/database/migrations`). A injeção de dependências é manual em `internal/app/app.go`, usada por `cmd/api`, `cmd/cli` e pelos testes de integração (que também ficam em `internal/app`). Domínio de convênios: `project` (cadastro, submissão, ciclo de vida, compartilhamento por CPF, documentos, emendas), `catalog`, `rules` (parâmetros de regra), `document`/`upload`/`storage`. A especificação OpenAPI está em `backend/api/openapi.yaml` e um teste garante que ela cobre as rotas registradas, então atualize-a ao criar rotas. Respostas: `{data, meta}` ou `{error:{status,code,message,details}}` (`code` estável em inglês, `message` em português).

**Frontend** (`frontend/src`). Organizado por feature em `features/<nome>/` com camadas `api/types.ts` → `api/service.ts` → `api/queries.ts` (React Query com key factories; prefetch no servidor + `useSuspenseQuery` + `HydrationBoundary`). Convenções: filtros de URL com nuqs; ícones só via `@/components/icons`; formulários com `useAppForm` (`@/lib/form`) + campos em `@/components/forms/fields` e Zod; cabeçalhos de página via props de `PageContainer`; permissões via `useCan()` / `<Can>`; chamadas do navegador usam `apiClient` (erros são `ApiError`). Guias em `frontend/docs/forms.md` e `themes.md`.

**Produção** é `docker-compose.prod.yml` (Portainer, build no servidor a partir do Git; API em binário distroless e Next em modo standalone), documentada em [docs/deploy-producao.md](docs/deploy-producao.md). Não use bind mount de arquivo do repositório nele (o Portainer clona o Git dentro do contêiner dele); configuração vai dentro de imagens.

Existe um grafo de conhecimento do repositório em `graphify-out/` (veja `GRAPH_REPORT.md`) para consultas de arquitetura.
