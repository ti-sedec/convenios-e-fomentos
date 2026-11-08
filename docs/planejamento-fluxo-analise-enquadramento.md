# Planejamento — Fluxo de análise, enquadramento e arquivamento

Fonte da verdade do fluxo: [bpmn/convenios-e-fomento.bpmn](bpmn/convenios-e-fomento.bpmn). Este documento diz o que construir para o sistema cumprir aquele diagrama, partindo do que já existe no código. Não contém código; contém decisões, modelo, contratos, ordem de entrega e testes.

**Escopo:** do envio do projeto até "Projeto enquadrado" (Revisor do Departamento confirma o departamento), mais as saídas a qualquer momento (prazo vencido, falta de documentos, desistência do Proponente, negação do Secretário de Estado). Parecer técnico, formalização e prestação de contas ficam para outros documentos.

---

## 1. Ponto de partida: o que o BPMN pede × o que o código já faz

Legenda: **Existe** = já funciona e tem teste; **Ajustar** = existe, mas diverge do BPMN; **Novo** = não existe.

| Passo do BPMN | Situação | Onde está / o que falta |
|---|---|---|
| Preencher formulário, anexar documentos, assinar declarações (MT-ID) | Existe | `project` (rascunho, checklist, declarações) |
| Enviar projeto (código, versão 1, comprovante PDF, e-mail ao Proponente) | Existe | `Submit` em `project/submit.go` |
| **Assumir a análise** ("Aguardando análise de checklist" → "Em análise") | **Novo** | Nenhuma rota faz essa transição. `in_analysis` só é gravado no reenvio (`Resubmit`) |
| **Analisar documento a documento** (abrir antes de aprovar) e cada grupo de informações | **Novo** | Não há situação de conferência por item nem registro de "documento aberto". O planejamento original já deixou isso para "fase seguinte" |
| Devolver para complementação (+ e-mail ao Proponente) | Existe | `RequestComplement`; e-mail em `notifyComplementRequested` |
| Complementar itens devolvidos e reenviar (nova versão) | Existe | `Resubmit`; só os itens devolvidos ficam editáveis |
| Prazo de 3 dias vencido arquiva | Existe | `ExpireComplements` (laço periódico em `app.go`) |
| Arquivar por falta de documentos obrigatórios (Analista) | Existe | `Archive` com motivo `missing_documents`; o Analista já tem a ação |
| Desistência do Proponente | Existe | `Withdraw`. **Ajustar:** hoje não envia e-mail (`notifyArchived` é pulado para `withdrawn`) |
| Negação pelo Secretário de Estado | **Ajustar** | `Deny` e a rota existem, mas a ação está no Supervisor Administrador. **Não existe o perfil Secretário de Estado** |
| **Indicar departamento** (só com tudo aprovado) | **Novo** | A coluna `projects.department_id` e a condição `admin::same-department` existem, mas nenhuma rota grava o departamento |
| Encaminhar ao Revisor do Departamento | **Novo** | A visibilidade sai de graça de `same-department` quando `department_id` for gravado |
| Analisar enquadramento (confirmar / recusar com justificativa) | **Novo** | O perfil Revisor só tem permissões sobre a entidade genérica `document`, que não tem relação com projeto |
| E-mail ao Supervisor Administrador (regular; recusa) | **Novo** | Nenhum e-mail para a equipe existe |
| E-mail de arquivamento a Proponente, Revisor, Analista e Supervisor | **Ajustar** | Hoje só vai ao Proponente (e-mail principal) |
| Supervisor Administrador só recebe e-mails | **Ajustar** | Hoje tem `request-complement`, `archive` e `deny` (migration 00022). Passa a ser somente leitura |
| Telas da equipe (fila do Analista, do Revisor, visão do Secretário) | **Novo** | O frontend só tem o fluxo do Proponente. Nada de devolver, arquivar ou negar na interface |

### Achado a verificar antes de tudo
A permissão de leitura de projeto do Analista e do Supervisor não tem condição de status. Pelo código de `List` e `Get`, isso sugere que **a equipe enxerga rascunhos** de todos os proponentes (dados pessoais, sem terem sido enviados). Escrever primeiro um teste que mostre ou descarte isso (ver 4.4 e 9).

---

## 2. Decisões

### 2.1 Já tomadas (nesta conversa)

1. O Analista analisa tudo e escolhe o Departamento. O Revisor do Departamento só diz se o projeto se enquadra ou não.
2. O Supervisor Administrador só recebe e-mails; é somente leitura. "Supervisor Gabinete" é o mesmo papel (nome já trocado em `planejamento-cadastro-projeto.md`).
3. Recusa de enquadramento: e-mail ao Supervisor e o projeto volta ao Analista para nova indicação. **Sem limite de recusas.** Exige justificativa.
4. Vale a primeira decisão de qualquer Revisor do departamento.
5. Novo perfil **Secretário de Estado**: vê todos os projetos e pode negar a qualquer momento (arquiva na hora).
6. Todo arquivamento envia e-mail a Proponente, Revisor do Departamento, Analista e Supervisor Administrador.
7. Saídas que entram agora: prazo vencido, falta de documentos (Analista), desistência (Proponente), negação (Secretário).
8. Nomes do sistema nas etapas: "Aguardando análise de checklist", "Em análise", "Aguardando proponente". "Devolver para complementação".
9. Reenvio cria nova versão e volta direto para "Em análise". Itens já aprovados e não alterados continuam aprovados.
10. **D1:** depois de "Projeto enquadrado" a etapa passa a ser `awaiting_opinion` ("Aguardando parecer").
11. **D2:** o e-mail ao "Analista" vai ao analista responsável (quem assumiu a análise); sem responsável, a todos os Analistas ativos. **Atualização:** só o responsável continua a análise; outro Analista visualiza e precisa assumir o projeto (`take-over`). Ver [planejamento-responsavel-pela-analise.md](planejamento-responsavel-pela-analise.md).
12. **D3:** a indicação é bloqueada se o departamento não tiver nenhum Revisor ativo.
13. **D4:** o departamento ganha o marcador `is_finalistic`; só departamento finalístico pode ser indicado.
14. **D5:** o papel "Finalística" do planejamento antigo é o "Revisor do Departamento" (já renomeado em `planejamento-cadastro-projeto.md`).
15. **Leitura de departamentos:** o Analista recebe a permissão de ler departamentos (não haverá rota separada) para escolher o departamento.
16. **D6:** e-mails de projeto saem por caixa de saída com tentativas **e de forma assíncrona** (a requisição só grava; um processo em segundo plano envia). Ver 8.4.
17. **D7:** as permissões de `document.*` saem do Revisor do Departamento.
18. **D9:** o e-mail ao Supervisor leva só uma lista permitida de campos; CPF mascarado e **toda informação pessoal ou sensível fica de fora**. Ver 8.3.

### 2.2 Decisões com recomendação adotada

Todas as decisões, de D1 a D11, estão **confirmadas** (✔).

| # | Decisão | Recomendação | Alternativa |
|---|---|---|---|
| ✔ D1 | **Etapa depois de "Projeto enquadrado"** | Nova etapa `awaiting_opinion` ("Aguardando parecer", já prevista no planejamento §5). Deixa o fim do diagrama visível para todos | Manter `in_analysis` com um indicador "enquadrado" |
| ✔ D2 | **Quem é "o Analista" nos e-mails** | `analyst_id` (analista responsável) gravado em "Assumir análise". E-mail vai ao responsável; se ainda não houver, a todos os Analistas ativos | Sempre a todos os Analistas |
| ✔ D3 | **Departamento sem nenhum Revisor ativo** | Bloquear a indicação (422 `department_without_reviewer`) para o projeto não ficar parado | Só avisar |
| ✔ D4 | **Todo departamento é "Área Finalística"?** | Não: criar o marcador `is_finalistic` em `departments` (padrão `false`), editável na tela de departamentos. Só departamento marcado pode ser indicado. No seed de desenvolvimento, Financeiro e Jurídico saem marcados | Tratar todos como finalísticos |
| ✔ D5 | Papel "Finalística" em `planejamento-cadastro-projeto.md` §3.2/§9 | É o mesmo que "Revisor do Departamento"; renomeado | Manter os dois nomes |
| ✔ D6 | Envio de e-mails | **Caixa de saída** (outbox) transacional, com tentativas, sem duplicar e **assíncrona**: a requisição só grava a mensagem e um processo em segundo plano (goroutine) a envia. Ver 8.4 | Manter o envio atual |
| ✔ D7 | Permissões `document.*` do Revisor | Removidas do Revisor. Elas pertencem a um módulo genérico (o "Documentos" do template) e não ao fluxo de projetos. Os testes que usam o Revisor para demonstrar `same-department` em documentos precisam ser reescritos (seção 9) | Manter |
| ✔ D8 | Motivo `other` no arquivamento pelo Analista | O BPMN só prevê falta de documentos. Manter `other` na API, mas fora da interface nesta fase | Remover do backend |
| ✔ D9 | Conteúdo do e-mail regular ao Supervisor | Lista permitida de campos (8.3): identificação do projeto, da instituição e dos valores. CPF mascarado e nenhum outro dado pessoal ou sensível. Link para o sistema | Texto integral |
| ✔ D10 | Aprovação de documento exige que **o mesmo Analista** que aprova tenha aberto cada arquivo atual | Sim. Arquivo novo (após devolução) exige nova abertura | Qualquer Analista ter aberto |
| ✔ D11 | Analista arquiva ou devolve com enquadramento pendente | Bloquear (409 `framing_pending`). Só Proponente (desistência) e Secretário (negação) agem nesse momento | Permitir |

---

## 3. Modelo de estados

Três dimensões, como já era o desenho original (etapa, situação, parecer), mais o enquadramento:

- **Situação:** `draft` → `submitted` → `archived` (existe).
- **Etapa** (`stage`): `awaiting_checklist`, `in_analysis`, `awaiting_proponent` (existem) + **`awaiting_opinion`** (novo, D1).
- **Enquadramento** (`framing_status`, novo): vazio, `pending`, `confirmed`. A recusa não é um estado do projeto: encerra a rodada e limpa o departamento (ver 4.3).

### 3.1 Transições

| De → Para | Quem | Rota | Regras e efeitos |
|---|---|---|---|
| Rascunho → `awaiting_checklist` | Proponente | `POST /submit` (existe) | Código, versão 1, comprovante. E-mail ao Proponente |
| `awaiting_checklist` → `in_analysis` | Analista | `POST /start-analysis` (**novo**) | Grava `analyst_id`. Idempotente para o mesmo Analista; outro Analista recebe 409 `not_responsible_analyst` (ver [planejamento-responsavel-pela-analise.md](planejamento-responsavel-pela-analise.md)) |
| `in_analysis` / `awaiting_proponent`, com outro responsável | Analista | `POST /take-over` (**novo**) | Troca o `analyst_id`; só o responsável conferir, devolver, arquivar e indicar. Avisa o anterior por e-mail. Não vale com enquadramento pendente nem em `awaiting_opinion` |
| `in_analysis` → `awaiting_proponent` | Analista | `POST /complement-requests` (existe) | Itens devolvidos voltam a `pending` na conferência. Prazo de 3 dias. E-mail ao Proponente. Bloqueado com enquadramento pendente (D11) |
| `awaiting_proponent` → `in_analysis` | Proponente | `POST /resubmit` (existe) | Nova versão. Só itens devolvidos mudam; os demais conservam a aprovação |
| `awaiting_proponent` → `archived` | Sistema | job (existe) | Motivo `complement_deadline`. E-mail aos 4 perfis (**ajustar**) |
| `in_analysis` → `archived` | Analista | `POST /archive` (existe) | Motivo `missing_documents`, com texto. Bloqueado com enquadramento pendente (D11). E-mail aos 4 perfis |
| `in_analysis` + tudo aprovado → enquadramento `pending` | Analista | `PUT /department` (**novo**) | Todos os itens aprovados, sem rodada pendente, departamento com Revisor ativo (D3). Grava `department_id`. E-mail "regular" ao Supervisor |
| `pending` → `awaiting_opinion`, `confirmed` | Revisor do departamento | `POST /framing/confirm` (**novo**) | Primeira decisão vence; a segunda leva 409 |
| `pending` → `in_analysis` (rodada recusada) | Revisor do departamento | `POST /framing/refuse` (**novo**) | Justificativa obrigatória. Limpa `department_id`. E-mail "recusa" ao Supervisor. Sem limite de rodadas |
| Qualquer etapa ativa → `archived` | Proponente titular | `POST /withdraw` (existe) | Confirmação e motivo. **Passa a enviar e-mail** |
| Qualquer etapa ativa → `archived` | Secretário de Estado | `POST /deny` (existe) | Motivo `denied`, texto obrigatório. Funciona também com enquadramento pendente |

"Etapa ativa" = `submitted`. Rascunho não se arquiva (exclui-se) e arquivado é terminal; ambos já respondem 409 hoje.

---

## 4. Modelo de dados

Migrações novas, no padrão goose do projeto, com `Down` e idempotentes quando mexem em perfis. Depois de cada uma: `make sqlc` e commitar o código gerado.

### 4.1 `00024_secretary_and_staff_permissions.sql` (perfis e marcador de departamento)
- Cria o perfil `state-secretary` ("Secretário de Estado", sistema): `projects.read` (com a condição de 4.4), `projects.deny`, e as leituras de catálogo/regras que os outros perfis de equipe têm.
- Cria as ações novas e as atribui: ver matriz em 5.
- **Remove do Revisor** as permissões `admin::content.document.*` (D7) e concede ao **Analista** `admin::settings.departments.read`, `.update` e `.delete` (decisão do cliente; `create` fica de fora).
- `departments.is_finalistic boolean NOT NULL DEFAULT false` (D4). Os departamentos que já existem ficam `false` até alguém marcá-los; é um **passo de implantação** (ver seção 12). O `Down` remove a coluna.
- **Revoga do Supervisor** `request-complement`, `archive` e `deny`; atualiza as descrições dos perfis de sistema (Supervisor, Analista, Revisor).
- Teste de migração para cima e para baixo. Atenção: o `Down` do 00022 só apaga perfis sem uso; manter o mesmo cuidado.

### 4.2 Conferência e enquadramento, em duas migrações
**`00026_project_review.sql` (fase 3, detalhada em [planejamento-fase-3.md](planejamento-fase-3.md)):**
- `projects.analyst_id` (FK `admins`, nulo).
- `project_item_reviews`: `project_id`, `kind` (`section`/`document`/`extra`), `target` (mesma chave dos itens de complementação), `status` (`pending`/`approved`), `reviewed_by_id`, `reviewed_at`. Única por `(project_id, kind, target)`.
- `project_file_views`: `file_id`, `admin_id`, `opened_at`. Chave `(file_id, admin_id)`. Gravada no download feito pela equipe.

**`00027_project_framing.sql` (fase 4):**
- `projects.framing_status` (`pending`/`confirmed`, nulo) e ajuste do `CHECK` de `stage` para incluir `awaiting_opinion` (mesmo padrão do 00014: derrubar e recriar a constraint).
- `project_framings` (uma linha por rodada): `project_id`, `round`, `department_id`, `indicated_by_id`, `indicated_at`, `decision` (`pending`/`confirmed`/`refused`), `decided_by_id`, `decided_at`, `justification`. Índice único parcial: no máximo uma rodada `pending` por projeto.

### 4.3 Por que `department_id` é limpo na recusa
Se o departamento ficasse gravado, `same-department` continuaria dando acesso ao Revisor que acabou de recusar e o projeto apareceria como se ainda fosse dele. Limpando, o projeto some da fila do departamento e o histórico (departamento, quem recusou, justificativa) fica em `project_framings`. O Analista vê esse histórico ao indicar de novo.

### 4.4 Visibilidade da equipe
Duas condições novas, no estilo de `admin::project-member`, que restringem o status a `submitted`/`archived` na leitura de **todos** os perfis de equipe:

- `admin::project-submitted`: só o status. Para Analista, Supervisor e Secretário.
- `admin::project-submitted-same-department`: status **e** departamento do usuário. Para o Revisor.

**Por que duas e não uma condição de status somada a `same-department`:** no motor de permissões, várias condições na mesma permissão se combinam por **OU** (`permissionFilter` em `authz/engine.go`), e as permissões de perfis diferentes também se unem por OU. Listar `same-department` e uma condição de status juntas daria "mesmo departamento *ou* enviado", o oposto do desejado. A combinação E precisa estar dentro de uma única condição.

O campo `status` já faz parte do `Ref` do projeto no motor, então não exige mudança no motor. Rascunho continua visível só ao titular e aos membros (as permissões do Proponente se somam por OU, então quem tem os dois perfis ainda vê o próprio rascunho). Super Admin continua vendo tudo. Quem monta um perfil personalizado sem essas condições reabre a exposição; por isso o teste de matriz da fase 1 cobre cada rota e a documentação dos perfis avisa.

### 4.5 `00025_outbound_emails.sql` (D6) — **feita na fase 2**
`outbound_emails`: `kind`, `project_id`, `to_email`, `subject`, `body`, `dedup_key` (único: tipo + projeto + referência + destinatário), `status` (`pending`/`sent`/`failed`), `created_at`, `attempts`, `next_attempt_at`, `locked_until`, `sent_at`, `last_error`. Índice parcial em `(next_attempt_at) WHERE status = 'pending'`. Gravada na mesma transação da mudança de estado. O envio é descrito em 8.4.

---

## 5. Permissões e perfis

Ações novas (prefixo `admin::projects.`): `start-analysis`, `review-item`, `assign-department`, `review-framing`. As existentes continuam: `read`, `request-complement`, `archive`, `deny`, `withdraw`, `submit`.

| Perfil | Ações de projeto | Condição |
|---|---|---|
| Proponente | create, read, update, delete, submit, withdraw, share | `is-creator` / membro / editor (sem mudança) |
| **Analista** | read, **start-analysis**, **review-item**, request-complement, archive, **assign-department**; mais `admin::settings.departments.read`, `.update` e `.delete` | projetos: condição `admin::project-submitted` (4.4) |
| **Revisor do Departamento** | read, **review-framing** (sem `document.*`, D7) | `admin::project-submitted-same-department` (4.4). Sem departamento no cadastro do usuário, nega tudo |
| **Supervisor Administrador** | read | `admin::project-submitted`. Perde request-complement, archive e deny |
| **Secretário de Estado** (novo) | read, deny | `admin::project-submitted` |

Pontos de atenção:
- `admin::same-department` compara com o departamento do **usuário**. Revisor criado sem departamento enxerga e faz nada, sem erro. A tela de usuários deve avisar quando o perfil Revisor for atribuído a alguém sem departamento.
- Super Admin continua passando por tudo.
- O seed (`seed.go`) e o teste de contagem (`document_test.go`: hoje 7 perfis e 47 permissões) precisam de novos números.
- O Revisor recebe leituras de catálogo e de parâmetros de regra como os demais perfis de equipe.

---

## 6. API

Mesmo prefixo e estilo de hoje (`/api/admin/projects/{id}/...`), resposta `{data, meta}` e erro `{error:{status,code,message,details}}`. **Atualizar `backend/api/openapi.yaml` na mesma entrega**: existe teste que falha se uma rota registrada não estiver documentada.

| Rota | Quem | Corpo | Resultado |
|---|---|---|---|
| `POST /start-analysis` | Analista | — | Projeto com `stage=in_analysis` e `analyst` |
| `PUT /review/items` | Analista | `{kind, target, approved}` | Aprova ou desfaz a aprovação de um item. Aprovar documento exige que o próprio Analista tenha aberto cada arquivo atual (D10). 422 `file_not_opened` com a lista de arquivos pendentes |
| `PUT /department` | Analista | `{departmentId, version}` | Abre a rodada de enquadramento. Erros: 409 `review_incomplete` (itens pendentes), 409 `framing_pending`, 422 `department_not_finalistic`, 422 `department_without_reviewer`, 409 `project_stage_invalid` |
| `GET /admin/departments` e `PUT /admin/departments/{id}` (existentes) | Quem administra departamentos | `isFinalistic` | O campo é lido e gravado. O Analista lista, edita e exclui departamentos com as permissões `departments.read`, `.update` e `.delete` que passa a ter; não cria. Filtro novo `finalistic=true` para o campo de escolha. A lista continua trazendo `userCount` e `documentCount` (só números agregados) |
| `POST /framing/confirm` | Revisor | `{round}` | `awaiting_opinion`. 409 `framing_already_decided` se outro Revisor já decidiu |
| `POST /framing/refuse` | Revisor | `{round, justification}` | Rodada recusada, volta ao Analista. Mesmo 409 |
| `GET /admin/projects` (existente) | todos | filtros novos: `stage`, `departmentId`, `analystId`, `framing` | Filas de trabalho. `Summary` ganha `stage`, `department`, `analyst`, `framingStatus` |
| `GET /admin/projects/{id}` (existente) | equipe | — | A visão ganha `review` (itens, aprovados, arquivos abertos/total, `allApproved`), `framing` (rodada atual) e `framings` (histórico, com justificativas) |

Pontos de contrato:
- O `round` no corpo de confirmar/recusar evita agir sobre uma rodada antiga (Revisor com a tela aberta durante uma recusa e nova indicação).
- Download de arquivo pela equipe registra a abertura em `project_file_views` além da auditoria que já existe.
- O CPF segue mascarado para a equipe (já implementado).
- Erros com `code` estável em inglês e `message` em português, como em todo o projeto.

---

## 7. Regras de negócio, concorrência e auditoria

**Itens conferidos.** Os itens são as 5 seções devolvíveis (Proponente, Instituição, Proposta, Recursos, Situações dos documentos) e os itens do checklist. Um item de checklist é conferível se tem arquivo, justificativa aceita ou declaração; opcional vazio é ignorado. Obrigatório ausente sem justificativa **impede** a aprovação (o Analista devolve ou arquiva).

**Devolução e reenvio.** Ao devolver, os itens devolvidos voltam a `pending`; os demais continuam aprovados. Isso é seguro porque a regra de acesso já impede o Proponente de alterar o que não foi devolvido. No reenvio, arquivos novos têm outro id, então exigem nova abertura.

**Aprovar tudo ≠ indicar.** "Tudo aprovado" é calculado no servidor; a interface só mostra o campo de departamento quando for verdade, mas o servidor revalida.

**Concorrência.** Todas as transições travam a linha (`GetProjectForUpdate`, como em `RequestComplement`). Confirmar/recusar usa também a rodada: duas decisões simultâneas resultam em uma aplicada e uma 409. Negação/desistência concorrentes com uma decisão do Revisor: quem travar primeiro vence; o outro vê 409 `project_archived`.

**Auditoria.** Uma entrada por ação: `project.start-analysis`, `project.review-item`, `project.assign-department`, `project.framing-confirm`, `project.framing-refuse`, mais as existentes (`archive`, `complement-request`, `resubmit`). Justificativas ficam no registro de negócio, não no payload de auditoria, para não duplicar texto livre.

**Texto livre.** Mesma higiene dos campos atuais: normalização (`multiline`), limite de tamanho, mínimo de 3 caracteres para justificativas e motivos.

---

## 8. Notificações

### 8.1 Catálogo

| Evento | Destinatários | Conteúdo |
|---|---|---|
| Projeto recebido | Proponente | Existe |
| Complementação pedida | Proponente | Existe: itens, observações e prazo |
| Correções recebidas | Proponente | Existe |
| **Documentação regular e encaminhada** | Todos os Supervisores ativos | Dados do projeto sem documentos (D9), departamento indicado, link |
| **Enquadramento recusado** | Todos os Supervisores ativos | Departamento recusado, quem recusou, justificativa, rodada, link |
| **Projeto arquivado** (qualquer motivo, inclusive desistência) | Proponente; Revisores ativos do departamento (só se houver departamento); Analista (D2); Supervisores ativos | Motivo, texto registrado, quem arquivou, quando, link |

### 8.2 Regras de envio
- Destinatário = administrador ativo e não bloqueado (`is_active` e `blocked` em `admins`). Quem tem mais de um perfil recebe **um** e-mail por evento.
- O Proponente continua recebendo no e-mail principal do cadastro do projeto; não vira administrador no meio do caminho.
- Textos diferentes por público no arquivamento (o Proponente recebe a orientação de "Duplicar projeto", como hoje; a equipe recebe o resumo).
- Com a caixa de saída (D6), o evento e o e-mail são gravados juntos, o envio acontece fora da requisição (8.4) e falha de SMTP não perde mensagem nem derruba a ação.
- A caixa de saída vale para **todos os e-mails de projeto**, inclusive os que existem hoje (recebido, complementação, correções, convite de compartilhamento). E-mails de autenticação de usuário final ficam como estão.
- Sem `SMTP_HOST`, o comportamento atual se mantém (log em dev, descarte com aviso em produção).

### 8.3 LGPD
O e-mail ao Supervisor é montado por **lista permitida** (só entra o que está na lista; campo novo no cadastro nunca vaza por padrão). A mensagem completa fica no sistema, atrás de login, e o e-mail traz o link.

| Entra | Fica de fora |
|---|---|
| Código, instrumento, título, objeto, nº SIGCon, vigência | CPF do proponente e do representante legal (nem completo, nem mascarado; ver nota) |
| Tipo de recurso, valor total, emenda e contrapartida | Telefones, e-mails de contato e endereços (do proponente e do representante legal) |
| Municípios de execução | RG, estado civil, nacionalidade e endereço residencial de quem assina |
| Instituição: nome, CNPJ, tipo, natureza, município/UF da sede | Justificativa e demais textos livres longos (podem conter dados pessoais digitados pelo Proponente) |
| Proponente: nome e cargo | Documentos e anexos de qualquer tipo |
| Departamento indicado e link para o projeto | Declarações e comprovante em PDF |

**Nota:** a regra confirmada é "dados sensíveis ou pessoais ficam de fora". A lista acima é mais restritiva que a recomendação inicial (CPF mascarado). Se o Supervisor precisar ver o CPF mascarado para identificar o proponente, é só incluí-lo na lista. Os e-mails de recusa e de arquivamento seguem a mesma lista permitida. O teste desta seção procura no corpo qualquer sequência que pareça CPF, telefone ou e-mail pessoal.

### 8.4 Envio assíncrono (D6) — implementado na fase 2
Em Go, o equivalente à thread separada é uma **goroutine**, e o projeto já usa o padrão: `expireComplements` é iniciada em `app.go` com `go` e roda até o contexto terminar. O envio de e-mail segue o mesmo desenho (pacote `internal/outbox`):

1. **A requisição só grava.** Na mesma transação que muda o estado do projeto, as linhas em `outbound_emails` são inseridas (`outbox.Enqueue` com as consultas da transação). Se a ação for recusada ou desfeita, o e-mail não existe. A resposta HTTP não espera SMTP.
2. **Um processo em segundo plano envia.** Uma goroutine (`Worker.Run`), iniciada em `App.Start`, varre a fila a cada 5 s (e logo de novo se o lote veio cheio). Até 4 e-mails saem ao mesmo tempo, para um SMTP lento não segurar os demais. **Não há aviso imediato ao gravar**: o intervalo de 5 s é a latência máxima, o que dispensou ligar cada ponto de gravação ao worker.
3. **Reserva por concessão, sem transação aberta durante o SMTP.** Uma única instrução reserva um lote com `FOR UPDATE SKIP LOCKED`, conta a tentativa e marca `locked_until` (2 min); o envio acontece fora de qualquer transação; depois outra instrução marca `sent` ou agenda a nova tentativa. Se a instância cair no meio, a concessão vence e outra instância pega a mensagem. Várias instâncias podem rodar o processo ao mesmo tempo (testado com duas).
4. **Tentativas com recuo.** 1 min, 5 min, 30 min, 2 h e 12 h (o último repete), até 8 tentativas; depois a mensagem fica `failed`, com `last_error`, e aparece no log como erro. Reservas perdidas (queda da instância) contam como tentativa, e uma mensagem que as esgota é desistida em vez de insistir para sempre.
5. **Garantia.** Entrega *pelo menos uma vez*: numa queda entre enviar e marcar, uma mensagem pode sair duas vezes. A `dedup_key` (único) impede duplicar a *criação* do mesmo e-mail para o mesmo destinatário, não esse caso raro. Convites de compartilhamento não têm chave: convidar de novo manda outro aviso.
6. **Encerramento.** Ao receber o fim do contexto, o processo termina o envio em andamento e **grava o resultado** (com contexto separado), para a mensagem não voltar à fila e sair duas vezes; não reserva mais nada.
7. **Privacidade.** Nunca se registra o corpo no log. Como o corpo tem dados pessoais, `cleanup` apaga as mensagens `sent` e `failed` com mais de N dias (a mesma carência das sessões e tokens). Pendentes nunca são apagadas.
8. **Testes determinísticos.** O ambiente de teste não liga o worker (`app.WithoutOutboxWorker`) e esvazia a fila depois de cada requisição (`env.drainOutbox`); os testes que verificam o assincronismo desligam isso (`keepOutbox`) e usam `CaptureMailer.FailTimes` para simular SMTP fora do ar.

Sem `SMTP_HOST` o comportamento anterior se mantém: o `LogMailer` (desenvolvimento) ou o `NopMailer` (produção) recebe a mensagem do worker, que a marca como enviada.

Alternativa descartada: apenas `go enviar()` dentro do handler. É simples, mas perde a mensagem se o processo cair, não tem tentativas e não impede duplicidade.

---

## 9. Testes

Integração em `backend/internal/app` (testcontainers), no padrão dos arquivos `project_*_test.go`. Criar helpers `p.secretary()`, `p.supervisor()` e `p.reviewer(dept)` ao lado de `p.analyst()`.

**Matriz de acesso (por perfil × rota):** Proponente alheio, Analista, Revisor do departamento certo, Revisor de outro departamento, Revisor sem departamento, Supervisor, Secretário. Supervisor recebe 403 em complementar/arquivar/negar. Secretário recebe 403 em tudo que não seja ler e negar.

**Visibilidade:** equipe não lista nem abre rascunho (teste do achado da seção 1); Revisor só vê o que é do seu departamento e some da fila após recusa.

**Fluxo feliz:** envio → assumir → aprovar todos → indicar → confirmar → `awaiting_opinion`.

**Conferência:** aprovar documento sem abrir (422); abrir e aprovar; arquivo substituído exige nova abertura; devolver zera só os itens devolvidos; reenvio conserva os demais aprovados; obrigatório ausente impede aprovação.

**Enquadramento:** indicar sem tudo aprovado; indicar para departamento não finalístico; indicar para departamento sem Revisor; duas indicações; recusa seguida de nova indicação (repetir 5 vezes, sem limite); histórico completo; dois Revisores confirmando e recusando ao mesmo tempo (um vence); `round` antigo recusado.

**Saídas:** Secretário nega em cada etapa (`awaiting_checklist`, `in_analysis`, `awaiting_proponent`, enquadramento pendente, `awaiting_opinion`); em rascunho e em arquivado, 409. Desistência em cada etapa. Analista arquiva com enquadramento pendente (409). Job de prazo vencido arquiva e notifica.

**E-mails:** com `CaptureMailer` e `DrainOutbox`, verificar destinatários por evento, deduplicação quando a mesma pessoa tem dois perfis, ausência de Revisor quando não há departamento, desistência passando a notificar, corpo do e-mail ao Supervisor sem CPF, telefone ou e-mail pessoal (8.3), falha de SMTP não derrubando a ação nem perdendo a mensagem, tentativas com recuo até `failed`, concessão expirada reprocessada por outra instância, encerramento limpo e a requisição respondendo sem esperar o SMTP.

**D7 e testes que quebram:** `document_test.go` (permissões do seed e `TestRevisorDoDepartamento`) e `document_files_test.go` usam `revisor@dev.local` para demonstrar a condição `same-department` sobre documentos. Sem `document.*` no Revisor, esses testes precisam ser reescritos com um perfil de teste que concede essas ações ao departamento, para a condição continuar coberta. O Revisor passa a ser testado nas rotas de projeto.

**Departamentos:** Analista lista, edita e exclui departamentos e não cria; filtro `finalistic=true`; Proponente e Revisor continuam sem acesso à lista.

**Outros:** cobertura de `internal/authz` continua ≥ 80% (`make cover-authz`); migração sobe e desce; contagem de perfis/permissões do seed; cobertura do OpenAPI; testes atuais que usam o Supervisor para negar ou arquivar passam a usar o Secretário.

**Frontend:** não há suíte automatizada; a garantia é `typecheck`, `lint`, `format:check` e um roteiro manual (seção 11) executado em `docker compose` com usuários de desenvolvimento.

---

## 10. Frontend

Seguindo as convenções existentes: feature `features/projects` com `api/types.ts` → `service.ts` → `queries.ts` (React Query, prefetch no servidor), filtros com nuqs, formulários com `useAppForm` + Zod, ícones só de `@/components/icons`, permissões com `useCan()`/`<Can>`.

| Tela / componente | Quem | Conteúdo |
|---|---|---|
| Listas por perfil em `/dashboard/projetos` | Todos | Filtros `stage`/`framing`/`departmentId` e colunas conforme o perfil. Analista: fila "Aguardando análise de checklist" e "Em análise". Revisor: enquadramentos pendentes. Secretário e Supervisor: todos |
| Detalhe do projeto em modo equipe | Equipe | Duas visualizações de leitura (não são o formulário do cadastro): **Resumo** enxuto para o Revisor e **Projeto completo** para os demais perfis, com o completo a uma aba de distância do Revisor. Detalhes em `planejamento-visualizacao-projeto.md` |
| Painel de conferência | Analista | Aprovar por seção e por documento, indicador "abrir o documento para aprovar", progresso |
| Barra de decisão | Analista | Devolver para complementação, arquivar por falta de documentos, e o campo **Departamento** que só aparece com tudo aprovado |
| Painel de enquadramento | Revisor | Confirmar ou recusar (justificativa obrigatória); mostra o histórico de rodadas |
| Ação de negar | Secretário | Diálogo com motivo obrigatório e aviso de que arquiva na hora |
| Rótulos de etapa | Proponente | `awaiting_opinion` aparece como "Aguardando parecer"; faixa de arquivamento com o motivo |
| Administração de usuários | Admin | Aviso ao dar o perfil Revisor a quem não tem departamento |
| Administração de departamentos | Admin | Opção "Área finalística" (marcador `is_finalistic`) na criação e na edição; a lista mostra quais são finalísticos |
| Navegação (`nav-config.ts`) | Todos | Item de menu com a permissão adequada por perfil |

Espelhos de regra do servidor (por exemplo, "só mostrar o departamento com tudo aprovado") são conveniência; **o servidor é a autoridade**, como já é com tipos e tamanhos de arquivo.

O componente de kanban existente (`components/ui/kanban.tsx`) fica como evolução opcional da fila do Analista; não é pré-requisito.

---

## 11. Fases de entrega

Cada fase termina com `make test`, `make lint`, `make cover-authz`, `make sqlc` (quando houver SQL), `bun run typecheck && bun run lint && bun run format:check` (quando houver frontend) e atualização do OpenAPI.

| Fase | Conteúdo | Pronto quando | Depende de |
|---|---|---|---|
| **0 — Preparação** | Teste que prova ou descarta a exposição de rascunhos (D1–D5 já confirmadas); atualizar `planejamento-cadastro-projeto.md` (§3.2, §5, §9, §11); registrar o BPMN como fonte da verdade | Decisões fechadas e teste de rascunho escrito | — |
| **1 — Perfis e visibilidade** | Migração 00024, condição de status, perfil Secretário, Supervisor somente leitura, marcador `is_finalistic` (backend e tela de departamentos), seed e usuários de dev (Analista, Supervisor, Secretário, 2 Revisores) | Matriz de acesso passando; equipe não vê rascunho | 0 |
| **2 — Notificações** | Caixa de saída assíncrona (**feita**: migração 00025, worker, `DrainOutbox`, migração dos e-mails de projeto existentes), resolvedor de destinatários por perfil/departamento, e-mail de arquivamento a 4 perfis (inclui desistência), textos | Qualquer arquivamento notifica corretamente; Secretário já nega com e-mail | 1 |
| **3 — Conferência** | Migração 00026 (parte de itens/visualizações/`analyst_id`), `start-analysis`, `review/items`, registro de abertura no download, regras de devolução e reenvio | Analista aprova item a item e o reenvio conserva aprovações | 1 |
| **4 — Enquadramento** | Rodadas, `department`, `framing/confirm|refuse`, etapa `awaiting_opinion`, filtros de fila, e-mails ao Supervisor, bloqueios D11 | Fluxo do BPMN completo por API, com concorrência testada | 2, 3 |
| **5 — Telas da equipe** | Seção 10, a partir do contrato do OpenAPI. Pode começar na fase 3 com dados simulados | Roteiro manual completo executado | 3 (parcial), 4 |
| **6 — Endurecimento** | Matriz de acesso por rota, concorrência, revisão LGPD dos e-mails, documentação (`backend/README.md`, `README.md`), roteiro E2E | Roteiro manual de aceite (abaixo) executado | 5 |

Ordem sugerida: 0 → 1 → (2 e 3 em paralelo) → 4 → 5 → 6. A fase 2 sozinha já entrega valor: o Secretário nega e todos são avisados.

**Roteiro manual de aceite (fase 6):** (1) Proponente envia; (2) Analista assume, abre e aprova tudo e devolve um item; (3) Proponente complementa e reenvia; (4) Analista aprova, indica Financeiro; (5) Revisor de Jurídico não vê o projeto; (6) Revisor de Financeiro recusa com justificativa; (7) Supervisor recebe os dois e-mails; (8) Analista indica Jurídico; (9) Revisor de Jurídico confirma e a etapa vira "Aguardando parecer"; (10) repetir negando como Secretário em cada etapa e conferindo os quatro e-mails.

---

## 12. Riscos e cuidados

| Risco | Cuidado |
|---|---|
| Revogar ações do Supervisor em migração pode sobrescrever personalização feita por administradores | Remover só as 3 ações padrão, registrar no changelog e comunicar antes de aplicar |
| Revisor sem departamento fica sem acesso a qualquer projeto (403) | Aviso na administração de usuários e teste explícito |
| Condição de status mal aplicada reabre a exposição de rascunhos; condições na mesma permissão se combinam por OU, não por E | Condições compostas (4.4) e teste de matriz por rota com os perfis **semeados** (os testes atuais usam perfis ad hoc sem condições e não detectariam o problema) |
| E-mail para a equipe com dados pessoais | D9: lista permitida, sem anexos, teste que procura dados pessoais no corpo |
| Envio de e-mail dentro da requisição falha, atrasa a resposta ou duplica | Caixa de saída assíncrona com tentativas e chave de deduplicação (D6, 8.4) |
| Mover os e-mails existentes para a caixa de saída muda o momento do envio e pode quebrar testes que leem o e-mail logo após a requisição | `DrainOutbox` nos testes; migrar um tipo de e-mail por vez |
| Tirar `document.*` do Revisor quebra testes do módulo genérico de documentos | Reescrever com perfil de teste (seção 9) na fase 1 |
| Estado inconsistente se uma decisão e uma negação concorrem | Trava de linha + validação da rodada, com teste de concorrência |
| `project_versions` guarda foto do projeto; conferência é por projeto, não por versão | Documentado: a conferência vale para a versão atual; o histórico de decisões fica nas tabelas e na auditoria |
| Projeto parado por departamento sem Revisor | D3 (bloqueio na indicação) |
| Excluir um departamento (que o Analista agora pode fazer) esvazia em silêncio o `department_id` dos projetos que o referenciam | Na fase 4, a regra `department_in_use` passa a contar também projetos e rodadas de enquadramento |
| Em produção, nenhum departamento sairia marcado como finalístico e o Analista não conseguiria indicar | Passo de implantação: marcar os departamentos finalísticos reais logo após a migração 00024, antes de liberar o fluxo |
| Renomeações (estágio novo, papéis) quebrarem testes e o frontend | Rótulos de etapa centralizados no frontend e checagem por `typecheck` |

---

## 13. Atualizações de documentação ao final

- `docs/planejamento-cadastro-projeto.md`: §3.2 e §9 (papéis; "Finalística" → "Revisor do Departamento", já feito), §5 (nova etapa e enquadramento), §11 (fases), §12 (premissas).
- `backend/README.md` e `README.md`: perfis, ações e usuários de desenvolvimento.
- `backend/api/openapi.yaml`: rotas e schemas novos.
- BPMN: se qualquer decisão em aberto mudar o fluxo, atualizar o diagrama **antes** do código.
