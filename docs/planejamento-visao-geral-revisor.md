# Planejamento — Visão geral do Revisor do Departamento

Complementa o [planejamento da visão geral do Analista](planejamento-visao-geral-analista.md), o [fluxo de análise e enquadramento](planejamento-fluxo-analise-enquadramento.md) e o [histórico de etapas](planejamento-historico-etapas.md). Define o que a tela `/dashboard/overview` mostra para o **Revisor do Departamento**, de onde vem cada dado e em que ordem construir. Não contém código; contém decisões, contratos, ordem de entrega e testes.

**Escopo:** só o Revisor (quem tem a ação `admin::projects.review-framing`). Secretário de Estado e Supervisor Administrador seguem com o texto de boas-vindas (T33). Painel de prazos entre projetos (T29) e exportação (T30) continuam fora.

**Ideia da tela:** o Revisor abre o painel querendo saber **"o que o Analista mandou para o meu departamento decidir?"** e, depois, **"como anda o departamento?"**. Diferente do Analista, o Revisor tem **uma** tarefa (confirmar ou recusar o enquadramento), então a tela é menor: um bloco de trabalho forte, contadores e o acompanhamento.

---

## 1. Ponto de partida

| Pergunta | Hoje | Limite |
|---|---|---|
| O que a tela mostra para o Revisor? | Um parágrafo de boas-vindas | Nada de dados |
| O que o Revisor enxerga? | Projetos **enviados ou arquivados** com `department_id` igual ao do cadastro dele (condição `admin::project-submitted-same-department`). Sem departamento no cadastro, não vê nada | O alcance é por **departamento**, não por pessoa |
| O que ele faz? | Confirma ou recusa o enquadramento (`POST /framing/confirm` e `/refuse`). **Vale a primeira decisão de qualquer Revisor do departamento** | Nenhum projeto é "dele": não existe "só os meus" para o trabalho pendente |
| Onde ele trabalha hoje? | `/dashboard/analise` com duas abas: "Pendentes de decisão" e "Confirmados" (`reviewerTabs` em `queues.ts`) | Sem aba de arquivados; nada prioriza o mais antigo; sem contadores |
| Como sabe que chegou projeto? | **Não sabe.** O e-mail de "encaminhado ao departamento" (`notifyFramingIndicated`) vai só aos **Supervisores** (`toSupervisors`); o Revisor só recebe e-mail de **arquivamento** | A visão geral vira o único aviso de trabalho novo (ver 3.3 e 9.1) |
| O que acontece depois da recusa? | `department_id` é limpo e o projeto **sai do alcance** do Revisor; a rodada fica em `project_framings` | Qualquer consulta baseada só em `projects` perde as recusas (ver 5) |
| Depois de confirmar? | O projeto continua do departamento (`framing_status = confirmed`, etapa `awaiting_opinion`) | Etapas depois do parecer não existem (T23) |
| Arquivado? | `department_id` é mantido (só a recusa o limpa) | Arquivado depois de uma recusa não tem mais departamento |
| Limite de "parado" do enquadramento? | Já existe: `stalled_after_days.framing` (padrão 5 dias) e `awaiting_opinion` (15) | Reaproveitar, sem parâmetro novo |
| Tempo com o departamento? | Trecho aberto do histórico (`project_stage_history`), medido por `sinceSQL` | Reaproveitar |
| Quem decidiu e quando? | `project_framings.decided_by_id` / `decided_at` | Dá o "resumo das minhas decisões" sem usar a auditoria |

---

## 2. O que a tela mostra

Uma coluna de leitura, do mais urgente para o menos. **Sem seletor "equipe / meus"** (decisão 3.1): o trabalho é do departamento.

```
Olá, <nome>                                     Departamento: <Financeiro>

┌ Aguardando a sua decisão (3) ──────────────────────────────────────────┐
│ <Título> · <código> · <instituição>                    [Analisar]       │
│ Indicado por <Analista> em 03/11 · com o departamento há 6 dias         │
│ Acima do limite de 5 dias · Fora do prazo de envio                      │
│ … (5 mais antigos; "Mostrar mais" até 15; "Ver todos" → fila)           │
└─────────────────────────────────────────────────────────────────────────┘

[Pendentes de decisão 3] [Enquadrados, aguardando parecer 8] [Arquivados (30 dias) 2]
Decididos pelo departamento nos últimos 30 dias: 11 confirmados · 2 recusados

┌ Atividade do departamento ───────┐  ┌ Minhas decisões ───────────────┐
│ 15 últimos marcos                │  │ 7 dias · 30 dias               │
└──────────────────────────────────┘  └────────────────────────────────┘
```

### 2.1 Aguardando a sua decisão
Projetos do departamento com `framing_status = 'pending'` (e `status = 'submitted'`). Ordem: o que está **há mais tempo com o departamento** primeiro. Cada linha traz título, código, instituição, **quem indicou e quando**, há quanto tempo está com o departamento e o botão **Analisar** (abre o detalhe, onde ficam o painel de enquadramento e a justificativa). Selos sempre em texto: "Acima do limite de N dias" (quando o tempo passou de `stalled_after_days.framing`) e "Fora do prazo" (`lateSubmission`).

Bloco vazio mostra "Nenhum enquadramento aguarda a sua decisão." (nunca um espaço em branco). Mostra os 5 mais antigos, com o total, "Mostrar mais" (até 15) e "Ver todos" para `/dashboard/analise?aba=pendentes`.

Aviso fixo discreto: "Vale a primeira decisão de qualquer Revisor do departamento; um projeto pode sumir desta lista se um colega decidir antes."

### 2.2 Contadores do departamento
Cartões que levam à fila, com as regras das abas do Revisor:

| Cartão | Regra | Destino |
|---|---|---|
| Pendentes de decisão | `submitted`, `framing_status = pending` | `?aba=pendentes` |
| Enquadrados, aguardando parecer | `submitted`, `framing_status = confirmed` | `?aba=confirmados` |
| Arquivados (30 dias) | `archived` há até 30 dias, do departamento | `?aba=arquivados` (aba **nova** para o Revisor, ver 6) |

Mais uma linha de texto (não cartão): "Decididos pelo departamento nos últimos 30 dias: N confirmados, N recusados", vinda de `project_framings`.

### 2.3 Atividade do departamento
Os 15 marcos mais novos que dizem respeito ao departamento. Lista permitida, **própria** (não reaproveita a do Analista nem a do proponente, para uma não crescer por causa da outra):

| Evento | Texto | Origem |
|---|---|---|
| Rodada aberta | "<Analista> indicou o projeto <código> para o seu departamento" | `project_framings.indicated_at` |
| Confirmação | "<Revisor> confirmou o enquadramento de <código>" ("Você" se for a própria pessoa) | `project_framings.decided_at`, decisão `confirmed` |
| Recusa | "<Revisor> recusou o enquadramento de <código>" (**sem** a justificativa) | decisão `refused` |
| Arquivamento | "Projeto <código> arquivado: <motivo>" (inclusive "pelo sistema") | `audit_logs` `project.archive`, projetos do alcance |

Nomes de quem agiu aparecem (decisão do usuário): são equipe do mesmo departamento, e o painel de enquadramento já mostra quem indicou e quem decidiu. Proponente **nunca** aparece com nome. Fora do feed: conferência de itens, arquivos, reenvio, complementação, comentários, tudo que não é do enquadramento ou do arquivamento.

**Projeto recusado não abre.** Depois da recusa o projeto sai do alcance do Revisor; o item do feed é só texto (código e título, sem link) e leva o marcador `linkable: false` para a tela não montar um link que daria 403 (recomendação adotada, ver 3.2 / R1).

### 2.4 Minhas decisões
Só o **próprio** Revisor: nos últimos 7 e 30 dias, quantos enquadramentos **confirmei** e quantos **recusei**. Vem de `project_framings.decided_by_id = pessoa` (não depende da auditoria). A rota devolve **sempre e somente** os números de quem chamou, sem parâmetro de pessoa. Nada de tempo médio por pessoa nem comparação entre Revisores (isso é T29, outra decisão).

### 2.5 Estados especiais
- **Revisor sem departamento no cadastro:** a rota responde **403** (a condição do perfil nega tudo sem departamento, antes de chegar ao serviço); a tela trata esse 403 como o estado "sem departamento" (e, como a sessão já traz `departmentId`, nem chama a API quando ele é nulo) e mostra "Seu usuário ainda não tem um departamento. Peça à administração para vinculá-lo; sem isso você não enxerga nenhum projeto." Nenhuma outra causa dá 403 a quem já abriu a página como Revisor.
- **Carregando / erro:** esqueleto por bloco e `QueryError` (`what='o resumo do departamento'`), como no Analista.
- **Super Admin:** cai na visão do Analista (precedência da 3.1); se chamar a rota do Revisor direto, recebe `department: null` e zeros (a rota nunca devolve projetos de outros departamentos).

---

## 3. Decisões

### 3.1 Já tomadas (nesta conversa)
1. **Conteúdo:** núcleo (pendentes) + acompanhamento (contadores do departamento, atividade e resumo pessoal).
2. **Privacidade:** o feed mostra nomes de quem agiu no departamento; o resumo de decisões é só da própria pessoa.
3. **Precedência na página:** Analista → **Revisor** → Proponente → texto. Quem acumula Revisor e Proponente cai na do Revisor (segue o precedente: equipe vence proponente). O Super Admin continua na do Analista.
4. **Sem seletor "equipe / meus"**, porque o trabalho pendente é do departamento (qualquer Revisor decide).
5. Planejar antes de implementar.

### 3.2 Com recomendação adotada (confirmar na revisão)

| # | Decisão | Recomendação adotada | Alternativa |
|---|---|---|---|
| R1 | Recusas no feed | Mostrar código e título **sem link** (`linkable: false`), sem a justificativa. O Revisor já viu o projeto ao decidir | Esconder título ("um projeto") ou não mostrar recusas |
| R2 | Rotas | Três, novas e só do Revisor: `GET /admin/projects/department/overview`, `/department/activity`, `/department/me`, todas com a ação `admin::projects.review-framing`. As rotas do Analista e do proponente **não mudam** (Analista e Revisor recebem 403 nas rotas um do outro) | Reaproveitar `/queue/overview` por perfil (mistura públicos e quebra a regra de acesso do Analista) |
| R3 | Origem do histórico | Recusas, indicações e decisões vêm de **`project_framings`** filtrado por `department_id` da rodada, não do alcance de `projects` (que perde o projeto depois da recusa) | Gravar auditoria nova (escrita em ponto crítico, sem necessidade) |
| R4 | Departamento da pessoa | Lido do cadastro do administrador (`admins.department_id`). Revisor sem departamento já leva 403 no guard (verificado na fase 1); o serviço ainda responde vazio com `department: null` para quem passa por qualquer permissão (Super Admin), e soma o departamento ao alcance por garantia | Afrouxar o guard para devolver vazio (mexe na autorização; descartado) |
| R5 | Medida de tempo | Trecho aberto do histórico (mesmo `sinceSQL` do Analista), contra `stalled_after_days.framing`; "aguardando parecer" usa `awaiting_opinion` só no selo do contador, sem lista | Tempo desde `indicated_at` (quase igual, mas fora da regra E3) |
| R6 | "Aguardando parecer" | Só **contador** que leva à aba "Confirmados"; sem lista (o Revisor já cumpriu a parte dele) | Lista dos 5 mais antigos |
| R7 | Reaproveitamento no backend | Reutilizar `queueQuery`, `workSQL`, `sinceSQL` e `scopeConds` de `queue_overview.go`, com **structs de resposta próprias** (`ReviewerOverview`, `ReviewerItem`) para não inflar `QueueItem` do Analista | Estender `QueueItem` com campos do Revisor |
| R8 | Frontend | Feature nova `features/reviewer-overview/`, mesmas camadas e convenções do Analista; extrair para um componente comum só se a linha de bloco for quase idêntica ao `work-blocks.tsx` (verificar na implementação) | Duplicar |
| R9 | Aba nova na fila | `reviewerTabs` ganha "Arquivados" (`status: 'archived'`), para o cartão ter destino. O alcance do Revisor já inclui arquivados do departamento | Cartão sem link |
| R10 | Frontend | Consultas no cliente, chaves sob `projectKeys.all` (confirmar, recusar ou qualquer ação já invalida a visão geral), `staleTime` 30 s e refetch ao voltar à aba | Prefetch no servidor |

### 3.3 O e-mail ao Revisor na indicação (fora do escopo, a registrar)
Como o Revisor **não** recebe e-mail quando o Analista indica o departamento, a visão geral é consulta, não aviso. Recomendo registrar um débito novo ("Revisor não é avisado quando recebe um projeto", irmão do T14 e do T11) e **não** mudar `notifyFramingIndicated` neste plano. Se a equipe quiser, é um pedido pequeno depois: acrescentar os Revisores do departamento como destinatários, com a mesma chave de deduplicação por rodada.

---

## 4. Contratos de API

Todas sob `/api/admin`, `{data, meta}`, guardadas por `admin::projects.review-framing`. **Sem parâmetro `scope`.**

### 4.1 `GET /projects/department/overview`
```
data: {
  department: { id, name } | null,          // null: Super Admin sem departamento (tudo abaixo vem zerado); Revisor sem departamento leva 403
  limits: { framing, awaiting_opinion },    // dias vigentes (stalled_after_days)
  counts: {
    framing_pending, awaiting_opinion, archived_30d,
    decided_30d: { confirmed, refused }     // do departamento, por project_framings
  },
  pending: { total, items: [ { projectId, title, code, institution, round,
             indicatedAt, indicatedBy, sinceSeconds, late, overLimit } ] }
}
```
`pending` traz até 15 itens, os mais antigos primeiro. `indicatedBy` é o nome do Analista que abriu a rodada. Nenhum CPF, e-mail ou telefone do proponente, e nenhuma justificativa de recusa.

### 4.2 `GET /projects/department/activity?limit=`
`limit` padrão 15, máximo 30 (valor inválido é 422 com o campo). `[ { id, at, kind, by: {type: admin|proponent|system, name?, self?}, projectId, title, code, linkable, data } ]` (`id` é texto: uma rodada gera dois marcos, a indicação e a decisão; `data` leva `round` nos marcos do enquadramento e `reason` no arquivamento; o arquivamento por desistência é do proponente, sem nome), com `kind` (`framing_indicated`, `framing_confirmed`, `framing_refused`, `archived`) e `data` (`department`, `reason`) montados no servidor, nunca o `payload` da auditoria.

### 4.3 `GET /projects/department/me`
`{ last7: {confirmed, refused}, last30: {confirmed, refused} }`, só de quem chama.

---

## 5. Backend

- **Pacote `project`**, arquivos novos `reviewer_overview.go`, `reviewer_activity.go`, `reviewer_me.go`, no mesmo molde de `queue_*.go`.
- **Alcance dos projetos (contagens e `pending`):** `scopeConds(authz.And(perm, submittedFilter()), actor, "")`, que já embute `department_id = departamento do Revisor` pela condição do perfil. Sem departamento, a condição nega e a rota devolve a resposta vazia (R4); o departamento é lido antes, por consulta ao cadastro do administrador.
- **Contagens:** uma consulta agregada por `(status, framing_status)` sobre o alcance; `archived_30d` por `archived_at`. `decided_30d`: `COUNT … FILTER (WHERE decision = …)` em `project_framings` com `department_id = departamento` e `decided_at` nos últimos 30 dias.
- **`pending`:** a CTE `w` de `queue_overview.go` com `framing_status = 'pending'`, `ORDER BY since DESC`, mais a rodada pendente (`round`, `indicated_at`, nome de quem indicou, vindos de `project_framings` com `decision = 'pending'`) e `overLimit = since >= limits.framing * 86400`.
- **Feed:** `UNION` de (a) rodadas de `project_framings` do departamento (um evento de indicação por `indicated_at` e um de decisão por `decided_at`; `linkable` = o projeto ainda está no alcance, isto é, `projects.department_id` igual ao do Revisor) e (b) `project.archive` em `audit_logs` dos projetos do alcance. Nome do administrador resolvido pelo cadastro (só nome e sobrenome; sem o e-mail como alternativa, ao contrário do feed do Analista); `self` quando `actor_id` é o chamador. `kind` e `data` por função pura testável, lista permitida **própria**.
- **Minhas decisões:** `COUNT` em `project_framings` por `decided_by_id = pessoa` e `decision`, janelas de 7 e 30 dias.
- **Índice:** `project_framings (department_id, decided_at DESC)` só se a medição pedir (hoje existem a unicidade `(project_id, round)` e a pendente por projeto); `admins_department_idx` já existe.
- **OpenAPI** para as três rotas (o teste de cobertura falha sem isso). **Sem migração** de tabela e **sem parâmetro de regra novo** (reaproveita `stalled_after_days`).

---

## 6. Frontend

Mesmas convenções do Analista: camadas `types` → `service` → `queries`, ícones só por `@/components/icons`, `PageContainer`, filtros na URL (nuqs), consultas no cliente.

- **Página:** `app/dashboard/overview/page.tsx` escolhe pela sessão, nesta ordem: `start-analysis` → Analista; `review-framing` → **Revisor** (novo); `isProjectProponentSession` → Proponente; senão, o texto.
- **Feature nova** `features/reviewer-overview/` (`api/` e `components/`): `reviewer-overview.tsx` (cabeçalho com o departamento, estado "sem departamento"), `pending-decisions.tsx`, `department-counters.tsx`, `department-activity-feed.tsx`, `my-decisions.tsx`.
- **Ações:** o botão **Analisar** leva ao detalhe do projeto (`/dashboard/projetos/{id}`), onde ficam o painel de enquadramento e a justificativa; a visão geral não confirma nem recusa por conta própria.
- **Contadores:** levam a `/dashboard/analise?aba=pendentes|confirmados|arquivados`. `queues.ts` ganha a aba "Arquivados" do Revisor (R9).
- **Atualização:** `staleTime` curto, refetch ao voltar à aba; confirmar ou recusar já invalida `projectKeys.all`.
- **Acessibilidade e celular:** estado sempre em texto ("Acima do limite de 5 dias", "Fora do prazo"), nunca só cor; uma coluna no celular; rótulos completos nos links e botões.
- **Documentação do frontend:** `frontend/CLAUDE.md` ganha o item da visão geral do Revisor.

---

## 7. Testes

**Integração** (`internal/app`, testcontainers), com os perfis semeados (Regina/Financeiro e Rui/Jurídico), como nos testes de visibilidade da equipe:
- *Alcance:* o Revisor do Financeiro só vê os pendentes do Financeiro; o do Jurídico não os vê; Revisor sem departamento recebe 403; Super Admin recebe `department: null` e zeros.
- *Contagens:* pendentes, aguardando parecer e arquivados (30 dias) batem com as abas da fila; um projeto arquivado há mais de 30 dias não conta.
- *Pendentes:* ordem do mais antigo; `overLimit` ao envelhecer o trecho aberto (como em `project_history_test.go`); mudar `stalled_after_days.framing` muda o resultado; limite de 15 itens com o total correto; confirmar por um colega tira o projeto da lista.
- *Recusa:* depois de recusar, o projeto some de `pending` e dos contadores, **continua** no feed como `framing_refused` com `linkable: false`; nova indicação ao mesmo departamento volta a `pending` com a rodada nova.
- *Feed:* indicação, confirmação, recusa e arquivamento aparecem com o nome certo e `self` para o chamador; **não vaza** CPF, e-mail, telefone nem o texto da justificativa; não traz conferência de item, arquivo nem reenvio; outro departamento não aparece.
- *Minhas decisões:* conta só as decisões da pessoa que chamou (um colega decidindo não entra), nas janelas de 7 e 30 dias.
- *Acesso:* Analista, Secretário, Supervisor e Proponente recebem 403 nas três rotas; o Revisor recebe 403 nas rotas `/queue/*`.
- `limit` inválido: 422. Cobertura do OpenAPI.

**Unidade:** montagem de `kind`/`data` do feed (cada tipo; ação fora da lista ignorada).
O frontend não tem suíte automatizada: percorrer no navegador com Regina e Rui (lista cheia, vazia, sem departamento), com um colega decidindo no meio, em tema claro e escuro e no celular. Isso depende do T24 (logins de desenvolvimento por perfil, hoje os administradores de exemplo não entram pelo MT Login).

---

## 8. Fases de entrega

| Fase | Conteúdo | Pronto quando |
|---|---|---|
| **1 (feita)** | `GET /projects/department/overview` (departamento, limites, contagens, bloco `pending`); OpenAPI; testes de alcance, contagens, ordem e limite | Testes da seção 7 desses itens passam |
| **2 (feita)** | `GET /projects/department/activity` e `/department/me` (origem em `project_framings`); teste de não vazar | Testes do feed e do resumo passam |
| **3 (feita, sem ver no navegador)** | Frontend: camada de API, página escolhe o Revisor, bloco de pendentes, contadores, estados vazio e sem departamento; aba "Arquivados" na fila | Percorrida no navegador |
| **4 (feita, sem ver no navegador)** | Frontend: feed do departamento e "Minhas decisões"; ajustes de tema claro e celular | Percorrida no navegador |
| **5 (feita)** | Documentação (seção 10) | — |

Cada fase termina com `make test` e `make lint` (backend) e `bun run typecheck`, `lint` e `format:check` (frontend), e commit próprio.

---

## 9. Riscos e cuidados

1. **A visão geral é o único aviso.** O Revisor não recebe e-mail ao receber um projeto (3.3). Se ele não abrir o painel, o projeto fica parado; o selo "Acima do limite" ajuda quem abre, não quem não abre. Registrar o débito e decidir sobre o e-mail depois.
2. **Recusa tira o projeto do alcance.** Por isso o feed e o resumo vêm de `project_framings` e o item recusado não tem link (R1). Qualquer outra consulta nova sobre "meus projetos" precisa lembrar disso.
3. **Concorrência.** Dois Revisores veem o mesmo pendente; quem decide primeiro vence e o outro leva 409 `framing_already_decided` no detalhe. A lista some o item no próximo refetch; o aviso da 2.1 explica.
4. **Revisor sem departamento** leva 403 na rota; a tela precisa tratá-lo como "sem departamento" e dizer o motivo (2.5), em vez de mostrar o erro genérico. A administração de usuários já avisa na atribuição do perfil (`reviewer-warning.tsx`).
5. **Departamento excluído.** `project_framings.department_id` vira nulo e o nome fica copiado; o Revisor daquele departamento também perde o vínculo (`admins.department_id` vira nulo), então cai no caso 4.
6. **Vazamento pelo feed.** Lista permitida própria, dados montados no servidor, nunca o `payload`, nunca a justificativa, e o teste de não vazar.
7. **Métrica pessoal.** "Minhas decisões" fica só com a própria pessoa e a rota não aceita parâmetro de outra; comparar Revisores é decisão separada (T29).
8. **Limites mal escolhidos.** 5 dias (enquadramento) e 15 (parecer) são suposição do plano do Analista; quem os ajusta hoje é só o Super Admin (E10).
9. **Duplicação com o Analista.** Os dois painéis dividem molde e medida de tempo; se um mudar, o outro precisa acompanhar (R7 e R8 reduzem o risco).

---

## 10. Atualizações de documentação ao final

- `backend/api/openapi.yaml` (três rotas novas).
- `backend/README.md`: as rotas, o alcance, a origem em `project_framings` e a lista permitida do feed do departamento.
- `README.md`: uma frase sobre a visão geral do Revisor.
- `frontend/CLAUDE.md`: a estrutura de `features/reviewer-overview`.
- `docs/debitos-tecnicos.md`: **T33** (Revisor sai da lista de perfis sem visão geral; Secretário e Supervisor continuam), **T34** novo ("Revisor não é avisado quando recebe um projeto", junto de T11 e T14). O índice de `project_framings` não foi criado: só se a medição pedir.
- `graphify-out/`: `graphify update .` (sem custo de IA).
