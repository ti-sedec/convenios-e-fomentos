# Planejamento — Visão geral do proponente

Complementa o [planejamento do cadastro de projeto](planejamento-cadastro-projeto.md), o [fluxo de análise e enquadramento](planejamento-fluxo-analise-enquadramento.md) e o [histórico de etapas](planejamento-historico-etapas.md). Define o que a tela `/dashboard/overview` mostra para o **Proponente**, de onde vem cada dado e em que ordem construir. Não contém código; contém decisões, contratos, ordem de entrega e testes.

**Escopo:** o Proponente (titular de projeto ou membro convidado por CPF). A visão geral da equipe (Analista, Revisor, Secretário, Supervisor) fica como está hoje (texto de boas-vindas) e é assunto de outro documento. Prestação de contas, vigência e execução não existem no sistema ainda (T23); a tela deixa o lugar pronto, sem mostrar nada vazio.

---

## 1. Ponto de partida

| Pergunta | Hoje | Limite |
|---|---|---|
| O que a tela mostra? | `overview/page.tsx`: um parágrafo de boas-vindas | Nada de dados |
| Como o Proponente lista projetos? | `GET /admin/projects`, já limitado pela permissão (titular + `admin::project-member`) | Não distingue "meus" de "compartilhados comigo"; o frontend (`ProjectsList`) guarda busca e página em `useState`, sem filtros na URL |
| Que dados já saem por projeto? | `Summary`: `status`, `stage`, `complementDueAt`, `totalCents`, `access`, `isOwner`, `archiveReason`, `submittedAt` | Etapa e totais, mas nenhuma contagem agregada |
| Pendências de rascunho | `Project.Issues` (`block`/`warn`) | Só no `GET /projects/{id}`; calculado por projeto |
| Perfil completo? | `proponent-profile.complete` | Já existe a rota; basta o frontend usar |
| Linha do tempo do que aconteceu | `audit_logs` (uma linha por ação, imutável desde a 00032) e `project_stage_history` | Os dois trazem dado **interno** (analista, departamento, conferência). O histórico de etapas e `stageSeconds` são só da equipe (404 para o titular) |
| Rascunho esquecido | `CleanupAbandonedDrafts` apaga por tempo | Quem decide o prazo é o `--draft-days` do CLI; **a API não sabe qual é** |

---

## 2. O que a tela mostra

Uma coluna de leitura, do mais urgente para o menos urgente, com um filtro de acesso no topo que vale para tudo.

```
Olá, <nome>                                  [ Todos | Meus | Compartilhados comigo ]

┌ Precisa da sua atenção ──────────────────────────────┐   (some se não houver nada)
│ ⚠ Projeto X devolvido — responder até 03/11 (2 dias)  │
│ ✎ Projeto Y: rascunho com 3 pendências                │
│ ✉ Projeto Z foi compartilhado com você                │
└───────────────────────────────────────────────────────┘

[Rascunhos 2] [Com a equipe 3] [Suas correções 1] [Aguardando parecer 1] [Arquivados 4]   Em tramitação: R$ 1,2 mi

┌ Projetos recentes ───────────────┐  ┌ Atividade recente ─────────────────┐
│ 5 últimos, com etapa e ação      │  │ 10 últimos marcos dos projetos     │
│ Ver todos os projetos →          │  │                                    │
└──────────────────────────────────┘  └────────────────────────────────────┘
```

### 2.1 Filtro de acesso (decisão do usuário)
Mostra os projetos **do Proponente** e os **em que é membro**, com três opções: **Todos** (padrão), **Meus** (titular) e **Compartilhados comigo** (membro, qualquer nível). O filtro vale para todos os blocos. Cada opção mostra o seu total (`Meus (3)`, `Compartilhados comigo (2)`), calculado sem depender da opção escolhida. Fica na URL (`?acesso=meus`, nuqs), para o link e o botão voltar funcionarem. O mesmo filtro entra na página "Meus projetos".

### 2.2 Precisa da sua atenção
Só mostra o que o Proponente **pode agir**: projetos em que é titular ou membro com edição. Membro de leitura não recebe alerta de ação. Em ordem de urgência:

| Tipo | Quando aparece | Mostra | Ação |
|---|---|---|---|
| `complement_open` | Etapa `awaiting_proponent` com pedido aberto | Projeto, prazo (`dueAt`), quantos itens, "vence hoje", ou "prazo vencido" se `dueAt` passou (o projeto será arquivado e não é mais editável) | Abrir o projeto na seção devolvida |
| `profile_incomplete` | `proponent-profile.complete = false` | Aviso do perfil | Ir ao perfil |
| `signature_pending` | Declaração em `awaiting_signature` no MT-ID | Projeto e declaração | Abrir o projeto (conferir na implementação se a assinatura expira) |
| `draft` | Rascunho do Proponente | Projeto, última alteração e **quantas pendências bloqueiam o envio** (só dos 5 rascunhos mais recentes, ver D5) | Continuar o rascunho |
| `shared_new` | Convite ativo ainda não aberto (`accepted_at` nulo) | Projeto e quem compartilhou (rótulo que o titular deu) e o nível | Abrir o projeto |

`profile_incomplete` vem do frontend (usa a rota do perfil que já existe); os demais vêm do resumo. O bloco some inteiro quando não há nada, para não virar ruído.

### 2.3 Contadores
Exclusivos entre si (cada projeto cai em um só). O Proponente vê as etapas pelos rótulos que já existem; o enquadramento **continua** como "Com a equipe" (é interno):

| Cartão | Regra |
|---|---|
| Rascunhos | `status = draft` |
| Com a equipe | `submitted` com etapa `awaiting_checklist` ou `in_analysis` (inclui enquadramento pendente) |
| Aguardam suas correções | `submitted` com etapa `awaiting_proponent` |
| Aguardando parecer | `submitted` com etapa `awaiting_opinion` |
| Arquivados | `status = archived` |

Cada cartão leva a `/dashboard/projetos` já filtrado (situação + acesso). Ao lado, **valor em tramitação**: soma de `totalCents` dos projetos enviados (`submitted`), sem rascunhos nem arquivados. Se houver os dois instrumentos, mostra a divisão (convênio / termo de fomento) numa linha de apoio.

### 2.4 Projetos recentes
Os 5 mais recentemente alterados (já é a ordenação padrão da lista), com título, instituição, etapa, valor e uma **ação principal por linha**: "Responder complementação" (devolvido), "Continuar" (rascunho), "Abrir" (os demais). Rodapé: "Ver todos os projetos". Reaproveita `GET /admin/projects`.

### 2.5 Atividade recente
Últimos marcos dos projetos visíveis, do mais novo ao mais antigo. **Só marcos que o Proponente já pode saber**, escritos para ele:

| Evento (`audit_logs.action`) | Texto | Dados enviados |
|---|---|---|
| `project.submit` | "Projeto enviado. Código ABC" | código, `late` |
| `project.start-analysis` | "A equipe iniciou a análise" | — |
| `project.complement-request` | "A equipe pediu correções (N itens), prazo até 03/11" | itens, `dueAt` |
| `project.resubmit` | "Correções enviadas (versão N)" | versão |
| `project.framing-confirm` | "Projeto enquadrado. Aguardando parecer" | — |
| `project.archive` (inclusive o do sistema) | "Projeto arquivado: <motivo>" | motivo (rótulos de `archiveReasonLabels`) |
| `project.member-invited` | "Projeto compartilhado com você" (só para o convidado) | nível |

**Fora do feed, de propósito:** `assign-department`, `framing-indicated`, `framing-refuse`, `framing-refused` e `review-item` (internos: revelariam departamento, recusa ou a conferência), todos os `file-*` e `receipt-download` (ruído), `update`, `create`, `duplicate`, `delete`, `draft-expired`, `member-updated`, `member-revoked` e as declarações (podem entrar depois). O feed mostra **quando** algo aconteceu, nunca **quanto tempo** o projeto ficou parado: tempo na etapa e linha do tempo continuam internos (decisão do usuário).

### 2.6 Lugar para o que vem depois
Prestação de contas, vigência e "próximos prazos" entram como novas seções no mesmo contêiner (a página é uma lista de blocos independentes, cada um com sua consulta). Nada é desenhado agora para eles.

### 2.7 Quem não tem projeto
Se o Proponente não tem nenhum projeto (nem compartilhado), a tela mostra só os dois passos que existem hoje (perfil, depois "Meus projetos"), no lugar dos blocos.

---

## 3. Decisões

### 3.1 Já tomadas (nesta conversa)
1. Mostrar projetos do Proponente **e** os que ele é membro, com filtro "Todos / Meus / Compartilhados comigo".
2. Tempo na etapa e linha do tempo continuam só da equipe.
3. Incluir o feed de atividade na primeira versão.
4. Planejar antes de implementar.

### 3.2 Com recomendação adotada (confirmar na revisão)

| # | Decisão | Recomendação adotada | Alternativa |
|---|---|---|---|
| D1 | Endpoints | Dois: `GET /admin/projects/overview` (contagens e atenção) e `GET /admin/projects/activity` (feed). Cada um com sua consulta e seu `staleTime` | Um só; o feed incha a resposta e invalida junto |
| D2 | Permissão | Nenhuma nova: as duas rotas usam `admin::projects.read` (o Proponente já tem). Não há migração nem seed | Ação própria, para poder ligar e desligar por perfil |
| D3 | Alcance | As duas rotas **sempre** se limitam ao que a pessoa tem como titular ou membro ativo, **mesmo para quem tem leitura ampla** (Super Admin, equipe), somado ao filtro da permissão. Assim a "visão geral" nunca vira painel da equipe nem expõe rascunho alheio (T19) | Seguir só a permissão |
| D4 | Fonte do feed | `audit_logs` com **lista permitida** de ações, devolvendo `kind` estável e dados **montados no servidor** (nunca o `payload`). O frontend traduz `kind` em texto | Nova tabela de eventos do proponente |
| D5 | Pendências do rascunho | Contar `block` só dos **5 rascunhos mais recentes** do Proponente, chamando a mesma validação do `GET /projects/{id}`. Os demais rascunhos aparecem sem número | Calcular todos (custo cresce com a lista) |
| D6 | Autor no feed | `by`: `you`, `member` ou `team`; **sem nome**. Nome de analista nunca sai; o de colega só se decidir depois | Mostrar o nome do colega |
| D7 | Janela do membro | O convidado só vê eventos **a partir da criação do convite** (`project_members.created_at`); quem perde o acesso deixa de ver na hora | Ver o histórico inteiro do projeto |
| D8 | Aviso de exclusão de rascunho | **Fora da v1.** A API não sabe o `--draft-days`. Se quiser, entra numa fase 7: `DRAFT_RETENTION_DAYS` na configuração, lido pelo CLI e pelo resumo ("será excluído em N dias") | Entrar na v1 |
| D9 | Contadores | Calculados no servidor com uma consulta agregada por `(status, stage)`; o frontend agrupa nos cinco cartões. A API devolve a contagem **bruta**, para a regra de agrupamento ficar só na tela | Devolver já agrupado |
| D10 | Onde mora o código do frontend | `features/proponent-overview/` com `api/types.ts`, `service.ts`, `queries.ts`; chaves sob `projectKeys.all` para que toda mutação de projeto (envio, correção, exclusão) já invalide a visão geral | Dentro de `features/projects` |

---

## 4. Contratos de API

Todas sob `/api/admin`, `{data, meta}`. Parâmetro comum **`access`**: `all` (padrão), `owner`, `member`. Valor inválido: 422 com o campo, como nos outros filtros.

### 4.1 `GET /projects/overview?access=`
```
data: {
  counts:  { draft, awaiting_checklist, in_analysis, awaiting_proponent, awaiting_opinion, archived },  // inteiros
  byAccess: { owner, member },                       // sempre sem o filtro, para os rótulos do seletor
  activeCents: { total, convenio, fomento },         // enviados (submitted)
  attention: [
    { kind: 'complement_open', projectId, title, code, dueAt, overdue, items },
    { kind: 'signature_pending', projectId, title, declaration },
    { kind: 'draft', projectId, title, updatedAt, blocking? },       // blocking só nos 5 primeiros
    { kind: 'shared_new', projectId, title, level, invitedBy }       // rótulo dado pelo titular
  ]  // ordenado por urgência e limitado a 20
}
```
Tudo calculado com a regra de alcance de D3. O frontend acrescenta o aviso de perfil (2.2).

### 4.2 `GET /projects/activity?access=&limit=`
`limit` padrão 10, máximo 30; sem paginação na v1.
```
data: [ { id, at, kind, by, projectId, title, code, data: { ... } } ]   // kinds da tabela 2.5
```
`data` leva só os campos da tabela 2.5 (por exemplo `{dueAt, items}` ou `{reason}`).

### 4.3 `GET /projects` ganha `access=owner|member`
`owner`: `created_by_id` é a pessoa. `member`: o projeto está entre os da pessoa como membro ativo (o `memberships()` que a listagem já chama) e ela não é a titular. Vale também para a página "Meus projetos". O filtro de permissão continua sendo aplicado sempre; o `access` só restringe mais.

---

## 5. Backend

- **Alcance (D3):** uma função que devolve o filtro "titular ou membro ativo" e é combinada com a decisão de leitura, no mesmo estilo de `List` (condições + `ToSQL`). `overview` e `activity` a usam; `List` usa a parte `owner`/`member`.
- **Contagens:** uma consulta agregada (`GROUP BY status, stage`) e `byAccess` com duas contagens. Valores por instrumento em `activeCents`.
- **Atenção:** consultas pequenas e separadas (pedidos de complementação abertos, declarações aguardando assinatura, rascunhos, convites com `accepted_at` nulo). Só projetos em que a pessoa pode agir. `overdue` = `dueAt` antes de agora; o resumo **não** arquiva (isso é do laço periódico e do `settle` ao abrir o projeto).
- **Feed:** consulta em `audit_logs` com `entity = 'project'`, `entity_id` entre os projetos de alcance e `action` na lista permitida, ordem `created_at DESC, id DESC`. O índice `audit_logs_entity_idx (entity, entity_id)` já cobre; um índice `(entity, entity_id, created_at DESC)` só se a medição pedir. Montagem do `kind`/`data` em uma função pura (testável sem banco); ação fora da lista nunca é lida do `payload`.
- **Sem migração**, a menos que D8 entre. Sem nova permissão. Rotas registradas em `handler.go` depois de `/projects` e antes de `/projects/{id}` não conflitam (o chi prefere a rota estática); conferir no teste.
- **OpenAPI:** as três mudanças em `backend/api/openapi.yaml` (o teste de cobertura falha sem isso).

---

## 6. Frontend

Seguindo `frontend/CLAUDE.md`: camadas `types` → `service` → `queries`, prefetch no servidor com `HydrationBoundary` + `useSuspenseQuery`, filtros na URL com nuqs, ícones só por `@/components/icons`, `PageContainer` para o cabeçalho.

- `app/dashboard/overview/page.tsx`: continua o ponto de entrada; para quem tem a role de proponente (ou a permissão de leitura de projeto), prefetch das duas consultas com o `access` da URL e renderiza `ProponentOverview`; os demais mantêm o texto de hoje.
- Componentes em `features/proponent-overview/components/`: `access-filter.tsx` (segmentado, com os totais), `attention-list.tsx`, `status-counters.tsx`, `recent-projects.tsx`, `activity-feed.tsx`, `proponent-overview.tsx` (monta e trata o estado vazio de 2.7). Cada bloco tem `Suspense`/esqueleto e erro próprios (`QueryError`), para um falhar sem derrubar os outros.
- **"Meus projetos"** (`ProjectsList`) passa a ler `acesso` e `situacao` da URL por nuqs (hoje só há `useState`), e ganha o seletor "Todos / Meus / Compartilhados comigo" e chips de situação. É o destino dos cartões. Rótulos de etapa e de motivo vêm de `stageLabels` e `archiveReasonLabels`.
- **Textos** do feed e dos alertas em pt-BR num único mapa por `kind`. Datas em `America/Cuiaba`, como o resto do painel. Prazo de complementação em dias restantes ("vence em 2 dias", "vence hoje", "vencido").
- **Atualização:** `staleTime` curto e refetch ao voltar à aba; toda mutação de projeto já invalida `projectKeys.all` (D10).
- **Acessibilidade e celular:** alertas são links com texto completo (não só cor); cartões de contagem são botões/links navegáveis por teclado; o layout empilha em uma coluna no celular.

---

## 7. Testes

**Integração** (`internal/app`, testcontainers):
- Titular com projetos em todas as situações: contagens batem; `byAccess` não muda com o filtro.
- Membro de leitura e de edição: `access=member` lista só os convidados; membro de leitura não recebe `attention`; revogar o acesso tira o projeto das contagens e do feed na hora.
- Enquadramento pendente conta em "Com a equipe" (`in_analysis`), nunca vira estado separado.
- Pedido de complementação vencido: `overdue = true`, sem arquivar.
- **Não vazar:** depois de indicar departamento, recusar enquadramento e conferir itens, o feed **não** traz nenhum desses eventos, nem nome de analista, departamento, CPF ou IP.
- D3: Super Admin e Analista chamando `overview`/`activity` recebem só o que é deles (nada de rascunhos alheios).
- D7: convidado não vê eventos anteriores ao convite.
- Valor inválido de `access` ou `limit`: 422.
- Cobertura do OpenAPI.

**Unidade:** a função que monta `kind`/`data` do feed (cada ação permitida, ação fora da lista ignorada) e o agrupamento dos cartões no frontend, se virar função pura. O frontend não tem suíte automatizada: percorrer no navegador (Proponente titular, membro de leitura, membro de edição, sem projeto), em tema claro e escuro e no celular.

---

## 8. Fases de entrega

| Fase | Conteúdo | Pronto quando |
|---|---|---|
| **1** | `access` na listagem (4.3), regra de alcance (D3), testes | Filtro funciona; `make test` verde |
| **2** | `GET /projects/overview`: contagens, valores e atenção; OpenAPI; testes | Cobertura de rotas e testes da seção 7 (resumo) passam |
| **3** | `GET /projects/activity` com lista permitida; OpenAPI; teste de não vazar | Teste de vazamento passa |
| **4** | Frontend: camadas de API e "Meus projetos" com filtros na URL | Cartões de destino funcionam |
| **5** | Frontend: a tela de visão geral (blocos, filtro, estado vazio) | Percorrida no navegador |
| **6** | Atualizar documentação (seção 10) | — |
| **7 (opcional)** | `DRAFT_RETENTION_DAYS` e o aviso "será excluído em N dias" (D8) | Só se pedido |

Cada fase termina com `make test` e `make lint` (backend) e `bun run typecheck`, `lint` e `format:check` (frontend), e commit próprio.

---

## 9. Riscos e cuidados

1. **Vazar dado interno pelo feed.** É o risco principal: `audit_logs` mistura o que é do Proponente com o que é da equipe. Mitigação: lista permitida, dados montados no servidor, nunca o `payload`, e o teste de vazamento. Evento novo no sistema **não** aparece sozinho: precisa entrar na lista de propósito.
2. **Alerta que vira ruído.** Muitos itens em "Precisa da sua atenção" fazem o Proponente ignorar o bloco. Por isso o limite de 20, a ordem por urgência e a regra de só mostrar o que ele pode agir.
3. **Prazo que parece estar valendo.** Entre o `dueAt` e o arquivamento pelo laço periódico, o projeto aparece como vencido (`overdue`) e a ação "Responder" fica desligada, porque o servidor já recusa a edição. A tela precisa dizer isso, não só mostrar "0 dias".
4. **Custo das pendências por rascunho.** Calcular `Issues` roda a validação inteira. Daí o limite de 5 (D5); reavaliar se a medição mostrar lentidão.
5. **Sem retenção visível.** O Proponente pode perder um rascunho abandonado sem aviso na tela (D8). O e-mail e o aviso de exclusão continuam fora do escopo.
6. **Contagens com o filtro de permissão.** Se a permissão do perfil Proponente for editada para mais restrita, os totais caem junto (comportamento desejado, já que usa a mesma decisão da listagem).

---

## 10. Atualizações de documentação ao final

- `backend/api/openapi.yaml` (rotas novas e `access`).
- `backend/README.md`: as duas rotas, a regra de alcance e a lista permitida do feed.
- `README.md`: uma frase sobre a visão geral do Proponente.
- `frontend/CLAUDE.md`, se a convenção de filtros por nuqs na lista de projetos mudar o que lá está.
- `docs/debitos-tecnicos.md`: registrar D8 (aviso de exclusão de rascunho), o limite de 5 rascunhos de D5 e a visão geral da equipe (hoje só texto).
- `graphify-out/`: regenerar o grafo.
