# Planejamento — Declarações assinadas que "não são reconhecidas" e endereço "não encontrado"

Relato: (1) as declarações geradas e assinadas pelo sistema não aparecem como entregues; (2) ao abrir o diálogo de uma declaração, o sistema não reconhece o endereço do proponente, embora o novo projeto já tenha vindo preenchido.

Este documento diz onde está o problema e o que mudar. **Nenhum código foi alterado.** A investigação usou só leitura: o banco de desenvolvimento, o log da API e um teste temporário (já removido) que recalculou os resumos das declarações.

---

## 1. O que a investigação mostrou

Projeto de teste `39848690…` (rascunho criado às 20:44 de 08/11). Linha do tempo do log da API:

| Hora | Fato |
|---|---|
| 20:44:54 | Proponente salvo com os dados vindos do perfil |
| 20:53:58 e 20:54:01 | Aceite e assinatura de *compatibilidade de preços* e *não duplicidade* (cargo "Presidente", vindo do perfil) |
| 20:54:22 | Abre o diálogo de gratuidade, publicidade e contrapartida |
| 20:54:44 → 20:55:35 | Quatro salvamentos seguidos da seção Proponente (o cargo vira "Prefeito"; o número do endereço é preenchido) |
| 20:55:37 | Reabre as três declarações e emite todas |
| 20:55:46 | Reemite as duas já assinadas, que ficaram **superseded** (o texto impresso mudou: o cargo) |
| 20:56:43 | Primeira vez que alguém consulta as cinco declarações depois de emitidas |

Estado final no banco: as cinco estão `signed`, com arquivo confirmado, e o resumo (`params_sha256`) de cada uma **é igual** ao recalculado hoje. Ou seja, o backend reconhece as declarações assinadas; o que falha é a tela e o momento em que o backend conclui a assinatura.

## 2. Causas

### C1. A assinatura só é concluída quando alguém abre a tela da declaração (backend + frontend)
`refreshDeclaration` (consulta o MT-ID, baixa o PDF assinado e o põe no checklist) só roda em `GET /declarations/{code}`. Nenhuma outra leitura faz isso: nem `GET /documents` (checklist), nem `GET /projects/{id}`, nem o **envio** do projeto.
- No log, entre 20:55:46 e 20:56:43 não há nenhuma consulta às declarações: o painel não consultava (a lista abre com "só o que falta", que esconde as declarações aguardando assinatura, e o painel escondido não faz a consulta de 5 em 5 segundos).
- O envio (`Submit`) calcula o checklist sem concluir assinaturas pendentes: quem assina e envia logo em seguida pode ser barrado por "documento faltando" mesmo com a declaração assinada.

### C2. A tela não atualiza o checklist quando a declaração muda de situação (frontend)
`DeclarationPanel` só invalida o checklist e o projeto quando o **aceite** é feito ou o pedido é cancelado. Quando a consulta de 5 s vê `awaiting_signature → signed`, o painel mostra "Assinada em…", mas o checklist, o resumo de documentos e a revisão ficam com o item pendente até o cache de 60 s vencer (`staleTime` global). No log, às 20:56:43 as cinco declarações foram consultadas e **não houve** nova leitura do checklist nem do projeto depois disso.

### C3. Salvar o cadastro não invalida as declarações em cache (frontend)
Os passos Proponente, Instituição, Proposta e Recursos invalidam `checklist` e `lists`, mas não `declaration`. A chave da declaração é outra (`['projects','declaration',id,code]`) e o cache dura 60 s. Resultado: a pessoa corrige o cadastro, volta aos Documentos e o diálogo ainda mostra as **pendências e o texto antigos** (20:54:22 foi lido e só foi recarregado às 20:55:37, mais de 60 s depois). É isso que dá a impressão de "endereço não reconhecido". Se aceitar com o texto velho, a API responde `declaration_changed` (409), porque o resumo enviado já não confere.

### C4. A pendência do endereço é vaga e o perfil deixa o número vazio (backend + frontend)
O perfil do proponente (`proponent_profiles.personal`) desse usuário tem rua, bairro, município e CEP, mas **`number` vazio**. O projeto novo copia o perfil, então "veio tudo preenchido" menos o número. `personalIssues` exige rua, número, bairro e município e, se faltar qualquer um, devolve uma única mensagem: *"Informe o endereço residencial (rua, número, bairro e município) … em Proponente"*. A pessoa vê rua, bairro e cidade preenchidos e conclui que o sistema "não reconheceu o endereço". Na tela o campo *Número* não indica que é necessário para as declarações.

### C5. Mudança de cargo invalida as declarações assinadas sem dizer o quê mudou (backend + frontend)
Por desenho, o cargo de quem assina está impresso no PDF, então mudá-lo invalida a assinatura (aqui, "Presidente" → "Prefeito" tornou as duas primeiras obsoletas; confirmado em `project_declarations.signer_role`). Está correto, mas a mensagem *"O cadastro mudou depois da assinatura…"* não diz **o que** mudou, e parece que o sistema "perdeu" a declaração.

## 3. O que alterar

### Backend
| # | Mudança | Onde |
|---|---|---|
| B1 | **Concluir assinaturas pendentes ao ler.** Nova função `settleDeclarations(ctx, id)`: busca as declarações `awaiting_signature` do projeto e chama `refreshDeclaration` em cada (falha do MT-ID é ignorada, como hoje). Chamá-la, antes de abrir qualquer transação, em `Get`, `Checklist`, `Submit` e `Resubmit`. É idempotente e segura em concorrência (`errRaceLost`). Consulta nova: `ListAwaitingDeclarations` (sqlc) | `project/declaration_service.go`, `project/service.go`, `project/documents.go`, `project/submit.go`, `project/lifecycle.go`, `database/queries/project_declarations.sql` |
| B2 | **Pendências de endereço específicas.** Em vez de uma mensagem só, uma pendência por parte que falta (rua, número, bairro, município), cada uma com campo próprio (`personal.number`…) e texto como *"Informe o número do endereço residencial do representante legal (use S/N se não houver)"* | `project/personal.go` (`personalIssues`) |
| B3 | **Dizer o que mudou.** `DeclarationRecord` ganha `staleReasons`, calculado comparando o registro com o texto atual: `signerRole`, `signerName`, `signerCpf`, `text` (parágrafos) e, se nada disso difere, `place` (local/instituição). Sem expor CPF. Opcional: o cargo anterior e o atual | `project/declaration_service.go` (`recordView`), OpenAPI |
| B4 | Atualizar `backend/api/openapi.yaml` (campos novos; leitura do checklist e do projeto passa a concluir assinaturas) | `backend/api/openapi.yaml` |

**Não vou mexer agora:** o `HomePlace` dentro do resumo (`ParamsSHA256`). Ele é redundante (o endereço já está no texto) e faz a troca do município residencial invalidar também as declarações que não o usam, mas retirá-lo mudaria o resumo de **todas** as declarações já assinadas e as marcaria como obsoletas. Fica como débito técnico, com a migração por "aceitar o resumo antigo ou o novo".

### Frontend (`features/projects`)
| # | Mudança | Onde |
|---|---|---|
| F1 | **Atualizar o checklist quando a declaração muda de situação.** O painel guarda a situação anterior e, ao ver `awaiting_signature → signed/rejected/expired` (ou o contrário), invalida `checklist`, `detail` e `lists` | `components/declaration-panel.tsx` |
| F2 | **Salvar o cadastro invalida as declarações** do projeto. Um único helper `invalidateAfterSave(qc, id)` (checklist + declaration + lists) usado por Proponente, Instituição, Proposta, Recursos e situações dos documentos | `components/*-step.tsx`, `api/queries.ts` |
| F3 | **Declaração sempre fresca.** `declarationQueryOptions` com `staleTime: 0` e `refetchOnMount: 'always'`; o botão de confirmar fica desabilitado enquanto a prévia recarrega (`isFetching`), e o diálogo recarrega ao abrir | `api/queries.ts`, `declaration-panel.tsx` |
| F4 | **Acompanhar a assinatura sem depender do painel.** O checklist faz `refetchInterval` de 5 s enquanto houver item com `declarationAwaiting` (com B1, a própria leitura conclui a assinatura). Vale também para a revisão do projeto | `api/queries.ts` (`checklistQueryOptions`), `review-step.tsx` |
| F5 | **Mensagem do que mudou** (usa `staleReasons`): "O cargo de quem assina mudou depois da assinatura. Gere e assine de novo." | `declaration-panel.tsx`, `checklist-item.tsx` |
| F6 | **Número do endereço visível como necessário** em `PersonalDataEditor`: rótulo "Número (use S/N se não houver)" e a nota que já existe passa a dizer que rua, número, bairro e município são necessários para gratuidade e publicidade. No perfil e na seção Proponente. Sem bloquear o salvamento | `components/personal-data-editor.tsx` |
| F7 | No diálogo, cada pendência mostra um botão **"Corrigir em Proponente"** que leva à etapa certa | `declaration-panel.tsx` |

## 4. Testes

**Backend (integração, `internal/app`, com o MT-ID simulado):**
1. Assinatura concluída pela leitura do checklist (`GET /documents`) e do projeto, sem ninguém abrir a declaração.
2. `Submit` e `Resubmit` concluem a assinatura pendente e o envio passa.
3. Falha do MT-ID na leitura não derruba a leitura (o pedido segue pendente).
4. Duas leituras simultâneas concluem uma única vez (um arquivo no checklist).
5. Pendências de endereço: número vazio devolve só a pendência do número, com o campo `personal.number`.
6. `staleReasons`: mudar o cargo, mudar o nome, mudar o texto; nada exposto além do necessário.
7. Ajustar `project_declarations_test.go` e `declarations_test.go` ao novo formato de pendência.

**Frontend** (sem suíte): `typecheck`, `lint`, `format:check` na cópia do scratchpad e o roteiro manual abaixo.

**Roteiro manual:**
1. Criar projeto com perfil sem número. Abrir gratuidade: o diálogo mostra só "Informe o número do endereço…" com atalho para Proponente.
2. Preencher o número, voltar: o diálogo já traz o texto novo (sem esperar 60 s) e permite emitir.
3. Emitir as cinco com a lista em "só o que falta": o checklist passa para "entregue" por si só em poucos segundos, sem abrir cada painel.
4. Assinar e **enviar logo em seguida**: o envio não reclama de declaração faltando.
5. Mudar o cargo depois de assinar: a declaração aparece como obsoleta dizendo que o **cargo** mudou.

## 5. Fases

| Fase | Entrega |
|---|---|
| 1 | Backend: B1 (concluir ao ler, `Get`/`Checklist`/`Submit`/`Resubmit`) e B2 (pendências de endereço), com testes. **Feita.** Junto: máscara de telefone (`(99) 9 9999-9999` ou `(99) 9999-9999`, conforme os dígitos) e de CEP (`99999-999`) nos campos do cadastro (`lib/masks.ts`, `PhoneField`, `ZipField`) |
| 2 | Backend: B3 (`staleReasons` no pedido e `declarationStaleReasons` no item do checklist) e B4 (OpenAPI), com testes. **Feita** |
| 3 | Frontend: F1 a F4 (cache e acompanhamento). **Feita**: `invalidateAfterSave`/`invalidateAfterSignature`/`useSignatureWatcher` em `api/queries.ts`; declaração com `staleTime: 0`; checklist consulta de 5 em 5 s com assinatura pendente; diálogo recarrega ao abrir e bloqueia a confirmação enquanto recarrega |
| 4 | Frontend: F5 a F7 (mensagens e atalhos). **Feita**: `declaration-hints.ts` (mensagem do que mudou e etapa de cada pendência), botão "Corrigir em …" no diálogo (via `onGoStep` do assistente, que respeita a confirmação de alterações não salvas), campos do endereço pessoal marcados com * e nota |
| 5 | Documentação (`planejamento-declaracoes.md`, `debitos-tecnicos.md` T35), checagens e teste manual. Documentação e checagens **feitas**; falta o teste manual nas telas |

Sem commit até tudo estar pronto e testado por você nas telas.

## 6. Pontos em aberto
- **B1 em `Get` e `Checklist`:** qualquer leitura pode chamar o MT-ID (um pedido por declaração pendente, no máximo cinco). Aceitável? A alternativa é só o frontend acompanhar (F4), mas aí o envio (`Submit`) ainda precisaria de B1.
- **Número do endereço:** só destacar (F6), ou também exigir no salvamento da seção Proponente quando há endereço parcial? Recomendo só destacar, porque o perfil pode ser preenchido por quem não assina nada.
- **Município residencial no resumo:** deixar como débito técnico (recomendado) ou migrar agora?
