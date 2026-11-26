# Planejamento — Abertura do processo no SIGADOC (depois do enquadramento)

Fonte da verdade do fluxo: [bpmn/convenios-e-fomento.bpmn](bpmn/convenios-e-fomento.bpmn) (processo "Cadastro, análise, enquadramento e abertura do processo no SIGADOC"). Este documento diz o que construir, em que ordem e como validar. Não contém código.

**Escopo.** Do "Enquadramento correto? Sim" até "Segue para o parecer técnico": o projeto volta à fila dos analistas, um analista assume, confere o Termo de Abertura, informa o PIN uma única vez e o sistema monta o processo no SIGADOC em segundo plano (Termo assinado e autuado, relatório e documentos do proponente juntados e assinados). Inclui também o **e-mail aos analistas** quando o projeto entra na fila e o **acompanhamento obrigatório da tramitação** do processo no SIGADOC (projetos ativos e arquivados). **Fora do escopo:** parecer técnico, formalização (PGE, assinatura do termo, Diário Oficial, portaria do fiscal) e prestação de contas.

**Estado (24/11/2026), branch `feat/integracao-sigadoc`:** **F0 a F4 e F6 implementadas contra o simulador** (seção 13). **A MTI liberou o SIGADOC de desenvolvimento** (novo usuário de sistema; 16 métodos liberados, 0 bloqueados): falta exercitar a escrita de verdade e ajustar o que o simulador não revelou. O plano da continuidade está na **seção 14**. A solicitação enviada à MTI está em [solicitacao-mti-sigadoc.md](solicitacao-mti-sigadoc.md) (a parte de produção segue pendente). O BPMN e este plano estão alinhados com as decisões S1 a S12.

---

## 1. Ponto de partida

### 1.1 No sistema
| Peça | Situação |
|---|---|
| Fluxo até o enquadramento (conferência, departamento, Revisor, arquivamentos) | Existe |
| Responsável pela análise (assumir, take-over) | Existe ([planejamento-responsavel-pela-analise.md](planejamento-responsavel-pela-analise.md)) |
| Relatório resumido e completo em PDF, com fila em segundo plano e andamento | Existe ([planejamento-relatorio-projeto.md](planejamento-relatorio-projeto.md)). `internal/pdfbuild` tem `Merge`, `ImageToPDF`, `Sanitize`, `PageCount`; `internal/preview` converte Word (Gotenberg) |
| Cliente do X-Via (TLS com certificado próprio, `x-road-client`) | Existe no `internal/mtid`, para o SmartPass |
| Etapa pós-enquadramento | Hoje o `framing_service.go` grava `awaiting_opinion` ao confirmar. **Muda** (seção 4) |
| Qualquer código do SIGADOC | **Não existe** |
| Divisão de PDF em partes | **Não existe** (`pdfbuild` não tem `Split`) |

### 1.2 No SIGADOC de desenvolvimento (testado em 16 e 17/11/2026)
- **Autenticação funciona:** `POST /siga/autenticar?matricula=...` com `SIGADOC_USERNAME` e `SIGADOC_PASSWORD` (usuário de sistema) devolve um JWT HS256 de cerca de 8 horas. A URL é `X_VIA_SEDEC_BASE_URL` + `/GOV/MTI/SIGADOC`.
- **O usuário de sistema representa qualquer matrícula:** `SEDEC123456` e `SEDEC654321` autenticaram. A matrícula do analista se descobre pelo CPF: `GET /siga/matricula?cpf=&sigla=SEDEC` (liberado).
- **O contrato real está no próprio SIGADOC:** `GET .../sigaex/api/v1/swagger.yaml`, salvo em `docs/sigadoc/swagger-sigaex.yaml` (e `swagger-siga.yaml`). Os arquivos em markdown de criar, finalizar, tramitar etc. estavam vazios; o swagger traz `POST /documentos`, `/autuar`, `/finalizar`, `/juntar`, `/anexar`, `/tramitar` e `GET /movimentacoes`.
- **A sigla vai sem traços nem barras** e com a via ou o volume no fim (ex.: `SEDECMEM202600001A`, `...V01`).
- **Autorização por método:** o usuário de sistema só tem acesso a alguns métodos. Hoje:

| Liberado (testado) | Bloqueado com 403 (testado) |
|---|---|
| `POST /siga/autenticar`, `GET /siga/matricula`, `GET /sigaex/documentos` (pesquisa, período de até 31 dias), `GET /sigaex/documentos/{sigla}`, `GET .../movimentacoes` | `GET /sigaex/modelos`, `/modelos/{id}`, `POST /modelos/{id}/processar-entrevista`, `GET /sigaex/classificacoes`, `POST /siga/pin/validar`, `POST /documentos/{sigla}/finalizar`, `/autuar`, `/juntar`, `/anexar`, `/tramitar`, `/assinar-com-pin`, `/gerar-protocolo`, `GET .../pdf-completo`, `/dossie`, `/consultar-protocolo`, `/consultar-arquivado`, `/modelos-para-autuar` |

`POST /sigaex/documentos` (criar) **não foi testado** (evitar criar documento solto); assume-se bloqueado. Nenhum PIN foi enviado nos testes.

---

## 2. Decisões

### 2.1 Confirmadas
| # | Decisão |
|---|---|
| S1 | Depois do enquadramento confirmado o projeto vai para a etapa **Aguardando analista** (`awaiting_analyst`), sem responsável. Ao assumir, passa a **Abertura do processo no SIGADOC** (`opening_sigadoc`). Só ao concluir a montagem vai para **Aguardando parecer** (`awaiting_opinion`). |
| S2 | A abertura é uma **ação explícita** do analista responsável (botão "Abrir no SIGADOC"), e só enquanto não houver número de processo gravado. |
| S3 | O **PIN** é digitado uma única vez, validado antes de começar e **nunca gravado** (banco, fila, cache, log, auditoria, mensagem de erro). Vive só na requisição e na memória da tarefa. |
| S4 | Montagem **em segundo plano e em memória**, sem fila persistente. Se a API cair, a tarefa fica **interrompida**; o analista informa o PIN de novo e só o que faltou é refeito. |
| S5 | Ordem fixa: assinar o Termo → autuar e gravar o número do processo → gerar e incluir o relatório → incluir cada documento, tudo assinado com o mesmo PIN. |
| S6 | Inclusão no processo sempre por **juntada** (`/juntar`), não por anexação. O **relatório** gerado pelo sistema entra como documento **interno** (`Interno Capturado`); os documentos do **proponente** entram como documento **capturado externo** (`Externo Capturado`). |
| S7 | **Todos** os documentos entram, inclusive Word e imagem, convertidos para PDF antes. Um item com vários arquivos gera **um documento por arquivo**. Arquivo acima de **24 MB** é dividido em partes de no máximo 24 MB, cada uma um documento. Item que só tem justificativa de não envio não gera documento. |
| S8 | Projeto **arquivado no sistema não é arquivado no SIGADOC.** A tela mostra um aviso chamativo ("arquivado no sistema; no SIGADOC o processo segue sem arquivamento") e o sistema **expõe o que foi acrescentado ao processo depois do arquivamento**. A observação dura **até o processo ser arquivado também no SIGADOC**, sem prazo fixo. |
| S9 | A matrícula do analista no SIGADOC vem do CPF (`/siga/matricula`) e a montagem usa o token do usuário de sistema representando essa matrícula. A assinatura vale, portanto, para o analista. |
| S10 | Desistência, negação ou arquivamento **cancelam** uma montagem em andamento e o PIN é descartado. O que já foi inserido permanece no SIGADOC. |
| S11 | Quando o projeto entra em "Aguardando analista", o sistema envia **e-mail a todos os Analistas ativos**, só com dados não pessoais (código, título, instrumento, valores, departamento) e o link, pela caixa de saída, na mesma transação que muda a etapa. |
| S12 | O **acompanhamento da tramitação é obrigatório**: depois que o número do processo é gravado, o sistema consulta o processo periodicamente (projeto ativo ou arquivado) e registra no histórico cada movimentação nova, com a descrição do documento tramitado. Só acompanha: parecer, despachos e tramitações são feitos no SIGADOC. Termina quando o processo é arquivado no SIGADOC. |

### 2.2 Premissas a confirmar (marcadas no BPMN)
| # | Premissa | Como confirmar |
|---|---|---|
| P1 | `Interno Capturado` é o tipo certo para o relatório. | MTI ou teste com a escrita liberada |
| P2 | Os modelos (Termo de Abertura, documento capturado interno e externo), a classificação documental, o nível de acesso e a lotação de destino são **parâmetros** (não fixos no código). | Definir com a SEDEC; guardar como parâmetros de regra (`internal/rules`) editáveis sem deploy |
| P3 | O número do processo vem da consulta ao documento depois de autuar (o `/autuar` devolve só um `id`). | Teste com a escrita liberada |
| P4 | Página única acima de 24 MB não tem como dividir: o item fica com falha e o analista é avisado. | Confirmar se o SIGADOC realmente rejeita acima de 24 MB e o que fazer com PDF cheio de imagens |
| P5 | O arquivamento no SIGADOC é reconhecido pelos marcadores e movimentações do `GET /documentos/{sigla}` (o `/consultar-arquivado` está bloqueado). | Teste com um processo arquivado; ou pedir a liberação do `/consultar-arquivado` |

---

## 3. O que depende da MTI (lista para enviar)

Liberar ao usuário de sistema `<usuário-de-sistema-antigo>` no SIGADOC de **desenvolvimento** e depois no de produção:

1. **Modelos:** `GET /sigaex/modelos`, `GET /sigaex/modelos/{id}` (e `/ativo`), `POST /sigaex/modelos/{id}/processar-entrevista`.
2. **PIN:** `POST /siga/pin/validar`.
3. **Escrita:** `POST /sigaex/documentos`, `POST .../finalizar`, `POST .../assinar-com-pin`, `POST .../autuar`, `POST .../juntar`.
4. **Leitura do processo:** `GET .../pdf-completo`, `GET .../dossie`, `GET .../consultar-protocolo`, `GET .../consultar-arquivado`.
5. **Se for usar:** `POST .../tramitar` (a tramitação para outra lotação) e `GET /sigaex/classificacoes`.
6. Confirmar que o usuário de sistema **pode representar qualquer matrícula da SEDEC** também em produção.
7. Informar: nome exato do modelo do Termo de Abertura, campos da entrevista, classificação documental, nível de acesso e lotação de destino.

---

## 4. Etapas do projeto (mudança de estado)

```
Em análise ──Indicar departamento──▶ enquadramento pendente ──Revisor confirma──▶ Aguardando analista (awaiting_analyst)
                                                                                         │ Assumir (analista fica responsável)
                                                                                         ▼
                                                                     Abertura do processo no SIGADOC (opening_sigadoc)
                                                                      │ Abrir no SIGADOC → PIN → montagem (roda, falha, retoma)
                                                                      ▼ tudo inserido e assinado
                                                                     Aguardando parecer (awaiting_opinion)
```

Desistência e negação valem em todas as etapas; o arquivamento ocorre só no sistema (S8).

**O que muda no código:**
- `framing_service.go`: ao confirmar, a etapa passa a ser `awaiting_analyst`, o responsável é liberado e isso entra no histórico; o departamento e a decisão `confirmed` permanecem. Na **mesma transação** entra na caixa de saída o e-mail aos Analistas ativos (S11; `DedupKey` por projeto e rodada de enquadramento, para não duplicar), no padrão de `notify.go`, com a lista permitida de campos do e-mail ao Supervisor (nada de CPF nem dados pessoais).
- Nova migração: restrições de etapa de `projects` e de `project_stage_history` aceitam `awaiting_analyst` e `opening_sigadoc` (hoje em `00027` e `00031`); atualizar a trava de imutabilidade do histórico (`00033`) se necessário.
- Locais que tratam `StageAwaitingOpinion` hoje: `history.go`, `queue_overview.go`, `overview.go`, `handler.go` (filtro `stage`). Cada um ganha as duas etapas novas (tempo em etapa, quem detém o projeto, abas da fila, visão geral do analista).
- **O relatório** já olha a decisão do enquadramento, não a etapa (confirmado no planejamento dele), então continua disponível nas etapas novas.
- Assumir da etapa nova reutiliza a regra de assumir/take-over existente; o **take-over fica bloqueado enquanto há montagem em andamento**.
- Testes que hoje esperam `awaiting_opinion` logo após confirmar (`project_framing_test.go`, `project_lifecycle_test.go`, `project_history_test.go`, `project_queue_test.go` e outros) são reescritos.

---

## 5. Modelo de dados (migração nova)

Sem dados pessoais além do que já existe; **nenhuma coluna guarda PIN**.

| Tabela | Campos principais | Regras |
|---|---|---|
| `project_sigadoc` (1 por projeto) | `project_id` (PK), `matricula`, `process_number` (nulo até autuar), `termo_sigla`, `process_sigla`, `opened_by_id`, `opened_at`, `sigadoc_archived_at` (nulo enquanto ativo no SIGADOC), `watch_next_at`, `watch_interval_seconds` | `process_number` **único e imutável** depois de gravado (trigger, como a `00033`); só se grava com a etapa `opening_sigadoc` |
| `sigadoc_tasks` | `id`, `project_id`, `started_by_id`, `status` (`running`, `interrupted`, `completed`, `cancelled`, `failed`), `started_at`, `finished_at`, `progress_done`, `progress_total`, `failures` (jsonb), `error` (texto **sem** dados do PIN), `heartbeat_at` | Índice único parcial: **no máximo uma tarefa `running` por projeto** (como `project_reports_active_key`) |
| `sigadoc_items` | `task_id`, `seq`, `kind` (`termo`, `report`, `document`), `file_id`/`item_ref`, `part` e `parts`, `sha256`, `status` (`pending`, `created`, `finalized`, `signed`, `joined`, `failed`), `sigla` (do documento criado no SIGADOC), `description`, `error` | O **status por passo** é o que permite retomar do ponto certo. A idempotência é por (projeto, item, arquivo, parte, hash): na retomada o que já está `joined` é pulado |
| `sigadoc_movements` | `project_id`, `mov_id` (do SIGADOC), `type`, `description`, `performed_at`, `performed_by`, `seen_at`, `after_project_archive` (booleano) | Único por (`project_id`, `mov_id`); alimenta a aba do histórico e o aviso do S8 |

Auditoria: toda ação (iniciar, retomar, cancelar, concluir) vai para `audit_logs`; o histórico de etapas ganha as transições novas. Registro de falhas guarda o código e a mensagem do SIGADOC, **nunca o corpo da requisição**.

---

## 6. Backend

### 6.1 Pacote `internal/sigadoc` (padrão do `mtid`)
- `Client` (interface) com os passos de que o fluxo precisa: autenticar por matrícula (com cache do token por matrícula, validade de 8 h e renovação), buscar matrícula por CPF, validar PIN, preencher a entrevista do modelo (pré-visualização), criar documento (capturado), finalizar, assinar com PIN, autuar, juntar, consultar documento, listar movimentações.
- `HTTP`: reaproveita o transporte TLS do X-Via (`X_VIA_CA_FILE`, `X_VIA_TLS_SERVER_NAME`, `x-road-client`); extrair esse transporte do `mtid` para um pacote comum, sem mudar o comportamento do MT-ID. Serviço padrão `GOV/MTI/SIGADOC`.
- `Memory`: simulador para desenvolvimento e testes (números de processo e siglas fictícios, falhas injetáveis). `SIGADOC_SIMULATE` é **recusado em produção**, como o `MTID_SIMULATE`.
- Erros tipados: PIN recusado (**nunca retentar**: o SIGADOC pode bloquear o PIN do analista), sem permissão (403 do método), documento inexistente, fora do ar, tempo esgotado.
- Configuração (`config.go` + `.env.example` + os dois compose): `SIGADOC_USERNAME`, `SIGADOC_PASSWORD`, `X_VIA_SIGADOC_SERVICE` (padrão `GOV/MTI/SIGADOC`), `SIGADOC_SIMULATE`, `SIGADOC_TASK_*` (timeouts, limites). Validação que impede subir em produção sem credencial; senha nunca em log.

### 6.2 Montagem (motor)
1. **Registrar a tarefa** (sem PIN) com os itens previstos e disparar a goroutine; responde 202 na hora.
2. Passos por item: criar documento → finalizar → assinar → juntar. A cada passo o status é gravado **antes de seguir**.
3. **Reconciliação antes de criar:** se um item está `pending` mas pode ter sido criado antes de uma queda, pesquisar no SIGADOC (`GET /sigaex/documentos` pela descrição padronizada, janela de até 31 dias) e adotar a sigla encontrada em vez de criar duplicado.
4. **Preparação dos arquivos** reaproveita a do relatório: Word pelo Gotenberg, imagem por `ImageToPDF`, limpeza por `Sanitize`, limites de memória e de entrada. **Novo:** `pdfbuild.Split` que divide um PDF em partes de até 24 MB (por páginas, conferindo o tamanho real de cada parte).
5. **Descrição padronizada** de cada documento: «&lt;Tipo do documento&gt; — &lt;proponente ou emenda&gt; — &lt;referência/vigência&gt;», acrescida de "arquivo n de N" e "parte n de N" quando couber. Ela é também a chave da reconciliação.
6. **Ordem:** a do checklist (emendas agrupadas), depois os documentos adicionais, arquivo por arquivo.
7. **Fim:** `defer` zera o PIN (buffer `[]byte`) em qualquer saída (sucesso, falha, cancelamento, pânico). Todo erro da montagem é recuperado (`recover`) e vira tarefa `interrupted`.
8. **Ao subir a API**, tarefas `running` viram `interrupted` (o PIN se perdeu); `App.Wait` aguarda a tarefa até um limite no desligamento e a marca como `interrupted`.
9. **Cancelamento:** desistência, negação e arquivamento sinalizam o contexto da tarefa; ela para no próximo ponto seguro e fica `cancelled`.
10. **Concluir:** com todos os itens `joined` (e Termo/relatório assinados), grava o histórico e muda a etapa para `awaiting_opinion`.

### 6.3 Rotas (`/api/admin`, atualizar `api/openapi.yaml`; há teste que exige)
| Rota | O que faz |
|---|---|
| `GET /projects/{id}/sigadoc` | Estado: etapa, matrícula do analista, número do processo, tarefa atual e itens, aviso de arquivamento |
| `POST /projects/{id}/sigadoc/termo` | Gera a pré-visualização do Termo (campos preenchidos com os dados do projeto) para conferência; **não grava nada no SIGADOC** |
| `POST /projects/{id}/sigadoc/open` | Corpo: Termo conferido e PIN. Valida o PIN, cria a tarefa e responde 202. Recusa (409/422) se não for o responsável, se a etapa estiver errada, se já houver número ou tarefa em andamento |
| `POST /projects/{id}/sigadoc/resume` | Corpo: PIN. Refaz só o pendente de uma tarefa `interrupted` |
| `POST /projects/{id}/sigadoc/cancel` | Cancela a tarefa em andamento (analista responsável) |
| `POST /projects/{id}/sigadoc/movements/seen` | Marca as movimentações novas como vistas |

Permissões: ação nova `admin::projects.open-sigadoc` (Analista, só com a condição de responsável pela análise) e leitura do estado com `admin::projects.read` para a equipe. O Proponente **não** vê nada do SIGADOC. A seed e a migração de perfis incluem a ação.

### 6.4 Acompanhamento da tramitação (S8 e S12)
- Um laço em segundo plano (como o `expireComplements` e o worker do relatório) consulta, a cada `watch_next_at`, **todos os processos com número gravado** (projeto ativo **ou** arquivado no sistema) que ainda **não foram arquivados no SIGADOC**: `GET /sigaex/documentos/{sigla}` e `GET .../movimentacoes`, com o token do usuário de sistema representando a matrícula de quem abriu o processo.
- Cada movimentação nova (id do SIGADOC como chave única) é gravada em `sigadoc_movements` com tipo, **descrição do documento tramitado**, data, quem fez e lotações de origem e destino, e aparece na aba de histórico como "Tramitação no SIGADOC". O sistema **não escreve nada** no SIGADOC por este caminho.
- Movimentações com data posterior ao arquivamento no sistema são gravadas com `after_project_archive = true` e expostas em destaque. Quando o SIGADOC mostra o processo arquivado, grava `sigadoc_archived_at` e **encerra o acompanhamento**.
- **Intervalo crescente** para não pesar (por exemplo 15 min no primeiro dia, depois de hora em hora, depois diário, com teto), com `heartbeat` e limite de consultas por ciclo. Falha de rede não encerra a observação.
- Sem a liberação do `GET .../consultar-arquivado`, o arquivamento é deduzido dos marcadores/movimentações (P5).

---

## 7. Frontend
- **Rótulos e filtros:** "Aguardando analista" e "Abertura do processo no SIGADOC" em `stageLabels`, na fila (nova aba "Aguardando analista"; os analistas veem todos, só o responsável age depois) e na visão geral do analista.
- **Assistente "Abrir no SIGADOC"** (botão no detalhe do projeto, só para o responsável na etapa certa):
  1. **Conferir o Termo:** mostra o texto pré-preenchido e permite ajustar;
  2. **PIN:** campo de senha, sem autocompletar, enviado uma única vez; erro de PIN volta para o campo e avisa que repetir pode bloquear o PIN;
  3. **Andamento:** "n de N", falhas por item, botão de cancelar, e o analista pode sair da tela.
- **Tarefa interrompida:** painel com o que já foi concluído (número do processo, itens) e botão "Retomar" que pede o PIN de novo.
- **Projeto concluído:** número do processo, sigla e lista do que foi inserido.
- **Histórico do projeto (equipe):** nova aba/seção "Tramitação no SIGADOC" com as movimentações (data, descrição do documento tramitado, quem, lotações), para projetos ativos e arquivados.
- **Projeto arquivado com processo no SIGADOC:** faixa de alerta no topo ("Arquivado no sistema. No SIGADOC o processo <número> segue sem arquivamento") e a lista **"Acrescentado no SIGADOC depois do arquivamento"** (data, descrição, quem), com selo de novas. Quando o SIGADOC arquiva, a faixa muda para "arquivado nos dois".
- **PIN no navegador e no proxy BFF:** a rota `src/app/bff/[...path]` não pode registrar o corpo; nenhum log do Next recebe o PIN; nada vai para `localStorage` ou URL.
- Convenções do projeto: `features/projects/api/types → service → queries`, `useAppForm` + Zod, ícones de `@/components/icons`, `useCan()`/`<Can>`.

---

## 8. Segurança e privacidade
1. **PIN:** só em memória; `[]byte` zerado ao fim; nunca em `slog`, auditoria, mensagem de erro, corpo guardado, métrica ou rastreio. O log de requisições da API precisa **ocultar** o corpo das rotas `open` e `resume`.
2. **Tentativas do PIN:** validar antes de começar (evita gastar tentativas dentro da montagem), nunca retentar PIN recusado, limitar abertura por analista (taxa) e responder 429.
3. **Quem pode:** só o analista responsável; o servidor confere (não confiar na tela). Outro analista só por take-over.
4. **Matrícula:** vem do CPF do token do MT Login do analista; nunca aceitar matrícula enviada pelo navegador.
5. **Credencial do usuário de sistema:** só no servidor, variável de ambiente; fora do `.env.example` com valor real; nunca em log.
6. **Dados enviados ao SIGADOC:** o que o projeto já contém; a descrição dos documentos não leva CPF.
7. **Conteúdo não confiável:** os arquivos dos proponentes passam por `Sanitize` e pelas verificações do relatório antes de seguir.
8. **Atenção ao e-mail da equipe:** continua sem CPF e sem dados pessoais (regra D9).

---

## 9. Testes
**Backend (simulador + Postgres via testcontainers, em `internal/app`):**
- caminho feliz: assumir → termo → abrir → etapa `awaiting_opinion`, número gravado e histórico completo;
- **PIN inválido:** nenhuma tarefa nasce, nenhuma tentativa extra é feita no SIGADOC;
- falha no meio (injetada em cada passo) → `interrupted` → retomada refaz **só o pendente**, sem duplicar documento;
- API reiniciada com tarefa `running` → `interrupted`; reconciliação não duplica item criado antes da queda;
- abertura concorrente (duas requisições) → uma vence, a outra 409; número do processo imutável;
- só o responsável abre; take-over bloqueado com tarefa ativa; Proponente não vê nada;
- cancelamento por desistência, negação e arquivamento; PIN descartado;
- **PIN fora de tudo:** capturar o log e as tabelas após uma execução e provar que o PIN não aparece em nenhum lugar;
- arquivos: Word e imagem convertidos; item com vários arquivos vira vários documentos; arquivo de 50 MB vira partes ≤ 24 MB; página gigante vira falha de item;
- e-mail aos Analistas: um por Analista ativo, sem duplicar na mesma rodada, sem CPF nem dados pessoais, e que **desfaz junto** se a transação do enquadramento falhar;
- acompanhamento da tramitação: movimentação nova entra no histórico com a descrição, sem duplicar; projeto ativo e arquivado; a posterior ao arquivamento fica marcada `after_project_archive`; encerra quando o SIGADOC arquiva; falha de rede não encerra e o intervalo cresce;
- migração de etapa (cima e baixo) e transições novas no histórico.

**Contrato:** servidor HTTP falso do SIGADOC com as respostas do swagger (incluindo 403 de método e erros de PIN), para o cliente real.

**Real (manual/opcional):** teste ligado por variável de ambiente contra o X-Via de desenvolvimento, só depois da liberação pela MTI, usando `SEDEC123456` e `SEDEC654321`.

**Frontend:** `typecheck`, lint e formato sem problema novo (não há suíte de testes de interface) e um **roteiro de aceite** novo no formato do [roteiro-aceite-fluxo-equipe.md](roteiro-aceite-fluxo-equipe.md).

---

## 10. Fases

| Fase | Entrega | Depende da MTI? | Pronto quando |
|---|---|---|---|
| **F0** | Comando `cli sigadoc:check`: autentica e roda a matriz de acesso por método (sem PIN e sem escrita); mostra o que está liberado | Não (a liberação é o que ele mede) | Mostra a tabela da seção 1.2 e serve para conferir depois do retorno da MTI |
| **F1** | Etapas `awaiting_analyst` e `opening_sigadoc`: migração, transição do enquadramento, **e-mail aos Analistas (S11)**, histórico, filas, rótulos, assumir; testes reescritos | Não | Confirmar o enquadramento leva à fila dos analistas (que recebem o e-mail) e assumir leva à etapa nova, sem SIGADOC |
| **F2** | Pacote `sigadoc` (interface, HTTP, simulador), configuração, variáveis nos compose, extração do transporte X-Via | Parcial | Cliente real autentica e consulta; simulador cobre o resto |
| **F3** | Pré-visualização do Termo e validação do PIN (rotas, tela do passo 1 e 2) | **Sim** (modelos, `pin/validar`) | O analista vê o Termo e o PIN é validado sem gravar nada |
| **F4** | Motor de montagem, `Split`, itens, retomada, cancelamento, concluir etapa | **Sim** (escrita) | Processo completo no SIGADOC de dev, com número gravado e etapa `awaiting_opinion` |
| **F5** | Painel de andamento, retomada e tela do projeto concluído | Não | Roteiro de aceite percorrido |
| **F6 (obrigatória)** | **Acompanhamento da tramitação** (S12): laço periódico com intervalo crescente, tabela de movimentações, aba "Tramitação no SIGADOC" no histórico (projetos ativos), e para projetos arquivados o aviso chamativo e a lista do que foi acrescentado depois do arquivamento | Parcial (leitura já liberada; P5) | Movimentação nova no SIGADOC aparece no histórico com a descrição do documento; a posterior ao arquivamento fica em destaque; encerra quando o SIGADOC arquiva |
| **F7** | Endurecimento: teste do PIN fora de logs, carga, `openapi.yaml`, runbook, débitos técnicos, variáveis de produção | Não | Suíte e lint verdes; documentação de implantação atualizada |

**Ordem sugerida:** F0 e F1 já (não dependem de ninguém), F2 em paralelo; **a leitura (F6) também não depende da MTI**, porque `GET /documentos/{sigla}` e `/movimentacoes` já estão liberados, então pode andar logo depois da F2; F3 a F5 conforme a MTI liberar os métodos de escrita. O simulador permite construir e testar tudo antes da liberação.

---

## 11. Riscos

| Risco | Mitigação |
|---|---|
| A MTI demora a liberar os métodos de escrita | Simulador completo; F0 mostra o que falta; lista da seção 3 pronta para enviar |
| PIN bloqueado por tentativas erradas | Validar antes, não retentar, aviso na tela, limite por analista |
| API cai no meio e o PIN se perde | Tarefa `interrupted` com progresso por item; retomada refaz só o pendente (S4) |
| Documento duplicado no SIGADOC | Reconciliação por descrição padronizada antes de criar; chave por hash; estado por passo |
| Documento criado, mas não assinado ou juntado | Status por passo (`created`, `finalized`, `signed`, `joined`); a retomada continua do passo certo |
| Memória da API (1 GB) com PDFs grandes | Reusar os limites e a medição do relatório; um item por vez; arquivos temporários em disco |
| Contrato do SIGADOC muda | Teste de contrato com o swagger versionado em `docs/sigadoc/` |
| Observação de projetos arquivados cresce sem fim | Intervalo crescente, limite por ciclo, encerra quando o SIGADOC arquiva (S8); painel de quantos estão sendo observados |
| `Interno Capturado` não ser o tipo certo (P1) | Confirmar com a MTI; o tipo é um parâmetro, não está fixo no código |
| Analista sem matrícula no SIGADOC | `/siga/matricula` vazio → mensagem clara e bloqueio da abertura |

---

## 12. Débitos técnicos previstos (registrar em [debitos-tecnicos.md](debitos-tecnicos.md))
- Montagem em memória: queda da API exige novo PIN (decisão S4).
- Tramitação para outras lotações (`/tramitar`) e parecer técnico (modelados à parte).
- Se o `GET .../consultar-arquivado` continuar bloqueado, o arquivamento no SIGADOC segue deduzido dos marcadores (P5).

---

## 13. O que já foi feito

### F0 — `sigadoc:check` (implementada)
- `internal/xvia`: o transporte TLS do X-Via saiu do `mtid` para um pacote comum (o `mtid` passou a usá-lo, sem mudar o comportamento; os testes dele continuam verdes).
- `internal/sigadoc`: cliente HTTP com `Authenticate` (JWT por matrícula, lendo o `exp`), `MatriculaByCPF` e a sondagem `Check`. Erros tipados (`IsForbidden`, `IsAuthFailed`, `IsUnknownIdentity`); o stacktrace do SIGADOC nunca entra na mensagem; senha e token nunca vão para o log.
- `cli sigadoc:check --matricula SEDEC123456 [--strict]`: sonda cada método do fluxo com uma sigla inexistente (não altera nada), **não** cria documento e **não** valida PIN (esses dois aparecem como "não sondado"). Com `--strict` falha se algum método obrigatório estiver bloqueado.
- Configuração: `SIGADOC_USERNAME`, `SIGADOC_PASSWORD`, `X_VIA_SIGADOC_SERVICE`, `SIGADOC_SIMULATE` (recusado em produção), nos `.env.example`, no `config.go` (com validação) e nos dois compose.
- Resultado contra o SIGADOC de desenvolvimento: 4 liberados (matrícula, consultar documento, movimentações, pesquisa), 12 bloqueados, 2 não sondados (a tabela da seção 1.2).

### F1 — etapas `awaiting_analyst` e `opening_sigadoc` (implementada)
- **Migração `00040`:** as duas etapas nas restrições de `projects` e do histórico; `project_holder_type` (rede de segurança do histórico) trata `opening_sigadoc` como do analista.
- **Confirmar o enquadramento** leva o projeto a `awaiting_analyst`, **libera o analista responsável** (a consulta `SetProjectFraming` ganhou `release_analyst`) e grava, na mesma transação, o **e-mail a todos os Analistas ativos** (`notifyAwaitingAnalyst`: lista permitida de campos, sem dados pessoais; chave de deduplicação por projeto, rodada e endereço).
- **Assumir:** `start-analysis` em `awaiting_analyst` leva a `opening_sigadoc` e torna quem chamou o responsável (consulta `AssumeProjectOpening`, ação de auditoria `project.assume-opening`); o take-over vale em `opening_sigadoc` e não vale em `awaiting_analyst`.
- **Fila e visões:** filtro `stage` aceita as etapas novas; contagens da visão geral do analista e do proponente; novo bloco `awaiting_analyst` ("Enquadrados aguardando analista") na visão geral do analista; as etapas novas não têm limite de "parado" (sem regra vigente) até o parâmetro `stalled_after_days` ganhar chaves para elas.
- **Frontend:** rótulos, abas da fila (Aguardando analista e Abertura no SIGADOC), cartões, botão **Assumir projeto**, faixas de situação para a equipe, aviso ao proponente e a transição no histórico.
- **Correção de uma brecha antiga:** o analista ainda conseguia **arquivar por falta de documentos um projeto já enquadrado** (a regra não olhava a etapa). Agora, depois do enquadramento, só o proponente (desistência) e o Secretário (negação) arquivam.
- **Testes:** os que esperavam `awaiting_opinion` logo após confirmar foram ajustados (`project_framing_test.go`, `project_history_test.go`) e há testes novos em `project_sigadoc_stage_test.go` (fila, e-mail sem dados pessoais e sem duplicar, assumir, take-over, filtros, contadores, bloqueio da conferência, desistência e negação).

### F2 — cliente completo e simulador (implementada)
- **Interface `sigadoc.Client`** com tudo o que o fluxo usa: autenticar, matrícula por CPF, pré-visualizar o Termo, validar PIN, criar documento (modelo ou capturado, com o PDF), finalizar, assinar, autuar, juntar e consultar movimentações. A implementação real (`HTTP`) segue o contrato do swagger; **os métodos de escrita ainda não foram exercitados contra o SIGADOC de verdade** (estão bloqueados), só contra um servidor falso com as respostas do swagger.
- **`sigadoc.PIN`:** tipo que se recusa a ser impresso (`%s`, `%v`, `%#v` e `slog` mostram `[PIN]`) e que se zera (`Zero`). O PIN de assinatura vai no corpo do formulário em um buffer zerado depois do envio. A **validação do PIN leva o par CPF:PIN em Base64 na query** (é o que a documentação do SIGADOC exige): por isso o erro de rede do `net/http`, que traria a URL inteira, é reduzido à causa, e o log só registra o caminho. Há teste para isso.
- **`Session`:** guarda o token de cada matrícula até faltarem 5 minutos para vencer.
- **`Memory` (simulador):** segue as regras do SIGADOC real (finalizar antes de assinar, assinar antes de autuar ou juntar, juntar só a processo não arquivado), com falhas programáveis, PIN por CPF e relógio controlável. Ligado por `SIGADOC_SIMULATE=true`; recusado em produção. O `cmd/api` liga o simulador, o cliente real (com `SIGADOC_USERNAME`) ou nada.
- **Heurística de "arquivado":** o evento precisa **começar** com "Arquivamento" (e "Desarquivamento" desfaz), para "Juntada de Certidão de Arquivamento…" não contar. Continua sendo dedução (P5).

### F6 — acompanhamento da tramitação (implementada)
- **Migração `00041`:** `project_sigadoc` (processo do projeto: matrícula, número e sigla, imutáveis por trigger; agendamento da próxima consulta, intervalo e último erro) e `sigadoc_movements` (movimentação do SIGADOC, única por `mov_id`, com a descrição do documento e a marca de "depois do arquivamento").
- **Laço em segundo plano** (`RunSigadocWatcher`, a cada minuto): reserva os processos vencidos (`ClaimSigadocWatches`, com `SKIP LOCKED`, seguro com várias instâncias), consulta cada um com o token da matrícula de quem abriu, grava as movimentações novas e agenda a próxima consulta: **15 minutos com novidade; dobra sem novidade, até 6 horas; falha também dobra** e guarda só um texto curto (nunca URL, credencial ou PIN). Termina quando o SIGADOC arquiva.
- **Rotas (só equipe; o proponente recebe 404):** `GET /api/admin/projects/{id}/sigadoc` e `POST .../sigadoc/movements/seen` (auditada), com `openapi.yaml`.
- **Frontend:** painel "Tramitação no SIGADOC" no histórico do projeto e o **aviso chamativo** no projeto arquivado cujo processo segue aberto no SIGADOC, com a lista do que foi acrescentado depois do arquivamento e o botão "Marcar como vistas"; "arquivado nos dois" quando o SIGADOC também arquiva.
- **O que ainda não grava o processo:** `RegisterSigadocProcess` existe, mas só a montagem (F4) vai chamá-lo; nos testes a linha entra por SQL. Enquanto a F4 não existe, nenhum projeto tem processo e o laço não consulta nada.

### F3 e F4 — Termo, PIN e montagem do processo (implementadas contra o simulador)
- **Configuração em `sigadoc_settings`** (parâmetro de regra, editável sem deploy): `termo_model_id` (id numérico do modelo do Termo, **vazio por padrão: a abertura fica indisponível até alguém informá-lo**), `termo_model`, `report_model`/`report_doc_type` (`Interno Capturado`), `document_model`/`document_doc_type` (`Externo Capturado`), `classification`, `access_level`, `destination_lotacao`, `max_part_mb` (24) e `field_map` (troca o nome lógico de cada campo do Termo pelo nome do campo da entrevista). No servidor, `SIGADOC_LOOKUP_MATRICULA` (uma matrícula válida, usada só para achar a matrícula de cada analista pelo CPF).
- **Rotas** (`start-analysis`, só o analista responsável; reaproveitei esta ação em vez de criar `open-sigadoc`, porque quem abre o processo é exatamente quem assume a análise): `POST .../sigadoc/termo` (pré-visualização, nada é gravado), `.../open` (PIN + ajustes; **valida o PIN antes** de criar a tarefa; 202), `.../resume` (PIN de novo) e `.../cancel`. `GET .../sigadoc` traz `task` (itens e andamento) e `opening` (se o botão cabe e por quê).
- **Motor** (`sigadoc_open.go`, `sigadoc_open_items.go`): Termo (criar, finalizar, assinar, autuar e gravar o número do processo) → relatório (interno capturado) → cada arquivo do proponente (Word e imagem convertidos, `pdfbuild.Split` em partes de até `max_part_mb`), cada um criado, finalizado, assinado e **juntado** ao processo. Cada item grava o último passo concluído; **`creating` é marcado antes de chamar o SIGADOC**, e na retomada a descrição padronizada («tipo — proponente ou emenda — código», com "arquivo n de N" e "parte n de N") é procurada (`FindByDescription`) para não duplicar. O cliente real ainda não sabe procurar (`ErrUnsupported`): a tela avisa para conferir duplicidade.
- **Regras da tarefa:** PIN recusado no meio para tudo e **nunca é tentado de novo**; arquivo com problema (corrompido, protegido, página acima do limite) falha só aquele item e a tarefa termina `interrupted` com a lista; o projeto só passa a `awaiting_opinion` quando tudo está juntado; desistência, negação e arquivamento **cancelam** a montagem (o motor confere a etapa antes de cada passo); o take-over fica bloqueado com a montagem em andamento; reinício da API deixa as tarefas `interrupted`; 5 tentativas de PIN por analista a cada 10 minutos (429).
- **PIN:** vive só em `sigadoc.PIN` na memória da tarefa (zerado ao fim); nenhum teste o encontra em auditoria, tarefas, itens ou nas chamadas do simulador. No navegador, o campo é `password`, o estado some ao fechar e a mutação não fica no cache (`gcTime: 0`).
- **Frontend:** assistente "Abrir no SIGADOC" (1. conferir e ajustar o Termo, com a pré-visualização em `iframe` sem permissões; 2. PIN), andamento com barra e itens, retomada e cancelamento.
- **O que só o SIGADOC real vai dizer** (nada disso foi exercitado, as escritas estão bloqueadas): de onde sai o número do processo depois do `autuar` (hoje: a sigla da consulta ao documento autuado; P3), qual sigla de volume o `juntar` espera (hoje: a sigla normalizada do processo), os nomes dos campos da entrevista do Termo, e o formato exato das respostas.

### Pendências de implantação (decidir antes de subir)
- **Projetos que já estão em `awaiting_opinion`** em homologação ou produção (enquadrados antes desta mudança) **continuam como estão**; não passam pela abertura do processo. Se for preciso que passem, é uma migração de dados que precisa também gravar o histórico de etapas.
- O parâmetro de regra `stalled_after_days` não tem chaves para as etapas novas.
- **Antes de usar:** informar `termo_model_id` (e conferir os demais nomes) em `sigadoc_settings`, e definir `SIGADOC_USERNAME`, `SIGADOC_PASSWORD` e `SIGADOC_LOOKUP_MATRICULA` no servidor.
- Teste já quebrado antes desta fase: `TestMigracaoPeriodosDeExecucaoPreservaOsDadosAntigos` (`internal/database`) assume que a 00036 é a última migração (`m.Down` desfaz só a mais recente, hoje a 00040). Falha também na `main`; precisa de um ajuste próprio.

---

## 14. Continuidade: do simulador ao SIGADOC real (24/11/2026)

### 14.1 O que mudou
- **Acesso.** O usuário de sistema passou a ser `<usuário-de-sistema>` (antes `<usuário-de-sistema-antigo>`). `cli sigadoc:check` em 24/11: **16 liberados, 0 bloqueados**, 2 não sondados de propósito (criar documento e validar PIN). Ou seja, modelos, finalizar, assinar, autuar, juntar, ler o processo (`pdf-completo`, `dossie`, `consultar-protocolo`), **`consultar-arquivado`** e `tramitar` deixaram de dar 403.
- **URL.** O endereço do SIGADOC continua sendo `X_VIA_SEDEC_BASE_URL` + `X_VIA_SIGADOC_SERVICE`: o SIGADOC é acessado pelo mesmo X-Via do MT-ID (mesmo certificado e `X_ROAD_CLIENT`), então uma variável própria seria redundante (chegou a ser criada e foi removida). A única mudança é interna: `config.SigadocURL()` monta a URL e `sigadoc.NewHTTP` recebe a URL completa. Só vale uma variável própria se o SIGADOC passar a ficar em outro host ou instância de X-Via. `SIGADOC_SIMULATE=true` continua ligado no `.env` local até a G1.
- **Modelos reais (lidos em `GET /sigaex/modelos`, nada gravado).**

| Uso | Modelo no SIGADOC | `idModelo` | Tipo |
|---|---|---|---|
| Termo de Abertura | `GOV MT: Termo de Abertura de Expediente/Processo no SIGADOC` | **23323** (inicial 3540) | Interno Produzido, espécie TER, acesso "Limitado ao órgão" |
| Relatório do projeto (P1) | `GOV MT: Documento Interno` | **8420** (inicial 3381) | Interno Capturado, espécie DIC |
| Documentos do proponente | `GOV MT: Capturado` | **26113** (inicial 3400) | Externo Capturado, espécie CAP |

  P1 fica **confirmada na prática** (existe o modelo de Interno Capturado); falta confirmar na escrita.
- **Campos reais da entrevista do Termo** (diferem dos nomes lógicos que o código supõe, porque o modelo é de atendimento ao cidadão): `docGovMT_atendimento` (rádio: `presencial` ou `e-mail`), `auxGovMT_interessadoProcurador` (rádio: `interessado` ou `procurador`), `docGovMT_interessado_nome`, `docGovMT_procurador_nome`, `docGovMT_cpf-cnpj`, `docGovMT_telefone`, `docGovMT_email`, `docGovMT_anexoFisico` (caixa) e `docGovMT_anexoFisico_descricao`, `docGovMT_resumoAssunto`, `auxGovMT_deveJuntar` (rádio) e `docGovMT_numProcesso`. Os campos `aux*` comandam a exibição dos outros; confirmar na G1 quais valores aceitam.

### 14.2 Etapas

| Etapa | Entrega | Pronto quando |
|---|---|---|
| **G0** (feita) | `config.SigadocURL()` (teste `TestSigadocURL`) e `sigadoc:check` rodado no real | `go test ./internal/config ./internal/sigadoc` verde e `sigadoc:check` com 16 liberados |
| **G1. Termo real, só leitura** (feita em 24/11/2026) | `TermoFields` reescrito com os nomes reais da entrevista (a instituição é o interessado; contato da instituição, com reserva no proponente); padrões do parâmetro `sigadoc_settings` com os ids reais (23323, 8420, 26113); rótulos do assistente; abrir o processo exige nome, CPF/CNPJ, telefone, e-mail e resumo preenchidos; pré-visualização exercitada contra o SIGADOC real | Todos os campos voltam preenchidos no formulário real (`processar-entrevista`), com acentos corretos. Veja 14.5 |
| **G2. PIN** | Escolher a matrícula e o dono do PIN de teste; exercitar `POST /siga/pin/validar` com PIN certo e errado (uma tentativa errada de cada, sem repetir); confirmar o formato da resposta e a mensagem de bloqueio | Erro de PIN vira a mensagem certa na tela; o limite de 5 tentativas continua valendo |
| **G3. Primeira escrita controlada** | Comando `cli sigadoc:smoke` (novo, só desenvolvimento, pede o PIN no terminal, descrição marcada "TESTE"): **primeiro cria um documento com acentos na entrevista e lê de volta (14.5)**; depois cria o Termo, finaliza, assina, autua, consulta o processo e junta um PDF interno capturado e um externo capturado | Confirma o tratamento de acentos, **P3** (de onde sai o número do processo), a sigla do volume que o `juntar` espera, o formato real das respostas e o tamanho aceito (**P4**). As respostas reais viram fixtures (sem dados pessoais) nos testes do cliente |
| **G4. Cliente sem suposições** | Ajustar `HTTP` ao que a G3 mostrou; implementar `FindByDescription` real (a pesquisa só filtra por marcador, pessoa, lotação e período de até 31 dias: listar os documentos do analista desde o início da tarefa e comparar a descrição); trocar a dedução de arquivamento por `consultar-arquivado` (**P5**, agora liberado), mantendo a dedução como reserva; revisar mensagens de erro | Retomada não duplica documento no SIGADOC real; arquivamento reconhecido sem heurística |
| **G5. Ponta a ponta em dev** | Projeto de teste do cadastro ao `awaiting_opinion` pela interface: assumir, abrir, andamento, falha de propósito (PIN errado no meio, arquivo corrompido), retomar, cancelar; o acompanhamento registra as movimentações. Atualizar [roteiro-aceite-fluxo-equipe.md](roteiro-aceite-fluxo-equipe.md) | Roteiro percorrido sem passo manual no banco; número do processo gravado e conferido no SIGADOC |
| **G6. Hml e produção (F7)** | Pedir à MTI o item 6 da solicitação (endereço, `x-road-client`, certificado e usuário de **produção**); credenciais (e `X_VIA_SEDEC_BASE_URL` de produção) no Coolify (hml) e no Portainer (produção); decidir o destino dos projetos que já estão em `awaiting_opinion`; chaves de `stalled_after_days` das etapas novas; corrigir `TestMigracaoPeriodosDeExecucaoPreservaOsDadosAntigos`; runbook e débitos técnicos | Hml abre um processo de verdade; produção só depois do aceite da SEDEC |

**Ordem e dependências.** G1 e G2 não escrevem nada no SIGADOC e podem andar já. G3 é a primeira escrita: **cria documentos reais no SIGADOC de desenvolvimento** (talvez sem como apagar), então só roda com a MTI ciente e com PIN informado por quem é dono dele. G4 depende da G3; G5 da G4; G6 da G5 e da MTI.

### 14.3 Decisões que dependem da SEDEC
1. **Quem fornece o PIN de teste** e com qual matrícula (hoje `SIGADOC_LOOKUP_MATRICULA=SEDEC123456`): o PIN é do dono da matrícula e errar bloqueia.
2. **Valores do Termo** que não vêm do projeto: `atendimento` (e-mail?), `interessado` ou `procurador` (o proponente é o interessado?), telefone e e-mail (do proponente?), `anexo físico` (não), `deve juntar` (não).
3. **Classificação documental, nível de acesso e lotação de destino** (`GET /sigaex/classificacoes` está liberado agora: dá para listar as opções e a SEDEC escolher).
4. **Aval da MTI** para criar documentos de teste em desenvolvimento e saber como cancelá-los.

### 14.4 Riscos novos
| Risco | Mitigação |
|---|---|
| Os campos do Termo não aceitam os valores esperados e a pré-visualização sai em branco | G1 antes de qualquer escrita; `field_map` editável sem deploy |
| Documentos de teste ficam no SIGADOC | Descrição "TESTE", poucos documentos, MTI avisada |
| PIN bloqueado por tentativa errada na G2 | Uma tentativa errada, de propósito, e nenhuma repetição |

### 14.5 O que a G1 mostrou
- **A "pré-visualização" é o formulário da entrevista preenchido, não o texto do Termo.** `processar-entrevista` devolve o HTML com os campos e os valores; o Termo renderizado só existe depois de criar o documento (G3). A tela continua útil para o analista conferir os valores, mas o título dela não deve prometer "o Termo como ficará".
- **Acentos na entrevista são corrompidos pelo SIGADOC de desenvolvimento.** Testado em 24/11/2026: `Solicitação` volta como `Solicita��o` com UTF-8, Latin-1, `\u` ou multipart; só a referência HTML (`&#231;`) chega íntegra. A pré-visualização agora manda os não-ASCII como referências numéricas (`asciiEntities`, com teste). **A criação do documento não foi mexida**: ainda não se sabe se o mesmo problema acontece ao gravar (nomes como "Associação" são certos). **Isto é a primeira coisa a testar na G3** (criar com acento, ler o documento de volta e ver o texto), e a MTI deve ser avisada se o problema se confirmar.
- **Valores dos rádios.** `auxGovMT_deveJuntar` usa `sim` e `não` (com acento, o que a mesma regra de codificação afeta); `docGovMT_atendimento` usa `presencial` e `e-mail`; `docGovMT_anexoFisico` usa `Nao` e `Sim`. Os padrões escolhidos (atendimento por e-mail, solicitante interessado, sem anexo físico, sem juntar) são suposições a confirmar com a SEDEC (14.3, item 2); o analista os ajusta na tela.
- **Ambientes que já têm `sigadoc_settings` gravado não mudam sozinhos**: `EnsureDefaults` só cria o valor de chaves sem nenhuma versão. Em desenvolvimento, hml e produção, o Supervisor precisa editar o parâmetro (gravando uma nova versão) para pôr os ids reais; os ids de produção ainda precisam ser conferidos com a MTI.
- **Criação do documento (teste de 24/11/2026, sem PIN).** `POST /sigaex/documentos` com o modelo 23323 **funciona** (não é 403), mas exige `classificacao`: sem ela, *"Modelo sem classificação automática, favor informar."* A SEDEC definiu a classificação **`004`**; com ela o documento foi criado como rascunho temporário `TMP-20985645` (matrícula `SEDEC123456`, descrição "TESTE integração - pode apagar"), **sem finalizar nem assinar**. O nível de acesso fica vazio no parâmetro (o modelo já usa "Limitado ao órgão", que é o desejado). O usuário de sistema **não consegue ler o documento de volta** (`GET /documentos/{sigla}` do temporário dá 500 no SIGADOC, `/html` e `/download` dão 403, `prever-documento-pdf` não está implantado), então o resultado dos acentos precisa ser conferido olhando o rascunho na tela do próprio SIGADOC.
- **Acentos ao gravar: confirmado correto.** A SEDEC abriu o rascunho `TMP-20985645` no SIGADOC e os acentos estão certos. O problema de codificação afeta só `processar-entrevista`; a pré-visualização continua mandando entidades (inofensivo) e a criação do documento segue como está.
- **Decisões da SEDEC (24/11/2026):** atendimento **em branco** (o campo é opcional; o modelo só oferece `presencial` ou `e-mail` e o projeto chega pelo sistema); solicitante é a instituição (interessado); sem anexo físico; sem juntar a processo existente (o projeto é autuado, conforme o BPMN); classificação **`004`**; nível de acesso "limitado ao órgão" (padrão do modelo, parâmetro vazio); **lotação de destino vazia**. Valores do projeto no Termo (interessado, CNPJ, telefone, e-mail e resumo) confirmados.
- **Ambiente de desenvolvimento ligado ao SIGADOC real (24/11/2026):** `SIGADOC_SIMULATE=false` no `.env`, API reiniciada ("SIGADOC real ligado" no log) e o parâmetro `sigadoc_settings` do banco de desenvolvimento atualizado por SQL (era o padrão semeado, nunca editado). **Próximo passo: a primeira abertura real pela interface, com o PIN do próprio analista** (G2 e G3 acontecem juntas nesse fluxo).

