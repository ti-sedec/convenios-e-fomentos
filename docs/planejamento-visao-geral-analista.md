# Planejamento — Visão geral do Analista

Complementa o [planejamento da visão geral do proponente](planejamento-visao-geral-proponente.md), o [fluxo de análise e enquadramento](planejamento-fluxo-analise-enquadramento.md) e o [histórico de etapas](planejamento-historico-etapas.md). Define o que a tela `/dashboard/overview` mostra para o **Analista**, de onde vem cada dado e em que ordem construir. Não contém código; contém decisões, contratos, ordem de entrega e testes.

**Escopo:** só o Analista (quem tem a ação `admin::projects.start-analysis`). Revisor do Departamento, Secretário de Estado e Supervisor Administrador seguem com o texto de boas-vindas; quando chegarem, reaproveitam os blocos que fizerem sentido. Painel de prazos entre projetos (T29) e exportação (T30) continuam fora.

**Ideia da tela:** o Analista abre o painel querendo saber **"o que preciso fazer agora?"** e depois **"como está a fila?"**. A tela prioriza trabalho, não só conta projetos.

---

## 1. Ponto de partida

| Pergunta | Hoje | Limite |
|---|---|---|
| O que a tela mostra? | Um parágrafo de boas-vindas (a equipe não tem visão geral) | Nada de dados |
| Como o Analista trabalha? | `/dashboard/analise`: 7 abas por situação, filtro "na etapa há N dias ou mais" e ordenação por tempo parado | Nada **prioriza** o que fazer primeiro; cada aba faz uma consulta só para o contador (T26) |
| Quem enxerga o quê? | Todos os projetos **enviados ou arquivados**, de qualquer proponente; rascunho não. Qualquer Analista age em qualquer projeto (D13) | "Meu" só existe como `analyst_id` (quem assumiu a análise) |
| Quanto tempo parado? | Histórico de etapas (`project_stage_history`): cada trecho tem entrada, saída e quem estava com o projeto | A fila mostra o tempo na **etapa**; o tempo com o **responsável atual** (analista, departamento) não aparece |
| O projeto voltou por recusa do Revisor? | `project_framings` guarda cada rodada, com decisão e justificativa | A listagem só diz `pending` ou `confirmed`; recusado vira "sem enquadramento" e some do radar (T14: o Analista nem é avisado) |
| O proponente reenviou? | O trecho aberto do histórico guarda a transição (`project.resubmit`) | Continua assim mesmo depois de o Analista conferir, então não serve sozinho |
| Tudo aprovado, falta indicar o departamento? | `Review.AllApproved`, calculado em Go a partir do checklist e das conferências gravadas | Só no detalhe de cada projeto; não dá para contar em SQL |
| Enviado fora do prazo? | `projects.late_submission` | Só no detalhe do projeto, não na listagem |
| O que "parado" significa? | Nada decide isso; T29 já pedia a decisão | — |
| O que cada Analista fez? | `audit_logs`, com ator, ação e data | Ninguém consulta por pessoa |

---

## 2. O que a tela mostra

Uma coluna de leitura, do mais urgente para o menos urgente, com um seletor **Toda a equipe / Só os meus** no topo.

```
Olá, <nome>                                          [ Toda a equipe | Só os meus ]

┌ Precisa de você ──────────────────────────────────────────────────────┐
│ Aguardando assumir (5)      Reenviados pelo proponente (2)             │
│ Recusados pelo Revisor (1)  Tudo aprovado, falta indicar o depto. (3)  │
│ Parados além do limite (4)  Prazos de correção a vencer (2)            │
└────────────────────────────────────────────────────────────────────────┘

[Aguardando análise 6] [Em análise 9] [Em enquadramento 4] [Aguardam proponente 3] [Aguardando parecer 7] [Arquivados (30 dias) 5]

┌ Atividade recente (equipe) ──────┐  ┌ Meu resumo ────────────────────┐
│ 15 últimos marcos                │  │ 7 dias · 30 dias               │
└──────────────────────────────────┘  └────────────────────────────────┘
```

### 2.1 Seletor "Toda a equipe / Só os meus" (decisão do usuário)
"Meu" é o projeto cuja análise o Analista **assumiu** (`analyst_id`). Vale para os contadores e para os blocos de trabalho, **menos** "Aguardando assumir": projeto que ninguém assumiu ainda não tem dono, então esse bloco mostra sempre a fila inteira, com um aviso na tela ("não depende do filtro"). Fica na URL (`?escopo=meus`, nuqs).

### 2.2 Precisa de você
Cada bloco mostra os **5 mais antigos** (o mais urgente primeiro), com o total e "Mostrar mais" (até 15 na tela). Cada linha tem título, código, instituição, há quanto tempo está naquele ponto e um botão de ação. Em ordem de urgência:

| Bloco | O que entra | Ação |
|---|---|---|
| **Recusados pelo Revisor** | Em análise, sem departamento, e a **última** rodada de enquadramento foi `refused` (nome do departamento e data da recusa; a justificativa fica no detalhe) | Reindicar departamento |
| **Reenviados pelo proponente** | Em análise, com o último trecho aberto vindo de `project.resubmit` e **nenhuma conferência** (`project.review-item`) depois do reenvio | Conferir os itens alterados |
| **Aguardando assumir** | Etapa `awaiting_checklist`. Selo "Fora do prazo" quando `lateSubmission` | Assumir a análise |
| **Tudo aprovado, falta indicar** | Em análise, sem enquadramento em curso e sem departamento, com `AllApproved` | Indicar departamento |
| **Parados além do limite** | Tempo com o responsável atual passou do limite da etapa (2.3), exceto "aguardando proponente" | Abrir o projeto |
| **Prazos de correção a vencer** | `awaiting_proponent` com prazo (`due_at`) nas próximas 48 horas, ou vencido ainda não arquivado | Só informativo (quem age é o proponente) |

Um mesmo projeto pode aparecer em dois blocos (por exemplo, reenviado **e** parado); cada bloco responde a uma pergunta diferente. Bloco vazio não aparece. Se todos estiverem vazios, a tela diz "Nada pede a sua ação agora." em vez de um espaço em branco.

### 2.3 Limite de "parado", por etapa (decisão do usuário)
O limite vira um **parâmetro de regra** (`stalled_after_days`), versionado por vigência como os demais, e vale por etapa de trabalho:

| Etapa de trabalho | Quem está com o projeto | Limite inicial sugerido |
|---|---|---|
| Aguardando análise de checklist (`awaiting_checklist`) | Equipe (fila de entrada) | 2 dias |
| Em análise (`in_analysis`) | Analista | 7 dias |
| Em enquadramento (`framing`: análise com enquadramento pendente) | Departamento (Revisor) | 5 dias |
| Aguardando parecer (`awaiting_opinion`) | Departamento | 15 dias |
| Aguardando proponente | Proponente | **Sem limite**: já tem prazo por regra (3 dias) |

Os números são só um ponto de partida para o Supervisor ajustar; **confirmar na revisão**. O tempo é o do **trecho aberto** do histórico (tempo com o responsável atual, em horas corridas). Ele pode diferir do "Na etapa há" da fila, que soma os trechos seguidos da mesma etapa (a análise e o enquadramento contam juntos ali). As duas medidas ficam; a diferença é anotada na tela ("com o Revisor há 3 dias").

### 2.4 Contadores
Seis cartões que levam à aba correspondente de `/dashboard/analise`, mesmas regras das abas atuais, com o seletor da 2.1. Resolvem de quebra o T26 (uma consulta em vez de uma por aba) se a fila passar a usá-los depois.

| Cartão | Regra |
|---|---|
| Aguardando análise | `submitted`, `awaiting_checklist` |
| Em análise | `submitted`, `in_analysis`, sem enquadramento pendente |
| Em enquadramento | `submitted`, enquadramento `pending` |
| Aguardam proponente | `submitted`, `awaiting_proponent` |
| Aguardando parecer | `submitted`, `awaiting_opinion` |
| Arquivados (30 dias) | `archived` há até 30 dias; o total completo continua na aba "Arquivados" |

Em "Só os meus", "Aguardando análise" fica fora (ninguém é dono ainda).

### 2.5 Atividade recente (equipe)
Os 15 marcos mais novos dos projetos que o Analista enxerga. **Diferente do feed do proponente, pode mostrar o que é interno**, porque quem lê é a própria equipe, e cobre a lacuna do T14. Lista permitida, com o nome de quem agiu (só da equipe; o proponente aparece como "o proponente", sem CPF):

| Evento | Texto |
|---|---|
| `project.submit` | "Novo projeto enviado" (selo de fora do prazo) |
| `project.resubmit` | "O proponente reenviou as correções" |
| `project.start-analysis` | "<nome> assumiu a análise" |
| `project.complement-request` | "<nome> devolveu para correção (N itens)" |
| `project.assign-department` | "<nome> indicou o departamento <X>" |
| `project.framing-confirm` | "O Revisor confirmou o enquadramento" |
| `project.framing-refuse` | "O Revisor recusou o enquadramento" (sem a justificativa no feed) |
| `project.archive` | "Projeto arquivado: <motivo>" (inclusive por prazo vencido, "pelo sistema") |

Fora do feed: `review-item`, `file-*`, `receipt-download`, `update`, `create`, `duplicate`, `delete`, `member-*`, declarações. Cada linha leva ao projeto.

### 2.6 Meu resumo (opção B, decisão do usuário)
Só o **próprio** Analista, sem comparar com ninguém e sem que outra pessoa veja: nos últimos 7 e 30 dias, quantas análises **assumi**, quantas **complementações pedi**, quantos **departamentos indiquei** e quantos projetos **arquivei**. Vem de `audit_logs` filtrado pela pessoa. A rota devolve **sempre e somente** os números de quem chamou. Um painel por pessoa para o Supervisor é outra decisão (junto com o T29), fora daqui.

### 2.7 Lugar para o que vem depois
Cada bloco é independente (uma consulta, um esqueleto, um erro). Revisor e Secretário entrarão com os seus próprios blocos na mesma página, por perfil (decisão do usuário: só o Analista por enquanto).

---

## 3. Decisões

### 3.1 Já tomadas (nesta conversa)
1. "Meu" = a análise que a pessoa **assumiu** (`analyst_id`).
2. O limite de "parado" é **por etapa**, e vira parâmetro de regra.
3. "Meu resumo" só para o próprio Analista (opção B).
4. Só o Analista nesta rodada.
5. Planejar antes de implementar.

### 3.2 Com recomendação adotada (confirmar na revisão)

| # | Decisão | Recomendação adotada | Alternativa |
|---|---|---|---|
| E1 | Rotas | Três, novas e só da equipe: `GET /admin/projects/queue/overview`, `/queue/activity` e `/queue/me`, todas com a ação `admin::projects.start-analysis`. As rotas do proponente (`/projects/overview`, `/projects/activity`) **não mudam** | Reaproveitar `/projects/overview` com comportamento por perfil (mistura dois públicos e quebra a regra D3 do outro plano) |
| E2 | Alcance | A permissão da ação **e** só projetos enviados ou arquivados (`submittedFilter`), como a listagem; nunca rascunho (T19 continua valendo só para o Super Admin) | Só a permissão |
| E3 | Medida do "parado" | Duração do **trecho aberto** do histórico, em horas corridas (tempo com o responsável atual) | Reusar `stageSeconds` (tempo na etapa), que mistura análise e enquadramento |
| E4 | "Reenviado" | Último trecho aberto vindo de `project.resubmit` **e** sem `project.review-item` depois dele (consulta em `audit_logs`). Sem isso, o projeto ficaria como "reenviado" para sempre, porque conferir não muda o estado | Só o trecho aberto |
| E5 | "Recusado pelo Revisor" | Última rodada de `project_framings` com `decision = 'refused'`, projeto em análise sem departamento. A primeira indicação nova abre outra rodada e o projeto sai do bloco | Marcar uma coluna no projeto |
| E6 | "Tudo aprovado, falta indicar" | Calculado em Go (`AllApproved`) só sobre os **30 candidatos mais antigos** (em análise, sem departamento nem enquadramento); mais que isso, a tela diz "30 ou mais". O custo é o mesmo do contador de pendências de rascunho do outro plano (D5) | Calcular todos; ou gravar um indicador atualizado a cada conferência (escrita nova em um ponto crítico) |
| E7 | "Aguardando assumir" | Ignora o seletor "Só os meus", com aviso na tela (2.1) | Esconder o bloco em "Só os meus" |
| E8 | "Aguardando proponente" | Fora do "parado" (tem prazo próprio); entra em "Prazos a vencer", com janela fixa de **48 horas** | Janela configurável |
| E9 | `lateSubmission` | Entra em `Summary` (listagem), para o selo "Fora do prazo" na fila e na visão geral. É dado que o proponente já vê no detalhe do próprio projeto | Só na visão geral |
| E10 | Quem ajusta o parâmetro | O parâmetro nasce com padrão; editar exige `admin::settings.rule-parameters.update`, que **hoje só o Super Admin tem**. Dar essa ação ao Supervisor é decisão de perfil, fora deste plano | Já nascer com a ação no Supervisor |
| E11 | Frontend | Consultas no cliente, como as demais telas (o sistema não faz prefetch no servidor); chaves sob as chaves de projeto para que qualquer ação do Analista (assumir, conferir, devolver, indicar) já atualize a visão geral | Prefetch no servidor |

---

## 4. Contratos de API

Todas sob `/api/admin`, `{data, meta}`. Parâmetro comum **`scope`**: `team` (padrão) ou `mine`; valor inválido é 422 com o campo.

### 4.1 `GET /projects/queue/overview?scope=`
```
data: {
  counts: { awaiting_checklist, in_analysis, framing_pending, awaiting_proponent, awaiting_opinion, archived_30d },
  limits: { awaiting_checklist, in_analysis, framing, awaiting_opinion },   // dias vigentes (de stalled_after_days)
  blocks: {
    refused:      { total, items: [ { projectId, title, code, institution, department, refusedAt } ] },
    resubmitted:  { total, items: [ { projectId, title, code, institution, sinceSeconds, analyst } ] },
    entry:        { total, items: [ { projectId, title, code, institution, sinceSeconds, late, overLimit } ] },
    ready:        { total, capped, items: [ { projectId, title, code, institution, sinceSeconds } ] },
    stalled:      { total, items: [ { projectId, title, code, institution, stage, holder, sinceSeconds, limitDays, analyst } ] },
    deadlines:    { total, items: [ { projectId, title, code, institution, dueAt, overdue } ] }
  }
}
```
Cada bloco traz até 15 itens, do mais urgente para o menos. `analyst {id, name}` só quando alguém assumiu. Nenhum CPF, e-mail ou telefone do proponente.

### 4.2 `GET /projects/queue/activity?scope=&limit=`
`limit` padrão 15, máximo 30. `[ { id, at, kind, by: {type: admin|proponent|system, name?}, projectId, title, code, data } ]`, com `kind` e `data` montados no servidor a partir da lista da 2.5, nunca o `payload` da auditoria.

### 4.3 `GET /projects/queue/me`
`{ last7: {assumed, complements, indicated, archived}, last30: {...} }`, só de quem chama. Sem parâmetro de pessoa.

### 4.4 `lateSubmission` em `GET /projects`
Campo novo em cada linha, para o selo.

### 4.5 Parâmetro de regra `stalled_after_days`
Valor JSON `{ "awaiting_checklist": n, "in_analysis": n, "framing": n, "awaiting_opinion": n }`, cada `n` inteiro de 1 a 365, as quatro chaves obrigatórias. Vigência e validação como as chaves existentes (`complement_days` etc.). Padrão inicial: 2, 7, 5, 15 (2.3).

---

## 5. Backend

- **Parâmetro (rules):** nova chave em `rules/keys.go`, validação das quatro chaves, padrão inicial e uma função que devolve os limites vigentes hoje. O serviço de projetos recebe essa leitura por injeção (como já recebe os demais), sem importar o pacote de regras inteiro.
- **Alcance (E2):** o filtro da permissão `start-analysis` somado a `status IN (submitted, archived)`; `scope=mine` soma `analyst_id = pessoa`. Uma função única monta isso, como o `scopeConds` do proponente.
- **Contagens:** uma consulta agregada por `(status, stage, framing_status)`; `archived_30d` pelo `archived_at`.
- **Blocos em SQL** (cada um uma consulta curta, limitada a 15 e com a contagem total):
  - *entry:* `awaiting_checklist` com o início do trecho aberto.
  - *resubmitted* (E4): em análise, trecho aberto com `transition = 'project.resubmit'`, e `NOT EXISTS` de `project.review-item` em `audit_logs` (entidade e id do projeto) depois de `entered_at`.
  - *refused* (E5): última rodada de `project_framings` com `refused` e sem rodada pendente.
  - *stalled:* duração do trecho aberto contra o limite da etapa de trabalho do projeto (a etapa vem do estado: `awaiting_checklist`; `in_analysis` sem enquadramento; enquadramento pendente; `awaiting_opinion`).
  - *deadlines:* `project_complement_requests` abertos com `due_at <= now() + 48h`.
- **Bloco `ready` (E6):** candidatos em SQL (30 mais antigos), depois o cálculo de `AllApproved` em Go com o mesmo código do detalhe do projeto; falhas de um projeto são registradas no log e o projeto sai do bloco, sem derrubar a resposta.
- **Feed:** consulta em `audit_logs` (`entity = 'project'`, ações da lista permitida, projetos do alcance), nome do administrador resolvido pelo cadastro, `kind` e `data` montados por função pura testável. Reaproveita o desenho de `activity.go`, com lista **própria** (a do proponente não pode crescer por causa da equipe).
- **Meu resumo:** `COUNT` em `audit_logs` por `actor_id = pessoa` e ação, em duas janelas (7 e 30 dias). Arquivamento só conta o feito pela pessoa (não o do sistema).
- **`lateSubmission`:** coluna a mais na listagem.
- **Índice:** `audit_logs (entity, entity_id, created_at DESC)` só se a medição pedir; o filtro por `actor_id` já tem índice (`audit_logs_actor_idx`).
- **OpenAPI** para tudo (o teste de cobertura falha sem isso). **Sem migração** de tabela; o parâmetro nasce pelo `EnsureDefaults` das regras.

---

## 6. Frontend

Mesmas convenções do plano do proponente: camadas `types` → `service` → `queries`, ícones só por `@/components/icons`, `PageContainer`, filtros na URL (nuqs), consultas no cliente.

- **Página:** `app/dashboard/overview/page.tsx` passa a escolher pela sessão: Proponente (`isProjectProponentSession`) → tela do proponente; Analista (ação `start-analysis`) → tela nova; os demais → texto. Quem é as duas coisas (Super Admin) cai na do Analista, pois é a do papel de trabalho (**confirmar**).
- **Feature nova** `features/analyst-overview/` (`api/` e `components/`): `scope-filter.tsx`, `work-blocks.tsx` (os seis blocos, com "Mostrar mais"), `queue-counters.tsx`, `staff-activity-feed.tsx`, `my-summary.tsx`, `analyst-overview.tsx`.
- **Ações:** os botões levam ao detalhe do projeto (`/dashboard/projetos/{id}`), onde o Analista já assume, confere, devolve e indica; a visão geral não repete essas ações.
- **Contadores:** levam a `/dashboard/analise?aba=…` (as abas já são lidas da URL).
- **Atualização:** `staleTime` curto, refetch ao voltar à aba; as ações do Analista já invalidam as chaves de projeto, então a visão geral acompanha.
- **Acessibilidade e celular:** estado sempre em texto (nada só por cor: "Fora do prazo", "Parado há 9 dias, limite 7"); blocos em uma coluna no celular; links e botões com rótulo completo.
- **Fila (`/dashboard/analise`):** ganha o selo "Fora do prazo" (`lateSubmission`). Nada além disso nesta rodada.

---

## 7. Testes

**Integração** (`internal/app`, testcontainers), com os perfis semeados (não perfis ad hoc), como nos testes de visibilidade da equipe:
- Contagens por etapa batem com as abas da fila; `scope=mine` só conta o que o Analista assumiu e deixa "aguardando análise" de fora.
- *Recusado:* depois de indicar, recusar e antes de reindicar, o projeto está em `refused`; a nova indicação o tira.
- *Reenviado:* devolver, reenviar → entra; uma conferência (`review-item`) depois do reenvio → sai.
- *Pronto para indicar:* com tudo aprovado entra; reprovar um item o tira; limite de 30 e o aviso `capped`.
- *Parado:* envelhecer o trecho aberto (como em `project_history_test.go`) e ver o projeto entrar ao passar do limite da **sua** etapa, não da vizinha; "aguardando proponente" nunca entra; mudar o parâmetro muda o resultado.
- *Prazos:* complementação com `due_at` em 40 horas entra; em 60 horas não.
- **Alcance:** Revisor, Secretário, Supervisor e Proponente recebem 403 nas três rotas; rascunho nunca aparece (nem para o Super Admin, que passa só pelo que é enviado nestas rotas).
- **Não vazar:** a resposta de `/queue/overview` e `/queue/activity` não contém CPF, e-mail, telefone nem o texto da justificativa de recusa; o feed não traz `review-item` nem ação de arquivo.
- *Meu resumo:* conta só as ações da pessoa que chamou (outro Analista agindo não entra), arquivamento por prazo vencido não conta; janelas de 7 e 30 dias.
- Valor inválido de `scope` ou `limit`: 422. Cobertura do OpenAPI. `lateSubmission` na listagem.
- **Parâmetro:** validação das quatro chaves, vigência e padrão inicial.

**Unidade:** montagem de `kind`/`data` do feed (cada ação permitida; ação fora da lista ignorada); escolha da etapa de trabalho de um projeto.
O frontend não tem suíte automatizada: percorrer no navegador com um Analista (fila cheia, fila vazia, "Só os meus"), em tema claro e escuro e no celular.

---

## 8. Fases de entrega

| Fase | Conteúdo | Pronto quando |
|---|---|---|
| **1 (feita)** | Parâmetro `stalled_after_days` (chave, validação, padrão, leitura) e `lateSubmission` na listagem; OpenAPI; testes | Parâmetro aparece na administração e `make test` verde |
| **2** | `GET /projects/queue/overview`: alcance, contagens e os blocos *entry*, *resubmitted*, *refused*, *stalled* e *deadlines*; OpenAPI; testes | Testes da seção 7 desses blocos passam |
| **3** | Bloco *ready* (E6) | Testes do bloco e do limite passam |
| **4** | `GET /projects/queue/activity` com lista permitida; teste de não vazar | Teste de vazamento passa |
| **5** | `GET /projects/queue/me` | Testes das janelas passam |
| **6** | Frontend: camada de API, contadores, blocos de trabalho, seletor e estados vazios; selo na fila | Percorrida no navegador |
| **7** | Frontend: feed da equipe e "Meu resumo"; ajustes de tema claro e celular | Percorrida no navegador |
| **8** | Documentação (seção 10) | — |

Cada fase termina com `make test` e `make lint` (backend) e `bun run typecheck`, `lint` e `format:check` (frontend), e commit próprio.

---

## 9. Riscos e cuidados

1. **Prioridade errada vira ruído.** Seis blocos podem cansar. Por isso 5 itens por bloco, bloco vazio some e o texto diz o que fazer. Reavaliar com o uso real (talvez fundir "parados" com os demais).
2. **Limite mal escolhido.** Os padrões 2/7/5/15 são suposição. Como são parâmetro, o ajuste não exige código; a tela mostra o limite junto do tempo para ninguém ter de adivinhar.
3. **Duas medidas de tempo** (trecho aberto na visão geral, tempo na etapa na fila). Documentadas na tela; se confundir, unificar a fila depois.
4. **Custo do bloco *ready*.** Roda a conferência inteira de cada candidato. O limite de 30 e o registro de falhas contêm o custo; se pesar, gravar um indicador.
5. **Vazamento pelo feed.** `audit_logs` guarda IP, CPF mascarado e justificativas. Lista permitida, dados montados no servidor, nunca o `payload`, e o teste de não vazar. O feed da equipe **não** reaproveita a lista do proponente, para uma não crescer por causa da outra.
6. **Métrica pessoal.** "Meu resumo" fica só com a própria pessoa e a rota não aceita parâmetro de outra; ninguém, nem o Supervisor, a consulta. Se um dia houver painel por pessoa, é decisão separada (T29).
7. **Quem edita o limite.** Hoje só o Super Admin (E10). Se o Supervisor deve ajustar, dar-lhe a ação é decisão de perfil.
8. **Avisos que continuam faltando.** A visão geral é consulta, não aviso: o e-mail ao Analista quando o Revisor recusa (T14) e quando entra projeto novo (T11) continuam como débitos; a tela os alivia, mas não os substitui.
9. **Dado estimado.** Trechos `source = initial` (projetos anteriores ao histórico) dão "parado" aproximado. Só existem em desenvolvimento.

---

## 10. Atualizações de documentação ao final

- `backend/api/openapi.yaml` (rotas novas, `lateSubmission`, parâmetro `scope`).
- `backend/README.md`: as três rotas, o alcance, a lista permitida do feed da equipe e o parâmetro `stalled_after_days` (junto da tabela de parâmetros de regra, se existir).
- `README.md`: uma frase sobre a visão geral do Analista.
- `frontend/CLAUDE.md`: a estrutura de `features/analyst-overview`.
- `docs/debitos-tecnicos.md`: **T26** (se a fila passar a usar as contagens, resolver; senão, registrar que a visão geral já as tem), **T29** (a decisão do que é "parado" foi tomada aqui), um item novo para "visão geral de Revisor, Secretário e Supervisor", e o limite de 30 do bloco *ready*.
- `graphify-out/`: `graphify update .` (sem custo de IA).
