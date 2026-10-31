# Planejamento — Histórico de etapas do projeto

Complementa o [planejamento do fluxo de análise e enquadramento](planejamento-fluxo-analise-enquadramento.md). Aquele documento definiu as etapas e as transições; este define como **registrar por onde o projeto passou, por quanto tempo e com quem ele estava**. Não contém código; contém decisões, modelo, contratos, ordem de entrega e testes.

**Escopo:** a vida do projeto depois do envio (`awaiting_checklist` até o arquivamento). Rascunho não tem histórico de etapas. Parecer técnico, formalização e prestação de contas ficam para outros documentos, mas o modelo abaixo já comporta novas etapas sem mudança de estrutura.

---

## 1. Ponto de partida: o que existe hoje

Conferido no código (nada foi executado):

| Pergunta | Hoje | Limite |
|---|---|---|
| O que foi feito, por quem, quando? | `audit_logs`: uma linha por ação, com ator, IP, ação, entidade e `payload` (`audit/audit.go`). Gravada na mesma transação da mudança | O `payload` traz o resumo da ação, não o antes e o depois. A etapa só consta no `start-analysis` |
| Em que etapa o projeto está? | `projects.stage`, `framing_status`, `department_id`, `analyst_id` | Guardam só o valor de agora |
| Por quanto tempo ficou em cada etapa? | Só por reconstrução: diferença entre eventos do log | Depende da sequência de eventos estar completa; não há tabela de transições |
| Com quem estava? | Enquadramento: `project_framings` (indicação e decisão, com autor e data). Demais etapas: deduzido da etapa | `analyst_id` mostra só o analista **atual**; não há registro de quem foi analista antes |
| O conteúdo enviado em cada versão? | `project_versions` (imutável por trigger, `00013`) | Só submissão e reenvio; edições de rascunho não ficam |

**Verificação feita:** todas as mudanças de etapa encontradas no código (`Submit`, início de análise, `RequestComplement`, `Resubmit`, indicação de departamento, confirmação e recusa do enquadramento, arquivamento, desistência, negação e expiração de prazo) gravam um evento de auditoria na mesma transação. Não achei mudança de etapa sem evento. Logo, o log **permite** reconstruir o histórico, e isso será usado para preencher o passado (seção 6). O problema é que a reconstrução depende de interpretar eventos, e o log pode ser alterado ou apagado sem barreira no banco.

### Pontos a corrigir ou ter em mente
1. `audit_logs` **não é protegido** contra `UPDATE`/`DELETE`, ao contrário de `project_versions`. Além disso não há rotina de retenção para ele: o `cleanup` do CLI mexe só em sessões, tokens, envios de arquivo, rascunhos antigos e e-mails enviados, e não toca em `audit_logs`.
2. A troca de analista não existe como ação (débito **T8**). Quando existir, precisa gerar uma linha de histórico nova (seção 3.3).
3. Após "Aguardando parecer" não existe saída (débito **T23**). O modelo não depende disso.

---

## 2. Decisões

### 2.1 Tomadas nesta conversa

1. Criar uma **tabela de transições** como fonte de verdade para prazos e para "com quem estava".
2. Manter `audit_logs` como trilha de "quem fez o quê" e usá-la **quando for necessário** (detalhe de uma ação, contexto de um intervalo).
3. As duas fontes se ligam: cada linha do histórico aponta para o evento de auditoria que a abriu.

### 2.2 Decisões com recomendação adotada (confirmar na revisão)

| # | Decisão | Recomendação adotada | Motivo |
|---|---|---|---|
| H1 | Onde gravar | No **serviço**, por uma única função central chamada em todo ponto que muda etapa, enquadramento, analista ou situação. Mais uma **trava no banco** (seção 4.4) que recusa a mudança sem a linha de histórico | O serviço conhece o autor e a ação; a trava garante que ninguém esqueça |
| H2 | O que abre uma linha nova | Qualquer mudança em **(situação, etapa, enquadramento, departamento, analista)** | Assim a troca de analista ou de departamento dentro da mesma etapa também aparece |
| H3 | Tempo | Guardar só os **instantes** de entrada e saída, em **horas corridas**. A apresentação mostra dias e horas (ex.: "2 dias e 4 horas"). Dias úteis, se um dia forem necessários, são cálculo na consulta | Permite mudar a regra depois sem reescrever dados |
| H4 | Passado dos projetos atuais | **Não se reconstrói nada.** Os dados atuais são só de desenvolvimento e o sistema ainda não está em produção. A migração apenas cria uma linha `initial` para cada projeto já enviado, para o banco sempre ter um trecho aberto igual ao estado atual | Sem dado real a preservar, reconstruir pelo log não compensa |
| H8 | Proteção do `audit_logs` | **Entra agora**, na fase 2: o banco recusa `UPDATE` e `DELETE` | Decidido na revisão; o histórico aponta para o log, então o log precisa ser confiável |
| H9 | Relatórios entre projetos | Fase própria **depois** da linha do tempo, sem atrasar a entrega básica (fase 6) | Decidido na revisão |
| H5 | Retenção | A tabela **não entra** em nenhuma limpeza automática | É dado de prazo e de responsabilidade |
| H6 | Quem pode ler | **Só a equipe.** Mesma checagem de alcance do projeto (`admin::projects.read`), mas o titular e os membros recebem 404, como nos demais campos internos (analista, departamento, conferência). Sem permissão nova nesta fase | O histórico traz o nome do analista e o departamento, que o proponente não vê |
| H7 | Conteúdo pessoal | A tabela guarda ids e o **nome copiado** do departamento. Não guarda CPF nem texto livre | Mantém as regras de LGPD da seção 8.3 do planejamento do fluxo |

---

## 3. Modelo

### 3.1 Tabela `project_stage_history`

Uma linha por **trecho de tempo** em que o projeto esteve numa combinação estável de situação, etapa, enquadramento, departamento e analista.

| Coluna | Para quê |
|---|---|
| `id` | Identificador |
| `project_id` | Projeto, sem exclusão em cascata: só rascunhos são excluídos e rascunho não tem histórico |
| `seq` | Ordem do trecho dentro do projeto (1, 2, 3…). Único por projeto |
| `status` | `submitted` ou `archived` |
| `stage` | Etapa (`awaiting_checklist`, `in_analysis`, `awaiting_proponent`, `awaiting_opinion`). No trecho de arquivamento mantém a última etapa do projeto |
| `framing_status` | `pending`, `confirmed` ou vazio, igual a `projects` |
| `holder_type` | Com quem está: `team` (aguardando conferência, ninguém assumiu), `analyst`, `department`, `proponent`, `none` (arquivado) |
| `holder_admin_id` | Analista responsável (quando `holder_type = analyst`). Sem chave estrangeira, para a exclusão de um admin não alterar a tabela imutável |
| `holder_department_id` e `holder_department_name` | Departamento, com o nome **copiado** como em `project_framings`, para sobreviver a renomeações |
| `entered_at` | Início do trecho |
| `left_at` | Fim do trecho; vazio no trecho atual |
| `entered_by_type` e `entered_by_id` | Quem causou a entrada: `admin`, `end_user`, `system` |
| `transition` | A ação que abriu o trecho, com o mesmo texto do log (`project.submit`, `project.resubmit`…) |
| `audit_log_id` | Evento de auditoria que abriu o trecho. Vazio nos trechos `initial` |
| `source` | `live` (gravado em tempo real) ou `initial` (estimado pela migração, para projetos que já existiam) |

**Regras de integridade (no banco):**
- No máximo **um trecho aberto** (`left_at` vazio) por projeto.
- `left_at` maior ou igual a `entered_at`.
- `holder_type` coerente com o resto: `analyst` exige `holder_admin_id`; `department` exige departamento; `none` só com `status = archived`.
- O trecho seguinte começa exatamente quando o anterior termina (a função central usa o mesmo instante).

### 3.2 Como `holder_type` é decidido

| Situação, etapa e enquadramento | `holder_type` |
|---|---|
| `submitted` + `awaiting_checklist` | `team` |
| `submitted` + `in_analysis`, sem enquadramento pendente | `analyst` (ou `team` se ainda sem analista) |
| `submitted` + `in_analysis` + enquadramento `pending` | `department` |
| `submitted` + `awaiting_proponent` | `proponent` |
| `submitted` + `awaiting_opinion` | `department` |
| `archived` | `none` |

Essa tabela fica em **um único lugar** no código, ao lado da definição das etapas, e é a mesma usada pela trava do banco e pelo preenchimento do passado.

### 3.3 Quando uma linha é fechada e outra aberta

A função central compara o projeto **antes e depois** da alteração:
- Se nada de (situação, etapa, enquadramento, departamento, analista) mudou, **não grava nada** (por exemplo, abrir um projeto que o analista já assumiu).
- Se mudou, fecha o trecho aberto em `T` e abre o novo em `T`, na mesma transação.

Casos que passam a gerar trecho novo, além das transições do fluxo: a troca de analista (T8) e a retirada de uma indicação pendente (T9), quando existirem.

### 3.4 Integração com `audit_logs`

Hoje `Record` só devolve erro. A mudança é devolver também o `id` do evento criado, para a função central preencher `audit_log_id`. A consulta de inserção passa a retornar o id. Nenhuma outra parte do log muda.

### 3.5 Exemplo de leitura

Um projeto enviado na segunda, assumido na terça, devolvido, reenviado e enquadrado teria trechos assim:

| seq | etapa | com quem | entrada | saída |
|---|---|---|---|---|
| 1 | aguardando conferência | equipe | seg 09:00 | ter 14:00 |
| 2 | em análise | Analista A | ter 14:00 | qua 10:00 |
| 3 | aguardando proponente | proponente | qua 10:00 | sex 16:00 |
| 4 | em análise | Analista A | sex 16:00 | seg 11:00 |
| 5 | em análise (enquadramento pendente) | Departamento X | seg 11:00 | seg 15:00 |
| 6 | aguardando parecer | Departamento X | seg 15:00 | (aberto) |

O tempo "com a equipe" é a soma dos trechos `team` e `analyst`; o tempo "com o proponente" é a soma dos `proponent`. A consulta é uma agregação simples.

---

## 4. Regras de negócio e proteção

### 4.1 Função central
Uma só função grava o histórico. Recebe a transação, o projeto antes e depois, o autor e o evento de auditoria. **Todos** os pontos abaixo a chamam, depois de alterar o projeto e antes de confirmar a transação:

`Submit`, início de análise (incluindo o caso de assumir projeto já em análise), `RequestComplement`, `Resubmit`, indicação de departamento, confirmação e recusa do enquadramento, `Archive`, `Withdraw`, `Deny` e a expiração por prazo (`ExpireComplements`, autor `system`).

Quem criar uma etapa ou ação nova passa a ter um único lugar para registrar, e o teste de invariante (seção 7) falha se esquecer.

### 4.2 Imutabilidade
Trigger na tabela, no mesmo estilo de `project_versions`:
- bloqueia `DELETE`;
- bloqueia `UPDATE`, exceto preencher `left_at` **uma única vez** (de vazio para um valor);
- impede alterar qualquer outra coluna.

### 4.3 Concorrência
A função central roda dentro da transação que já trava o projeto, então duas ações simultâneas não abrem dois trechos. O índice de "um trecho aberto por projeto" é a segunda barreira.

### 4.4 Trava no banco (rede de segurança) — feita na fase 4
Trigger de restrição **adiado até o commit** na tabela `projects` (migração `00033`): ao confirmar, todo projeto enviado ou arquivado precisa ter um trecho aberto igual ao estado dele; senão a transação inteira é recusada. Pega alteração feita fora do serviço (script, SQL manual, rotina nova que esqueceu a função central). Só dispara quando situação, etapa, enquadramento, departamento ou analista mudam, e confere o estado no commit (não o `NEW` do comando), então vários `UPDATE` na mesma transação funcionam.

A regra "com quem está" passou a existir também como função SQL `project_holder_type`, igual a `holderOf` em Go; dois testes (um em Go, outro no banco) usam os mesmos casos.

**Exceção deliberada:** se o analista vira nulo e nada mais mudou, a trava deixa passar. É o que acontece quando o admin responsável é excluído (`ON DELETE SET NULL`), e bloquear a exclusão de um admin por causa disso seria pior. O trecho aberto continua apontando para quem era o responsável, e a próxima ação sobre o projeto abre um trecho novo, já sem analista.

### 4.5 O que o histórico não faz
- Não substitui `audit_logs`: não guarda IP, justificativa nem payload. Para isso o `audit_log_id` leva ao evento.
- Não guarda o conteúdo dos campos editados. Isso é assunto de `project_versions` e, se for necessário um antes e depois por campo, de uma mudança separada no `payload` do log (fora deste plano).

---

## 5. API

Uma só rota, `GET /api/admin/projects/{id}/history`, com `{segments, summary}`: a tela precisa dos dois e assim faz uma chamada. Atualizado `backend/api/openapi.yaml` (um teste confere a cobertura).

| Campo | Conteúdo |
|---|---|
| `segments` | Trechos em ordem, cada um com etapa, com quem (`holder`, analista ou departamento com nome), entrada, saída, `durationSeconds` (o trecho aberto usa o instante da consulta; a tela formata como dias e horas), autor da entrada, `transition`, `auditLogId` e `estimated` |
| `summary` | `sinceSubmissionSeconds`, `currentStageSeconds` (trechos seguidos da etapa atual), `byHolder` e `byStage`, em segundos. Depois do arquivamento o tempo para de contar |

Relatório entre projetos (média por etapa, fila por responsável) fica para a fase 6, depois da linha do tempo. O modelo já comporta, com índice em `(stage, entered_at)` e em `(holder_admin_id, entered_at)`.

Códigos de erro seguem o padrão do projeto (`code` estável em inglês, `message` em português).

---

## 6. Projetos que já existem

O sistema ainda não está em produção e os projetos atuais são só de desenvolvimento. Por isso **não há reconstrução do passado** e não haverá comando de preenchimento.

A migração `00031` cria, para cada projeto já enviado (`status` diferente de rascunho), **uma linha** com o estado atual: `entered_at` igual a `submitted_at` na conferência e a `updated_at` nas demais etapas, `source = initial`, sem `audit_log_id`. Serve só para que a regra "todo projeto enviado tem um trecho aberto igual ao estado atual" valha desde o primeiro dia e para a trava da fase 4 não recusar ações em projetos antigos. A interface marca esses trechos como "tempo estimado".

Se algum dia houver dados reais antes desta entrega, reabrir esta seção: a reconstrução pelo `audit_logs` (cada evento abre um trecho conforme a etapa que ele produz, com checagem de que o último trecho bate com o estado atual) continua possível, porque todos os eventos de mudança de etapa são gravados.

---

## 7. Testes

**Banco:**
- Um trecho aberto por projeto; segundo trecho aberto é recusado.
- `UPDATE` de qualquer coluna além de `left_at`, e `DELETE`, são recusados; fechar um trecho já fechado é recusado.
- Coerência de `holder_type` (analista sem id, arquivado com `holder_type` diferente de `none`, etc.).
- A trava adiada (fase 4) recusa mudança de etapa sem trecho novo.

**Serviço (integração, em `internal/app`):**
- Para **cada** transição da tabela 3.1 do planejamento do fluxo: após a ação, existe exatamente um trecho fechado e um aberto, o aberto bate com o estado de `projects`, `audit_log_id` aponta para o evento certo.
- Ação que não muda nada (analista abre projeto já assumido) não grava trecho.
- Reenvio, recusa e nova indicação: três ou mais rodadas geram a sequência esperada, sem lacunas nem sobreposição.
- Expiração por prazo e desistência: autor correto (`system` e proponente).
- **Teste de invariante:** percorre todos os caminhos do fluxo e, ao final de cada passo, confere que o trecho aberto de cada projeto é igual ao estado atual dele. É o teste que impede esquecer a função central numa ação nova.
- Concorrência: duas ações simultâneas no mesmo projeto resultam em histórico válido (uma delas recebe 409, como já acontece).

**Migração:**
- Todo projeto enviado ganha exatamente um trecho `initial` aberto, igual ao estado atual; rascunho não ganha.

**Proteção do `audit_logs`:**
- `UPDATE` e `DELETE` em `audit_logs` são recusados; `INSERT` continua funcionando; consulta e filtros não mudam.
- Nenhum código existente apaga ou altera o log (conferido: não há `DELETE` nem `UPDATE` em `audit_logs` nas consultas).

**API:**
- Visibilidade: quem não vê o projeto recebe 404 também no histórico.
- Duração de trecho aberto, soma por responsável e por etapa.
- Cobertura do OpenAPI.

---

## 8. Frontend

Seguindo as convenções de `frontend/CLAUDE.md` (camadas `api/types.ts` → `service.ts` → `queries.ts`, prefetch no servidor, ícones só por `@/components/icons`).

- A aba **"Histórico"** que já existia no detalhe do projeto da equipe ganhou, no topo, o cartão "Etapas do projeto" (`staff/stage-timeline.tsx`), sem criar aba nova. Vem depois o pedido de complementação e, para quem pode ler a auditoria, o registro de auditoria do projeto (`AuditHistory`).
- Durações sempre em dias e horas (ex.: "2 dias e 4 horas"; menos de 1 hora mostra minutos). Resumo no topo: tempo desde o envio, tempo na etapa atual, totais com a equipe, com o proponente e com o departamento.
- Trechos `initial` com marca de "tempo estimado".
- O vínculo com o log não vira link por trecho: o registro de auditoria do projeto aparece logo abaixo para quem tem a permissão de ler a auditoria (a equipe comum não tem), e `auditLogId` fica na API para uso futuro.
- Textos em pt-BR; nomes de etapa reaproveitam os rótulos já usados na fila da equipe.

---

## 9. Fases de entrega

**Andamento:** fases 1 a 5 e a opção A da fase 6 implementadas (migração `00031`, `history.go`, `audit.Recorder.RecordID`, testes em `project_history_test.go`; fase 2: migração `00032`, teste `TestAuditLogsEImutavel`; fase 3: `history_view.go`, rota `/history`, testes `TestHistoricoPelaAPI` e `TestBuildHistory`; fase 4: migração `00033`, testes `TestTravaDoHistoricoDeEtapas` e `TestRegraDeQuemEstaComOProjeto`; fase 5: `stage-timeline.tsx` na aba "Histórico", conferida no navegador com um projeto estimado; fase 6, opção A: filtro `idleDays`, ordenação `stage_since` e coluna `stageSeconds` na fila da equipe, teste `TestFilaPorTempoNaEtapa`).

| Fase | Conteúdo | Pronto quando |
|---|---|---|
| **1** | Migração `00031` (tabela, índices, trigger de imutabilidade, linha `initial` dos projetos existentes); `Record` devolvendo o id; função central; chamada em todos os caminhos da seção 4.1 | Teste de invariante passa em todos os fluxos |
| **2** | Proteção do `audit_logs`: trigger que recusa `UPDATE` e `DELETE` (migração `00032`) | Testes da seção 7 passam e a suíte inteira continua verde |
| **3** | API `GET /history` e `/history/summary`, OpenAPI, testes | Cobertura de rotas passa |
| **4** | Trava adiada no banco (seção 4.4) | Alteração sem trecho é recusada no commit |
| **5** | Aba "Histórico" no frontend, com durações em dias e horas | Percorrida num navegador (ver T24) |
| **6** | Relatórios entre projetos. **A (feita):** na fila de análise, filtro "na etapa há N dias ou mais" (3, 7, 15, 30), ordenação "há mais tempo na etapa" e coluna "Na etapa há", sem tela nova. **B e C (a decidir depois de usar a A):** painel de prazos (média e pior caso por etapa, fila por analista e por departamento) e exportação em CSV. Se a B vier, os trechos `initial` ficam fora das médias | A feita; B e C só se a equipe pedir |

As fases 1 a 5 são a entrega básica. A fase 1 é a única que muda código de escrita. Cada fase termina com `make test` e `make lint` verdes e commit próprio.

---

## 10. Riscos e cuidados

1. **Esquecer um ponto de escrita.** Mitigação: função central única, teste de invariante e, na fase 4, a trava no banco.
2. **Duas fontes divergirem.** O histórico é a verdade para prazos; o log é a verdade para "quem fez o quê". O `audit_log_id` e o teste de invariante mantêm as duas alinhadas, e a reconstrução reporta divergência em vez de esconder.
3. **Trechos `initial` darem prazo errado.** Por isso o `source` existe e a interface avisa. Prazo para efeito legal deve usar só trechos `live`. Em produção só haverá `live`, já que a migração roda antes de qualquer dado real.
4. **Migração em tabela em uso.** A tabela é nova e a função central entra junto, então não há projeto "no meio" sem trecho após a fase 2. A ordem das fases 1 e 2 deve ser a mesma implantação.
5. **Tempo que parece trabalho.** O intervalo inclui fins de semana e feriados e não mede esforço. A interface deve dizer "tempo na etapa", não "tempo de trabalho".
6. **Proteção do log.** A fase 2 impede `UPDATE` e `DELETE` por trigger, mas quem tem superusuário no banco ainda pode desativá-lo. Isso é o limite de qualquer proteção interna; para exigência maior, enviar cópia do log a armazenamento externo (fora deste plano). Se um dia houver exclusão por lei (LGPD), será por rotina própria que desativa o trigger de forma controlada e registrada.

---

## 11. Decisões fechadas na revisão

1. **Dados antigos:** só de desenvolvimento; sem reconstrução (H4, seção 6).
2. **Tempo:** horas corridas, apresentadas em dias e horas (H3).
3. **Relatórios entre projetos:** fase 6, depois da linha do tempo (H9).
4. **`audit_logs`:** proteger agora, na fase 2 (H8).

Sem decisões pendentes para começar a fase 1.

---

## 12. Atualizações de documentação ao final

- `backend/api/openapi.yaml` (rotas novas).
- `backend/README.md`: tabela, função central e a proteção do `audit_logs`.
- `README.md`: citação do histórico de etapas.
- `docs/debitos-tecnicos.md`: atualizar **T8** e **T9** (a troca de analista e a retirada de indicação devem gerar trecho).
- `graphify-out/`: regenerar o grafo.
