# Débitos técnicos do fluxo de análise e enquadramento

O que ficou para trás de propósito (ou por falta de acesso) nas fases 0 a 5 do [planejamento do fluxo](planejamento-fluxo-analise-enquadramento.md), com o motivo, o impacto e uma saída sugerida. Cada item cita a decisão ou a fase de onde veio. O débito do MT-ID continua no seu próprio documento: [debito-tecnico-mt-id.md](debito-tecnico-mt-id.md).

**Prioridade:** *Alta* = deve ser resolvido antes de operar de verdade; *Média* = incomoda ou limita a operação, tem contorno; *Baixa* = melhoria ou risco pequeno e conhecido.

Este documento é para ser atualizado: ao resolver um item, mover para "Resolvidos" com a data e o commit.

---

## Resumo

| # | Débito | Prioridade | Origem |
|---|---|---|---|
| T1 | A limpeza de e-mails já enviados não roda sozinha | **Alta** | Fase 2 |
| T2 | Sem SMTP em produção, e-mails são descartados e ficam como "enviados" | **Alta** | Fase 2 |
| T3 | Sem CI: lint e testes só rodam na máquina de quem desenvolve | **Alta** | Fase 0 |
| T4 | Nenhum teste automatizado de interface | Média | F8 (Fase 5) |
| T24 | As telas da equipe ainda não foram percorridas num navegador | **Alta** | Fase 5 |
| T5 | Ambiente de desenvolvimento sem login de perfis de equipe | Média | Fase 5 |
| T6 | E-mails que falharam de vez só aparecem no log | Média | Fase 2 |
| T7 | Perfil personalizado pode reabrir o acesso da equipe a rascunhos | Média | Fase 1 |
| T8 | Não dá para reatribuir a análise a outro Analista | Média | D16 |
| T9 | Não dá para retirar uma indicação de departamento pendente | Média | D19 |
| T10 | Documento adicional já enviado não pode ser devolvido sozinho | Média | D12 |
| T11 | A equipe não é avisada por e-mail quando entra um projeto novo | Média | Fase 4 |
| T12 | Passo de implantação manual: marcar os departamentos finalísticos | Média | D4 (Fase 1) |
| T13 | Documentos do planejamento fora do controle de versão | Média | Fase 0 |
| T14 | O Analista não é avisado quando o Revisor recusa | Baixa | D20 |
| T15 | Entrega de e-mail "pelo menos uma vez": raramente sai duplicado | Baixa | Fase 2 |
| T16 | E-mail sai até 5 s depois da ação (sem aviso imediato) | Baixa | Fase 2 |
| T17 | "Abrir o documento" prova o pedido do link, não a leitura | Baixa | D14 |
| T18 | A revogação das ações do Supervisor sobrescreve personalizações | Baixa | Fase 1 |
| T19 | Super Admin enxerga rascunhos | Baixa | Fase 1 |
| T20 | O teste de migração não usa dados reais | Baixa | Fase 1 |
| T21 | Problemas anteriores de lint e formato no frontend | Baixa | Fase 0 |
| T22 | Listagem de projetos com subconsultas por linha | Baixa | Fase 4 |
| T25 | O diálogo de devolução não oferece documentos opcionais vazios | Baixa | Fase 5 |
| T26 | Cada aba da fila faz uma consulta só para o contador | Baixa | F5 (Fase 5) |
| T31 | Rascunho abandonado some sem aviso na visão geral do proponente | Baixa | Visão geral do proponente (D8) |
| T32 | Visão geral da equipe ainda é só um texto; o resumo do proponente conta só 5 rascunhos com pendências | Baixa | Visão geral do proponente (D5) |
| T29 | Painel de prazos entre projetos (médias e filas por responsável) | Baixa | Fase 6 do histórico de etapas |
| T30 | Exportação em CSV do painel de prazos | Baixa | Fase 6 do histórico de etapas |
| T23 | Etapas depois de "Aguardando parecer" ainda não existem | Fora do escopo | D1 |

---

## Alta prioridade

### T1. A limpeza de e-mails já enviados não roda sozinha
- **O que ficou:** `outbound_emails` guarda o corpo de cada e-mail (nomes, títulos, valores). As mensagens `sent` e `failed` só são apagadas pelo comando `cleanup`, que hoje é **manual** (`docker compose exec api go run ./cmd/cli cleanup`).
- **Por quê:** o agendamento de rotinas de manutenção nunca foi montado no projeto (o `cleanup` já existia assim para sessões e tokens).
- **Impacto:** dados pessoais ficam guardados por mais tempo que o planejado (LGPD) e a tabela cresce sem limite.
- **Saída:** agendar o `cleanup` (cron/serviço do orquestrador) ou rodá-lo numa goroutine diária dentro da API, como `expireComplements`. Definir o prazo de retenção (hoje o `--days` do comando).

### T2. Sem SMTP em produção, e-mails são descartados e ficam como "enviados"
- **O que ficou:** sem `SMTP_HOST`, a API usa o `NopMailer`, que descarta com um aviso no log e devolve sucesso; o worker então marca a mensagem como `sent`.
- **Por quê:** manteve-se o comportamento anterior (antes não havia fila, e descartar era a regra).
- **Impacto:** com a fila, ninguém percebe que **nenhum e-mail saiu** (todos os avisos do fluxo viram `sent`). Uma configuração esquecida em produção perde todos os avisos de arquivamento, recusa etc.
- **Saída:** com `APP_ENV=production` e sem SMTP, o worker não deve consumir a fila (as mensagens ficam `pending` até o SMTP existir) e a API deve avisar no log ao subir. Hoje a configuração só é conferida em desenvolvimento.

### T3. Sem CI
- **O que ficou:** o repositório não tem pipeline (`.github/` não existe). O lint (`golangci-lint`), os testes do backend e o `typecheck`/lint/formato do frontend só rodam na máquina de quem desenvolve.
- **Por quê:** fora do escopo do fluxo; só descoberto na Fase 0 (quando o `golangci-lint` nem estava instalado).
- **Impacto:** regressões e o descuido com o lint passam despercebidos; a suíte do backend leva uns 4 minutos e depende de Docker.
- **Saída:** pipeline com `go test -race ./...`, `golangci-lint run`, `make cover-authz`, `sqlc generate` sem diferença, e no frontend `typecheck`, `lint` e `format:check` (com a linha de base de T21 até ser limpa).

### T24. As telas da equipe ainda não foram percorridas num navegador
- **O que ficou:** as telas da Fase 5 foram verificadas por `typecheck`, lint e formato sem problema novo, por um `build` de produção do Next e por uma revisão de acessibilidade contra as diretrizes. **Ninguém as abriu.** O login só acontece pelo MT Login real, e os usuários de exemplo do seed não entram.
- **Impacto:** erros de layout, de comportamento (foco, diálogos, atualização depois de uma ação) e regras de exibição só aparecem quando alguém usa.
- **Saída:** percorrer o [roteiro de aceite](roteiro-aceite-fluxo-equipe.md); depois, T5 (login de desenvolvimento por perfil) e T4 (testes automáticos) evitam depender disso.

---

## Média prioridade

### T4. Nenhum teste automatizado de interface
- **O que ficou:** o frontend não tem suíte de testes. As telas da Fase 5 são validadas por `typecheck`, lint, formato e um **roteiro manual** ([planejamento-fase-5.md](planejamento-fase-5.md), seção 8).
- **Por quê:** decisão F8: não introduzir ferramentas de teste sem pedido.
- **Impacto:** um erro de tela só aparece quando alguém usa. Regras espelhadas do servidor (quando mostrar cada botão) podem divergir sem aviso.
- **Saída:** começar por testes de unidade das funções puras (critério "é equipe", habilitação de ações, mapeamento de erros) e, depois, testes de ponta a ponta do roteiro de aceite.

### T5. Ambiente de desenvolvimento sem login de perfis de equipe
- **O que ficou:** os administradores de exemplo do seed têm CPFs fictícios e **não entram pelo MT Login**; só o Super Admin real entra, e ele passa por todas as permissões.
- **Impacto:** não dá para ver a interface como Analista, Revisor, Supervisor ou Secretário sem cadastrar CPFs reais; as restrições de cada perfil só se provam assim.
- **Saída:** um login de desenvolvimento por perfil (como o MT-ID simulado: `MTID_SIMULATE`), ligado só fora de produção, ou um servidor OIDC local (Keycloak em contêiner) com os usuários de exemplo.

### T6. E-mails que falharam de vez só aparecem no log
- **O que ficou:** depois de 8 tentativas a mensagem fica `failed`, com o último erro, e há um registro de erro no log. Nenhuma tela nem alerta mostra isso.
- **Impacto:** um aviso perdido (por exemplo, o de arquivamento) só é percebido se alguém olhar o log.
- **Saída:** tela de administração de e-mails (pendentes e falhos, com reenviar) e/ou alerta quando houver `failed`; contador na tela de saúde da API.

### T7. Perfil personalizado pode reabrir o acesso da equipe a rascunhos
- **O que ficou:** a regra "a equipe não vê rascunho" é feita por condições nas permissões (`admin::project-submitted*`). Quem monta um perfil personalizado com `projects.read` **sem** a condição volta a enxergar rascunhos. Está documentado, e o teste de matriz cobre os perfis padrão, mas nada impede.
- **Por quê:** as condições se combinam por OU no motor; uma regra fixa no serviço mudaria o motor de permissões.
- **Impacto:** vazamento de dados pessoais de rascunhos por configuração.
- **Saída:** invariante no serviço (rascunho só para titular, membros e Super Admin, independente do perfil) ou validação ao salvar um perfil com `projects.read` sem condição de status, com aviso na tela de perfis.

### T8. Não dá para reatribuir a análise a outro Analista
- **O que ficou:** o responsável é quem assumiu primeiro e não muda. Qualquer Analista continua podendo agir no projeto (D13), então nada trava; mas os e-mails de arquivamento vão só ao responsável (D2).
- **Impacto:** se o responsável sair ou ficar indisponível, o aviso de arquivamento cai nos demais só quando ele estiver bloqueado ou inativo.
- **Saída:** ação "Assumir/Transferir análise" para o Analista, ou para um perfil de coordenação, com auditoria.

### T9. Não dá para retirar uma indicação de departamento pendente
- **O que ficou:** só a recusa do Revisor desfaz uma indicação. Se o Analista errou o departamento, ou se o único Revisor ficou indisponível depois da indicação, o projeto espera. O Secretário ainda pode negá-lo.
- **Por quê:** o BPMN prevê só o ciclo recusa → nova indicação.
- **Saída:** `DELETE /department` que encerra a rodada como "retirada" (nova decisão de rodada no modelo), com registro e sem e-mail ao Revisor.

### T10. Documento adicional já enviado não pode ser devolvido sozinho
- **O que ficou:** o arquivo de um adicional enviado fica travado e não entra na lista de itens devolvíveis. O Analista usa a mensagem do pedido de complementação.
- **Impacto:** o proponente precisa adicionar outro documento em vez de corrigir o antigo.
- **Saída:** permitir devolver o adicional (destravar o arquivo na complementação).

### T11. A equipe não é avisada por e-mail quando entra um projeto novo
- **O que ficou:** o envio só confirma ao proponente. Os Analistas descobrem pela fila.
- **Saída:** aviso diário/por envio aos Analistas, ou contador na tela inicial.

### T12. Passo de implantação manual: marcar os departamentos finalísticos
- **O que ficou:** `is_finalistic` nasce `false`. Em produção, até alguém marcar os departamentos reais, o Analista não consegue indicar nenhum.
- **Saída:** documentar no procedimento de implantação (já está nos planos) e, se for recorrente, uma tela/aviso: "Nenhum departamento finalístico cadastrado".

### T13. Documentos do planejamento fora do controle de versão
- **O que ficou:** `docs/bpmn/`, os planejamentos das fases, o relatório da Fase 0 e este arquivo ainda estão **sem commit**; o `README.md` da raiz tem mudanças suas misturadas com as minhas (tabela de perfis).
- **Saída:** commitar os documentos quando você decidir; separar o `README.md`.

---

## Baixa prioridade

### T27. O Gotenberg não está no ambiente de produção
- O visualizador converte Word em PDF pelo Gotenberg (LibreOffice), que roda como serviço à parte (`docker-compose.dev.yml`). A imagem de produção da API é distroless e não carrega o LibreOffice; sem `PREVIEW_CONVERTER_URL`, o Word não é exibido (a tela oferece baixar). **Saída:** subir o serviço onde a API roda, sem porta pública, e definir a variável. O PDF convertido fica em `previews/<id>.pdf` no mesmo bucket.

### T25. O diálogo de devolução não oferece documentos opcionais vazios
- O diálogo lista as seções e os documentos que aparecem na conferência (com arquivo, justificativa ou obrigatório vazio). A API aceita devolver qualquer item do checklist, inclusive um opcional sem arquivo, que a tela não oferece. **Saída:** listar todo o checklist no diálogo.

### T26. Cada aba da fila faz uma consulta só para o contador
- A fila do Analista dispara até 7 consultas pequenas (`pageSize=1`) para os contadores das abas. Serve ao volume previsto; sob carga, um endpoint de contagem por etapa resolveria com uma consulta. (Decisão F5.)

### T31. Rascunho abandonado some sem aviso na visão geral do proponente
- `CleanupAbandonedDrafts` apaga rascunhos sem alteração há mais de N dias, mas N vem do `--draft-days` do comando `cleanup`; **a API não sabe o valor**, então a visão geral não pode dizer "este rascunho será excluído em N dias". Decisão D8 do [planejamento da visão geral](planejamento-visao-geral-proponente.md). **Saída:** variável `DRAFT_RETENTION_DAYS` na configuração, lida pelo `cleanup` (como padrão do `--draft-days`) e pelo resumo, que passaria a mandar `deleteAt` nos itens `draft`. Junto, decidir se o aviso também sai por e-mail.

### T32. Visão geral da equipe e limites do resumo do proponente
- A visão geral da equipe (Analista, Revisor, Secretário, Supervisor) continua sendo o texto de boas-vindas: `GET /admin/projects/overview` e `/activity` olham só o que é da própria pessoa (titular ou membro), de propósito. Para a equipe, o equivalente seria um resumo das filas (já há contagem por aba, T26) e dos prazos (T29).
- No resumo do proponente, só os 5 rascunhos mais recentes trazem o número de pendências que bloqueiam o envio (`blocking`), porque o cálculo roda a validação inteira (D5). O feed não tem paginação (máximo 30) e mostra só 7 tipos de marco; eventos novos (declaração assinada, troca de analista) só entram incluindo a ação em `activityKinds` de propósito. **Saída:** índice `(entity, entity_id, created_at DESC)` em `audit_logs` e cursor no feed se a medição pedir.

### T29. Painel de prazos entre projetos (melhoria futura)
- O [histórico de etapas](planejamento-historico-etapas.md) já guarda, por projeto, em que etapa e com quem ele esteve e por quanto tempo, mas só se enxerga um projeto por vez e, na fila de análise, "há quanto tempo na etapa atual" (opção A da fase 6, feita). Falta olhar entre projetos. **Saída:** uma página nova em Análise com o tempo médio e o pior caso por etapa, a fila e o trecho mais antigo por analista e por departamento, e a lista dos parados. Os índices `(stage, entered_at)` e `(holder_admin_id, entered_at)` já existem para isso; falta a rota de agregação (por exemplo `GET /admin/projects/stats/stage-times`), a tela e os testes.
- **Antes de construir, decidir:** quem vê (Supervisor e Secretário, ou também o Analista, que hoje só enxerga o alcance da permissão dele); o que conta como "parado" (limite único ou por etapa; "aguardando proponente" já tem prazo de 3 dias por regra); e deixar os trechos `source = initial` (estimados pela migração) **fora das médias**. Só vale a pena depois de a equipe usar a opção A e dizer o que quer medir: definir "tempo médio" cedo demais costuma gerar métrica que ninguém usa.

### T30. Exportação em CSV do painel de prazos (melhoria futura)
- Depende de T29. O mesmo conteúdo do painel em CSV, para planilha ou relatório oficial. **Saída:** rota de exportação com o mesmo filtro e a mesma permissão do painel; durações em segundos corridos (a planilha formata como quiser), sem CPF nem outro dado pessoal, só nome do analista e do departamento. Para prazo com valor legal, usar apenas trechos `live`, não os estimados.

### T14. O Analista não é avisado quando o Revisor recusa
- Decisão D20: o BPMN envia só ao Supervisor; o Analista vê o projeto de volta na fila, com o histórico. **Saída:** e-mail ao analista responsável, se a fila se mostrar pouco visível.

### T15. Entrega de e-mail "pelo menos uma vez"
- Numa queda entre enviar e marcar, uma mensagem pode sair duas vezes. A chave de deduplicação impede duplicar a **criação**, não esse caso. **Saída:** identificador de mensagem no SMTP (se o servidor o suportar) ou aceitar o risco.

### T16. E-mail sai até 5 s depois da ação
- O worker varre a fila a cada 5 s; não há aviso imediato ao gravar. **Saída:** canal de acordar o worker depois do commit, se a latência incomodar.

### T17. "Lido até o fim" prova que a rolagem chegou ao fim, não a leitura
- Aprovar exige chegar ao fim de cada arquivo (`POST .../read`) e reprovar, ver a primeira página (`POST .../viewed`). Rolar até o fim não prova que a pessoa leu o meio (um arrasto da barra de rolagem basta). **Saída:** só se a norma exigir prova de leitura (por exemplo, tempo mínimo por página).

### T18. A revogação das ações do Supervisor sobrescreve personalizações
- A migração 00024 tira `request-complement`, `archive` e `deny` do perfil de sistema Supervisor Administrador, mesmo que alguém as tenha mantido de propósito. Só as três ações, só desse perfil. **Saída:** nenhuma; comunicar antes de aplicar.

### T19. Super Admin enxerga rascunhos
- O Super Admin é curinga e vê tudo, inclusive rascunhos de outros usuários. **Saída:** decidir se isso é aceitável; relaciona-se com T7.

### T20. O teste de migração não usa dados reais
- `TestMigracoesEConsultas` desfaz **uma** migração (a última) e refaz. Não há teste que suba até a migração N-1, popule dados e aplique a N. **Saída:** teste com `goose.UpTo` e dados semeados, principalmente para as migrações que alteram permissões (00024, 00026, 00027).

### T21. Problemas anteriores de lint e formato no frontend
- Anteriores ao fluxo (linha de base da Fase 0): 1 erro de lint (`react(set-state-in-effect)` em `date-input.tsx:80`), 12 avisos e 5 arquivos fora do formato do `oxfmt`. As fases novas só prometem "nada novo". **Saída:** limpar de uma vez, antes de ligar o `format:check` e o `lint` no CI (T3).

### T22. Listagem de projetos com subconsultas por linha
- A listagem busca nome do departamento e do analista por subconsulta. Para o volume previsto serve; sem índice em `analyst_id`. **Saída:** índice e junção se a medição pedir.

---

## Fora do escopo (fases futuras)

### T23. Etapas depois de "Aguardando parecer"
- O fluxo termina em "Projeto enquadrado" (D1). Parecer técnico (deferido/indeferido), formalização e prestação de contas não existem. O Secretário nega e o proponente desiste em qualquer etapa, **inclusive depois do enquadramento**, e isso já funciona. As etapas "Parado" e "Concluído" do planejamento original também ficam para depois.

---

## Resolvidos

| # | O que era | Resolvido em |
|---|---|---|
| T28 | O visualizador abria o PDF sem os arquivos auxiliares do pdf.js: páginas de scanner (imagem CCITT/JBIG2, como o "Plano de Trabalho e anexos do SIGCon") saíam **em branco**, e PDFs sem fonte embutida usavam fonte substituta | 31/10/2026: `wasmUrl`, `cMapUrl` e `standardFontDataUrl` apontam para `public/pdfjs`, copiado do pacote por `scripts/copy-pdfjs-assets.mjs` antes do dev e do build |
| — | A API não esperava o envio de e-mail em andamento terminar ao desligar (só os tokens de API): a mensagem podia sair e a marcação como enviada se perder, reenviando depois | 31/10/2026, commit `c20195a` (`App.Wait`, chamado em `cmd/api/main.go`, espera até 20 s) |
