# Planejamento — Analista responsável pela análise

**Objetivo.** Quando um Analista assume um projeto, só ele continua a análise. Os outros Analistas **veem** o projeto, mas não agem sobre ele. Para continuar, outro Analista precisa **assumir ativamente** o projeto, que passa a ser dele.

Este documento diz o que construir. Não contém código. Estende [planejamento-fluxo-analise-enquadramento.md](planejamento-fluxo-analise-enquadramento.md) (§3.1, decisão D2 e a rota `start-analysis`).

---

## 1. Como é hoje

- `projects.analyst_id` já existe e é gravado em `POST /start-analysis` (`StartAnalysis`, `review_service.go`).
- **Nada impede outro Analista de agir.** O comentário do código diz: "Outro Analista que chame depois não troca o responsável: só passa a trabalhar no projeto (qualquer Analista pode agir)". O teste `TestAssumirAAnalise` fixa esse comportamento.
- As ações do Analista são quatro: conferir itens (`PUT /review/items`), devolver (`POST /complement-requests`), arquivar (`POST /archive`) e indicar departamento (`PUT /department`). Nenhuma confere quem é o responsável.
- O histórico de etapas já guarda o analista de cada trecho (`holder_admin_id`) e a caixa de saída de e-mails já existe. As duas coisas são reaproveitadas.
- Na tela, `canReviewNow` (`review-panel.tsx`) e `AnalystActions` (`staff-actions.tsx`) decidem o que o Analista vê habilitado.

## 2. Decisões

### 2.1 Confirmadas

| # | Decisão |
|---|---|
| R1 | **Quando se pode assumir de outro:** com o projeto em análise ou aguardando o proponente. Não vale com enquadramento pendente (o projeto está com o Revisor) nem em "aguardando parecer" (a análise acabou). |
| R2 | **Conferência ao trocar de responsável:** fica como está. Itens aprovados e reprovados continuam, com o nome de quem decidiu. O novo responsável abre e lê os arquivos só dos itens que ele for decidir (regra D10, que já existe e é por pessoa). |
| R3 | **Aviso:** o responsável anterior recebe e-mail (caixa de saída, lista permitida de campos, sem dado pessoal). A troca aparece também no histórico de etapas e na auditoria. |
| R4 | **Super Admin:** a trava vale também para ele. Para agir sobre o projeto de um Analista, ele assume antes, como qualquer um. |

### 2.2 Premissas adotadas (me diga se discorda)

| # | Premissa |
|---|---|
| P1 | **Ver continua livre.** Ler o projeto, abrir arquivos e ver a conferência não exigem ser o responsável. Abrir/ler arquivo continua registrado por pessoa (`project_file_views`), então quem só olhou não ganha prova de leitura para o dono. |
| P2 | **"Assumir de outro" é uma ação separada** (`POST /take-over`), com confirmação na tela. `start-analysis` continua só para quem **inicia** a análise (projeto sem responsável); chamado por quem não é o responsável, passa a responder 409 em vez de "só passar a trabalhar". |
| P3 | **Sem responsável, quem age primeiro vira o responsável.** Devolver para complementação com o projeto ainda sem responsável (etapa "Aguardando análise de checklist") grava o Analista como responsável. Assim o reenvio volta para ele. Hoje esse caso deixa `analyst_id` vazio. |
| P4 | **Arquivar não troca o responsável.** O arquivamento é terminal; o e-mail D2 segue indo ao responsável gravado (ou a todos os Analistas, se não houver). |
| P5 | **Não cria permissão nem migração de perfil.** `take-over` usa a ação `start-analysis`, que o Analista (e o Super Admin) já têm. |
| P6 | **Quem não é responsável não é bloqueado nas saídas de outros perfis:** desistência (Proponente), negação (Secretário) e decisão do Revisor seguem como estão. |

## 3. Regra

Para as quatro ações do Analista (conferir item, devolver, arquivar, indicar departamento):

1. Projeto **sem responsável** (`analyst_id` nulo): a regra atual vale (conferir exige ter assumido; devolver grava o responsável, P3).
2. Projeto **com responsável igual a quem age:** passa.
3. Projeto **com outro responsável:** `409 not_responsible_analyst`, mensagem em português que diz com quem o projeto está e como seguir ("Este projeto está com Fulano. Assuma o projeto para continuar a análise.").

Assumir de outro (`POST /admin/projects/{id}/take-over`):

| Situação do projeto | Resultado |
|---|---|
| Em análise ou aguardando proponente, responsável é outra pessoa | Troca o responsável. Auditoria `project.take-over` (de, para), histórico, e-mail ao anterior |
| Responsável já é quem chama | Idempotente: devolve o projeto, nada muda e nada é gravado |
| Sem responsável, em análise | Equivale a assumir: vira o responsável (como o `ClaimProjectAnalyst` de hoje) |
| Aguardando análise de checklist | 409 `analysis_not_started` (usar "Iniciar análise") |
| Enquadramento pendente | 409 `framing_pending` |
| Aguardando parecer | 409 `project_stage_invalid` |
| Arquivado ou rascunho | 409, como nas outras ações |

Tudo sob a trava da linha (`GetProjectForUpdate`). Duas pessoas assumindo ao mesmo tempo: a segunda vê o resultado da primeira e é tratada como "outro responsável" só se ainda quiser agir; assumir de novo é permitido (quem chegou por último fica com o projeto). A tela mostra a mudança ao recarregar.

## 4. Backend

**Consulta nova (sqlc):** `SetProjectAnalyst` (troca `analyst_id` sem mexer em etapa nem versão, como `ClaimProjectAnalyst`).

**Serviço (`internal/project`):**
- Função única `ensureResponsible(cur, actor)` aplicada em `ReviewItem`, `RequestComplement`, `Archive` (só o arquivamento da equipe; `Withdraw` e `Deny` não passam por ela) e `AssignDepartment`. O nome do responsável vem de `GetAdminByID`, como em `attachReview`.
- `StartAnalysis` deixa de aceitar quem não é o responsável (409 `not_responsible_analyst`, com a mesma mensagem).
- `TakeOverAnalysis` novo, conforme a seção 3, passando por `transition()` (que fecha o trecho do analista anterior e abre o do novo, e satisfaz o gatilho do banco da migração 00033).
- `RequestComplement` grava o responsável quando não há (P3), na mesma transação e antes de `transition`.
- Visão do projeto: `Analyst` ganha `isMe` (verdadeiro quando o responsável é quem consulta), para a tela não comparar ids. Vale também para o resumo da fila.
- E-mail `analysis_taken_over` ao responsável anterior (`GetNotifiableAdminEmailByID`; ativo e não bloqueado). Campos: código, instrumento, título, quem assumiu (nome), data, link. Sem CPF, telefone ou e-mail pessoal. Chave de deduplicação: tipo + projeto + data/hora da troca + destinatário.
- Feed da fila e contadores (`queue_activity.go`, `queue_overview.go`, `queue_me.go`): `project.take-over` aparece como "assumiu o projeto de X" e conta em "Assumidas" de quem assumiu. A visão geral do Analista descreve a regra nova.
- Rota registrada no `handler.go` com `g(ActionStartAnalysis)`, e documentada no `backend/api/openapi.yaml` (há teste que exige).

**Sem migração.** Nenhuma coluna nova.

## 5. Frontend (`features/projects`)

- `types.ts`: `AnalystInfo.isMe`; `takeOverAnalysis` em `service.ts`.
- `canReviewNow` passa a exigir "sou o responsável" (`project.analyst?.isMe`). Isso desabilita, de uma vez, aprovar/reprovar na conferência e no visualizador de arquivo.
- `AnalystActions`: para o Analista que **não** é o responsável, esconde "Devolver", "Arquivar" e "Indicar departamento" e mostra o botão **"Assumir projeto"** com confirmação (diz de quem é e o que acontece: "O projeto passa a ser seu; o responsável atual será avisado por e-mail"). Disponível só nas situações de R1.
- Faixa no topo do projeto ("Este projeto está com Fulano. Você pode visualizá-lo, mas para continuar a análise é preciso assumi-lo."), em `staff-banner.tsx`.
- `StartAnalysisButton`: o texto de sucesso continua; erro `not_responsible_analyst` cai no tratamento de erros da equipe com a mensagem do servidor.
- A fila e a linha da lista mostram o responsável; opcional: filtro "Meus projetos" (ver 8, item aberto).

## 6. Testes

**Backend (integração, `internal/app`):**
1. Responsável age normalmente nas quatro ações.
2. Outro Analista recebe 409 `not_responsible_analyst` nas quatro ações e em `start-analysis`; nada muda no banco (versão, itens, etapa).
3. Outro Analista **lê** o projeto, a conferência e baixa arquivo (200).
4. `take-over`: troca o responsável; histórico de etapas fecha o trecho do anterior e abre o do novo; auditoria `project.take-over`; e-mail na caixa de saída para o anterior e sem dado pessoal; o novo agora age e o antigo é bloqueado.
5. `take-over` em `awaiting_proponent`: troca; o reenvio do proponente volta para o novo responsável.
6. `take-over` bloqueado em checklist, enquadramento pendente, parecer, arquivado.
7. Idempotência (assumir o que já é seu); duas trocas simultâneas terminam com um único responsável e histórico consistente.
8. Conferência preservada na troca; o novo responsável precisa abrir/ler arquivos dos itens que decidir (D10).
9. Super Admin é bloqueado em projeto de Analista e consegue assumir (R4).
10. P3: devolver sem responsável grava o responsável.
11. Teste existente `TestAssumirAAnalise` (e os que usam um segundo Analista para agir) são reescritos para a regra nova. Contagem de rotas/OpenAPI e matriz de permissões (`TestQuemPodeDecidirEmProjetos`) atualizadas.
12. `make test`, `make lint`, `make sqlc` limpos; cobertura de `internal/authz` ≥ 80%.

**Frontend:** sem suíte automatizada; `bun run typecheck`, `lint` e `format:check` na cópia do scratchpad (a `node_modules` do repositório é de root e vazia).

**Roteiro manual (você testa nas telas),** com dois Analistas (A e B) e um projeto enviado:
1. A abre o projeto: vê "Iniciar análise"; inicia. B abre o mesmo projeto: vê a faixa "está com A", conferência só leitura, sem Devolver/Arquivar/Indicar, com "Assumir projeto".
2. B tenta aprovar um item e devolver: não consegue (botões ausentes); por API, 409.
3. B assume: o projeto passa a ser de B; A recebe e-mail (confira a caixa de saída/log) e perde as ações; o histórico de etapas mostra a troca.
4. Com o projeto devolvido ao proponente: B assume, o proponente reenvia, e a análise volta para B.
5. Com enquadramento pendente: "Assumir" não aparece.
6. Super Admin: não consegue agir sobre projeto de A até assumir.

## 7. Documentação a atualizar

`docs/planejamento-fluxo-analise-enquadramento.md` (D2 e a tabela de transições: trocar "Idempotente para o mesmo Analista; outro Analista recebe 409" pela regra nova e incluir `take-over`), `docs/roteiro-aceite-fluxo-equipe.md`, `docs/planejamento-visao-geral-analista.md` (contagem de "Assumidas"), `backend/README.md` e `backend/api/openapi.yaml`.

## 8. Fases

Nenhum commit até tudo estar pronto e testado por você nas telas.

| Fase | Entrega | Estado |
|---|---|---|
| 1 | Backend: `ensureResponsible` nas 4 ações, `start-analysis` restrito, P3, consulta `SetProjectAnalyst`, `isMe` na visão, filtro `analyst=me`, OpenAPI | **feita** (testes existentes ajustados; testes novos de bloqueio ficam na fase 3) |
| 2 | Backend: `take-over`, histórico, auditoria, e-mail, feed/contadores, fila de reenviados, OpenAPI | **feita** |
| 3 | Testes de integração (seção 6) e ajuste dos testes existentes | **feita** (`project_take_over_test.go`; suíte completa com `-race` e lint verdes) |
| 4 | Frontend: `canReviewNow`, ações, botão e diálogo "Assumir projeto", faixa, responsável na fila | **feita** (a fila já tinha o filtro "só os que assumi"; só foi renomeado para "Meus projetos") |
| 5 | Documentação (seção 7), `typecheck`/`lint`/`format`, roteiro manual com você | documentação e roteiro (F2 de `roteiro-aceite-fluxo-equipe.md`) feitos; falta o teste manual nas telas |

### Pontos em aberto resolvidos
- **Filtro "Meus projetos":** incluído. A API aceita `analyst=me` na listagem (fase 1) e a tela ganha a aba/filtro na fase 4.
- **Responsável bloqueado ou inativado:** sem regra especial. O caminho é assumir o projeto (`take-over`); excluir o usuário já zera o responsável.
