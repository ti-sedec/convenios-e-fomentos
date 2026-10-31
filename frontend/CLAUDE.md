# CLAUDE.md

This is a Next.js 16 + shadcn/ui admin dashboard starter kit.

## Key References

- **[AGENTS.md](./AGENTS.md)** — Full project overview, tech stack, structure, conventions, data fetching patterns, deployment
- **[docs/forms.md](./docs/forms.md)** — Form system: TanStack Form + Zod, composable fields, validation, multi-step, sheet/dialog forms
- **[docs/themes.md](./docs/themes.md)** — Theme system: OKLCH colors, adding themes, font config
- **Auth** — login pelo **MT Login** (Keycloak, Authorization Code + PKCE) conduzido pelo servidor do Next: `src/app/auth/login` e `src/app/auth/callback` (route handlers), `src/lib/auth/oidc.ts`; tokens em cookies httpOnly (`src/lib/auth/`), proxy autenticado em `src/app/bff/[...path]`, renovação em `src/proxy.ts`, permissões via `useCan()` / `<Can>` (`src/features/auth/session-provider.tsx`). A identidade do administrador é o CPF; sem roles o usuário vê "Aguardando liberação". Chamadas do navegador usam `apiClient` (`src/lib/api-client.ts`); erros são `ApiError`.
- **Documentos e arquivos** — `src/features/documents` (api → service → queries) e `src/app/dashboard/documentos`. O envio é em 3 passos: `requestUpload` (a API autoriza) → `uploadToStorage` (XHR com progresso, POST multipart direto em `/storage`, mesmo domínio, sem passar pelo BFF) → `confirmUpload`. O BFF só trafega JSON. As regras de tipo/tamanho em `lib/file-rules.ts` espelham o backend, que é a autoridade.
- **Telas da equipe de projetos** — `src/app/dashboard/analise` (fila por perfil, `features/projects/components/staff-queue.tsx` e `queues.ts`) e o detalhe do projeto para a equipe (`features/projects/components/staff/`: conferência, enquadramento, histórico (aba "Histórico": linha do tempo das etapas em `stage-timeline.tsx`, a partir de `GET /admin/projects/{id}/history`, com durações em dias e horas por `formatDuration`) e as ações do Analista, do Revisor e do Secretário). A equipe cai nessa visão quando `project.access` é vazio; quem é equipe é decidido por `isProjectStaff` (`features/projects/access.ts`) e `NavItem.access.when` (ver `docs/nav-rbac.md`). O servidor é a autoridade: a tela só esconde o que não cabe e mostra os erros do fluxo em português (`staff/errors.ts`). Os documentos se analisam no modal `staff/document-review-dialog.tsx`: o arquivo é exibido por `staff/viewer/` (react-pdf, com o worker do pdf.js no pacote; Word já chega convertido em PDF pela API) e o "visto" é registrado quando o conteúdo aparece, não ao baixar. A aba Documentos (de todos os perfis) também abre o arquivo sem baixar, pelo botão "Visualizar" (`components/project-file-viewer.tsx`): a equipe cai no mesmo modal da conferência, com a decisão só para quem pode analisar; o proponente e os membros caem num modal só de leitura.
- **[docs/deployment.md](./docs/deployment.md)** — Deployment: Vercel, production environment variables, Docker

## Critical Conventions

- **React Query** for all data fetching — `void prefetchQuery()` on server + `useSuspenseQuery` on client (standard TanStack pattern), `useMutation` for forms, `HydrationBoundary` + `dehydrate` for hydration, `<Suspense fallback>` for streaming
- **API layer** per feature — `api/types.ts` → `api/service.ts` → `api/queries.ts`; queries use key factories (`entityKeys.all/list/detail`); components import from service and queries, never from mock APIs directly
- **nuqs** for URL search params — `searchParamsCache` on server, `useQueryStates` on client, use `getSortingStateParser` for sort (same parser as `useDataTable`)
- **Icons** — only import from `@/components/icons`, never from `@tabler/icons-react` directly
- **Forms** — `useAppForm` from `@/lib/form` (TanStack `createFormHook`) + `form.AppField` rendering the field components in `@/components/forms/fields` (`field.TextField`, `field.SelectField`, …); each component is the shadcn TanStack Form doc anatomy; raw `form.Field` for one-off custom fields; form-level Zod `onSubmit` validators
- **Page headers** — use `PageContainer` props (`pageTitle`, `pageDescription`, `pageHeaderAction`), never import `<Heading>` manually
- **Formatting** — single quotes, JSX single quotes, no trailing comma, 2-space indent
