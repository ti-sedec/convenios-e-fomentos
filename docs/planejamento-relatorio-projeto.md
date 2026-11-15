# Planejamento — Relatório do projeto (resumido e completo)

**Objetivo.** Depois que o enquadramento é confirmado, permitir gerar um PDF com as informações cadastradas no projeto, em duas versões:

- **Resumido:** só as informações do cadastro (sem os documentos anexados).
- **Completo:** as mesmas informações **mais** os documentos do projeto, juntados no mesmo PDF. O relatório e **cada documento** têm um **marcador (bookmark)** e um **hiperlink** no sumário.

Este documento diz o que construir. Não contém código. O relatório **resumido** é o "Relatório contendo as informações cadastradas no projeto, com exceção dos documentos" que a etapa do SIGADOC vai precisar (ver [bpmn/convenios-e-fomento.bpmn](bpmn/convenios-e-fomento.bpmn), que continua o fluxo depois do enquadramento); por isso o PDF gerado é guardado e reproduzível.

---

## 1. Como é hoje (o que dá para reaproveitar)

- **Já existe um PDF gerado pelo servidor:** o comprovante de recebimento (`receipt.go`, biblioteca `go-pdf/fpdf`, fonte Helvetica, A4). Ele é montado a partir do **retrato imutável** da versão enviada (`project_versions.snapshot`: projeto, checklist de documentos, regras, declaração). Rota: `GET /api/admin/projects/{id}/receipt`, ação `admin::projects.read`, com auditoria `project.receipt-download`. O proponente e a equipe usam a mesma rota.
- **Conversão de Word para PDF já existe:** `internal/preview` (Gotenberg/LibreOffice, serviço isolado) e o cache do PDF convertido em `previews/{id}.pdf` no armazenamento (`convertedKey`, com `singleflight`). Limite de conversão: 50 MB por arquivo.
- **Armazenamento (S3/MinIO):** a interface `storage.Store` já tem `Get`, `PutObject`, `Stat` e `PresignDownload`. Os arquivos aceitos hoje: PDF e DOCX (até 200 MB), PNG, JPEG e WebP (até 25 MB), no máximo 300 arquivos por projeto.
- **Não existe ainda:** juntar PDFs, bookmarks, links internos, imagem para PDF, nem tarefa em segundo plano para PDF (só a caixa de saída de e-mails, `internal/outbox`, que é um worker que lê uma tabela).
- **"Enquadrado"** é a decisão da última rodada de enquadramento (`framing.decision = confirmed`). Hoje o estágio fica `awaiting_opinion`; no novo fluxo (retorno à fila dos analistas) o estágio muda, mas a decisão continua `confirmed`. **A regra tem de olhar a decisão, não o estágio.**

## 2. Decisões

### 2.1 Confirmadas

| # | Decisão |
|---|---|
| D1 | **Quem gera:** a equipe **e também o proponente**. Vale para quem já consegue ler o projeto (a visibilidade de hoje não muda). |
| D2 | **Entrega:** a versão **resumida é síncrona** (download direto). A **completa é assíncrona**: cria uma tarefa com status, o PDF é guardado no armazenamento e baixado quando ficar pronto. |
| D3 | **Quais projetos e quais dados:** enquadramento confirmado **e** projeto ativo (não arquivado). Os dados vêm do **retrato da última versão enviada**, como no comprovante. |
| D4 | **Arquivo que não entra no PDF** (PDF protegido ou corrompido, Word que o Gotenberg recusa, tamanho acima do limite): no lugar do documento entra uma **página de aviso** (com o bookmark e o link) e o aviso é listado no resultado da tarefa. A geração não aborta. |
| D5 | **Sem política de retenção.** O relatório que for usado no SIGADOC fica guardado lá. Aqui **não há limpeza por prazo** do histórico de gerações. |
| D6 | **Histórico de quem gerou.** Cada geração registra quem gerou (nome e CPF), quando, o tipo, as opções escolhidas, a versão do projeto e **as informações que foram usadas no relatório naquele momento** (os dados das seções, **sem** os documentos anexados). Vale para o resumido e para o completo. |
| D7 | **Membros por CPF** (quem recebeu acesso ao projeto) também geram o relatório. |
| D8 | **Opções na geração, as duas marcadas por padrão:** (a) **Folha de rosto** antes de cada documento (só no completo); (b) **Numeração das páginas**. Quando a numeração está marcada, ela é **contínua em todo o PDF, inclusive nos documentos anexados**. |
| D9 | **A data de geração** aparece na identificação do relatório e no rodapé das páginas do relatório. |
| D10 | **Não entram** no completo outras peças além do relatório e dos documentos do checklist (o comprovante de recebimento fica de fora). |

### 2.2 Premissas adotadas (me diga se discorda)

| # | Premissa |
|---|---|
| P1 | **Uma permissão nova,** `admin::projects.report` ("Gerar relatório do projeto"), com condições como as outras ações de projeto. É concedida por perfil (Analista, Revisor, Supervisor, Secretário, Proponente). A seed e a migração de perfis incluem a ação. A leitura sozinha **não** basta, para o Super Admin poder desligar o recurso por perfil. |
| P2 | **Projeto compartilhado por CPF** (membros): quem tem acesso de leitura ao projeto e o perfil com a ação também gera. Não há regra especial para membros. |
| P3 | **Os dois relatórios usam a mesma regra de elegibilidade** e o servidor devolve no próprio projeto se pode gerar e por que não (`report: {available, reason}`), para a tela não repetir a regra. |
| P4 | **O resumido também é guardado.** Mesmo sendo síncrono, o PDF vai para o armazenamento e ganha registro (data, versão, tamanho, hash SHA-256). Assim o mesmo PDF pode ser capturado no SIGADOC depois e a tela mostra "relatórios gerados". |
| P5 | **O que fica guardado (D5, D6):** o **registro da geração** (metadados, opções e os dados usados) fica sem prazo, porque é pequeno. O **PDF** do relatório completo pode chegar a centenas de MB; ele precisa ficar no armazenamento ao menos até o download. Como o resumido é reproduzível a partir dos dados guardados, a decisão sobre manter ou não o arquivo PDF antigo fica para a fase 2, e **nada é apagado automaticamente** enquanto você não decidir. Gerar de novo cria um registro novo (não sobrescreve). |
| P6 | **Folha de rosto (D8):** quando marcada, uma página antes de cada documento com nome do documento, emenda (se houver), nome do arquivo, tamanho, data de envio e SHA-256. O bookmark e o link do sumário apontam para a **folha de rosto** (que é a primeira página do documento no PDF final). Sem a opção marcada, apontam para a primeira página do arquivo. Com mais de um arquivo no documento, há uma folha por arquivo. |
| P7 | **Ordem dos documentos:** a mesma do checklist (grupo e ordem do catálogo; emendas agrupadas pelo rótulo da emenda), depois os documentos adicionais, na ordem de cadastro. Itens só com justificativa **não têm página própria**: a justificativa aparece no relatório. |
| P8 | **Só entram arquivos confirmados da versão do retrato** (o que foi enviado), não arquivos novos ainda não enviados. |
| P9 | **Rodapé das páginas do relatório:** "Projeto CÓDIGO — Relatório (resumido/completo) — gerado em DD/MM/AAAA HH:MM por NOME". A numeração, quando marcada (D8), é um carimbo "Página X de Y" pequeno no canto inferior de **todas** as páginas, inclusive as dos documentos anexados (o único acréscimo nas páginas de terceiros; o conteúdo não é alterado). Se o estudo mostrar que o carimbo encobre texto de algum documento, ele vai para uma margem que o documento não usa ou a numeração é feita só nas páginas geradas pelo sistema (a decidir com você). |
| P10 | **Sem marca d'água e sem assinatura digital** neste momento. O hash fica no registro, não dentro do PDF. |

## 3. Conteúdo do relatório

Seções, na ordem. A fonte de cada uma é o retrato da versão (as mesmas do comprovante, ampliadas):

| Seção | Conteúdo |
|---|---|
| Capa/identificação | Código, instrumento (Convênio ou Termo de Fomento), nº no SIGCon, versão do projeto, enquadramento (departamento e data da confirmação), tipo do relatório, data/hora (Cuiabá) e quem gerou |
| Proponente | Nome, cargo, representante legal (se não for o próprio) |
| Instituição | Nome, CNPJ, endereço, Certidão de Habilitação |
| Proposta | Título, objeto, vigência, períodos de execução |
| Recursos | Tipo de recurso, recurso próprio, **cada emenda** (parlamentar, número/ano, valor), totais, contrapartida, custos indiretos |
| Declaração de veracidade | Texto, quem aceitou, quando e o IP |
| Documentos | **Resumido:** lista com situação de cada item (entregue, justificado, quantidade de arquivos), nome do arquivo e SHA-256. **Completo:** a mesma lista como **sumário com link e número de página** de cada documento |
| Justificativas | O texto das justificativas de não envio |

Se houver o projeto enviado com atraso depois da data-limite de protocolo, o aviso do comprovante aparece também aqui.

## 4. Como o PDF completo é montado

Ordem das páginas: **relatório (inclui o sumário)** → **documentos**, um após o outro.

1. **Preparar cada arquivo como PDF**
   - PDF: usado como está, depois de validado (abre? tem senha? quantas páginas?).
   - Word: convertido pelo Gotenberg, aproveitando o cache `previews/…`.
   - Imagem (PNG/JPEG/WebP): vira uma página A4 (ou várias, se for grande), escalada para caber nas margens e mantendo a proporção.
   - Qualquer falha (D4): página de aviso com o nome do arquivo e o motivo.
2. **Contar as páginas de cada documento.** Com isso se sabe em que página do PDF final cada documento começa (o relatório vem antes, então o tamanho do relatório precisa ser conhecido).
3. **Gerar o relatório** em duas passagens: a primeira só descobre quantas páginas ele tem (o sumário tem tamanho previsível), a segunda grava os **números de página e os links** do sumário.
4. **Juntar tudo** e gravar a **árvore de bookmarks**:

```
Relatório do projeto
  ├ Identificação
  ├ Proponente
  ├ Instituição
  ├ Proposta
  ├ Recursos
  ├ Declaração
  └ Documentos (sumário)
Documentos
  ├ <Grupo> › <Documento> (<Emenda, se houver>)
  │     └ <arquivo>.pdf          (um por arquivo, quando o documento tem mais de um)
  ├ …
  └ Documentos adicionais › <título>
```

5. **Verificar** o PDF final: abre, tem o número de páginas esperado e todos os bookmarks e links apontam para páginas existentes. Só então grava no armazenamento.

**Hiperlinks.** No sumário, cada linha leva à primeira página do documento (a folha de rosto, quando marcada). A volta ao sumário é pelo painel de bookmarks. No relatório **resumido** não há links (não há o que apontar), só os bookmarks das seções.

**Cuidados com arquivos de terceiros.** O PDF enviado pelo proponente é conteúdo não confiável. Ao juntar, não pode carregar para o PDF final: JavaScript, ações de abrir arquivo/programa, anexos embutidos e links externos de formulário. Isso é um item do estudo (fase 0) e uma regra a testar.

**Limites** (a confirmar no estudo): soma dos arquivos de entrada, total de páginas e tempo máximo da tarefa. Estourar um limite falha a tarefa com mensagem clara ("o conjunto de arquivos é grande demais para um único PDF"), e o resumido continua disponível. A montagem usa **arquivos temporários em disco**, nunca o conjunto inteiro na memória (um projeto pode ter até 300 arquivos de até 200 MB).

## 5. Onde fica na tela

Dois lugares, ambos só aparecem para quem tem a ação `admin::projects.report`:

1. **Equipe — página do projeto** (`staff-project.tsx`): no cabeçalho de ações, um botão **"Relatório"** com menu:
   - *Resumido (só as informações)* → abre um quadro com a opção **Numerar as páginas** (marcada) e baixa.
   - *Completo (com os documentos)* → abre um quadro com **Folha de rosto antes de cada documento** e **Numerar as páginas** (as duas marcadas), cria a tarefa e mostra o andamento.
2. **Proponente — projeto enviado** (`review-step.tsx`, ao lado de "Baixar comprovante"): o mesmo menu, só depois do enquadramento.

Abaixo das ações, um painel **"Relatórios gerados"** (equipe e proponente) lista as gerações do projeto: tipo, versão, data, quem gerou, tamanho e situação (Na fila, Gerando n de N, Pronto, Falhou, Com avisos), com baixar. Enquanto há uma tarefa em andamento, a tela atualiza sozinha (polling) e a pessoa pode sair da página.

Enquanto o projeto não pode gerar, o botão fica **desabilitado com o motivo** ("O relatório fica disponível depois que o Revisor do Departamento confirma o enquadramento.").

Segue as convenções do frontend: tipos/serviço/queries em `features/projects/api`, `useCan()`, ícones por `@/components/icons`, download por `apiClient`/BFF. Não há upload nem leitura de token no navegador.

## 6. Backend

### 6.1 Rotas (`/api/admin`, ação `admin::projects.report`, atualizar o `openapi.yaml`)

| Rota | O que faz |
|---|---|
| `POST /projects/{id}/reports` `{kind: "summary"\|"full", coverSheets: true, pageNumbers: true}` (as duas opções valem `true` se omitidas; `coverSheets` não se aplica ao resumido) | Resumido: gera, guarda e devolve o registro (já `done`). Completo: cria a tarefa e devolve `202` com o registro (`queued`). 409 `framing_not_confirmed` se não é elegível. |
| `GET /projects/{id}/reports` | Lista as gerações (mais recentes primeiro) |
| `GET /projects/{id}/reports/{rid}` | Situação, andamento (n de N documentos) e avisos |
| `GET /projects/{id}/reports/{rid}/download` | Devolve a URL temporária pré-assinada do PDF (como os outros arquivos) |

Alternativa mais simples para o resumido: um `GET` que devolve o PDF direto, como o comprovante. Adoto o `POST` para o resumido também ter registro e hash (P4).

### 6.2 Dados

Nova migração (próxima: `00038_…`) com a tabela **`project_reports`**: id, projeto, número da versão usada, tipo (`summary`/`full`), **opções** (folha de rosto, numeração), situação (`queued`, `running`, `done`, `failed`), **quem gerou** (id, nome e CPF no momento da geração), datas (pedido, início, fim), chave do objeto no armazenamento, tamanho, número de páginas, SHA-256, andamento (feitos/total), avisos (lista de arquivo + motivo), erro e **`data`: os dados do relatório naquele momento (JSON das seções, sem os documentos anexados)**. Índices por projeto e por situação. Sem exclusão por prazo (D5). Todo acesso fica também na auditoria (§6.3).

O painel "Relatórios gerados" mostra o histórico (quem, quando, tipo, opções e versão) e permite **ver os dados usados** em cada geração.

### 6.3 Execução da versão completa

- **Fila no banco, como a caixa de saída:** um worker lê as tarefas `queued` com `FOR UPDATE SKIP LOCKED`, marca `running` com batimento (heartbeat), e uma tarefa parada além do prazo volta a `queued` (ou `failed` depois de N tentativas). Isso sobrevive a reinício da API e a mais de uma instância.
- **Concorrência baixa** (1 ou 2 montagens por vez) e um limite por projeto (não gerar o mesmo tipo e versão duas vezes ao mesmo tempo: devolve a tarefa em andamento).
- O worker roda no processo da API ou em processo próprio (como o `cleanup`, que já é um contêiner à parte na produção). Decidir no estudo, conforme o consumo de memória. Falta conferir onde o worker de e-mails é iniciado hoje, para seguir o mesmo padrão.
- **Autorização:** conferida ao criar a tarefa e de novo ao listar e baixar (o registro pertence ao projeto da rota, e a leitura cobre o projeto). 404 em vez de 403, como nas outras rotas.
- **Auditoria:** `project.report-generate` (tipo, versão) e `project.report-download`. O histórico de etapas não muda.

### 6.4 Código

- Novo arquivo de renderização do relatório (`report.go`), usando a mesma base do comprovante. As funções de seção (`section`, `line`) e os formatadores (`brl`, `brDate`, `formatCNPJ`) são extraídos para serem usados pelos dois. O comprovante **não muda de aparência**.
- Novo pacote de montagem de PDF (`internal/pdfbuild`, interface pequena: juntar, contar páginas, validar, gravar bookmarks), isolando a biblioteca escolhida. Assim os testes usam um duplo e a biblioteca pode trocar.
- Imagem para PDF com o próprio fpdf.
- `Service` ganha o conversor e o armazenamento que já tem; nada novo de infraestrutura além do worker.

## 7. Fases

| Fase | Entrega | Pronto quando |
|---|---|---|
| **0 — Estudo** | **Concluída** (resultados em §10). Biblioteca escolhida: `pdfcpu` v0.16.4. | Decisão e limites registrados |
| **1 — Relatório resumido** (**implementada**, ver §11) | Regra de elegibilidade, ação `admin::projects.report` (+ seed/perfis), tabela, renderização, `POST/GET` do resumido, botão e painel "Relatórios gerados" para equipe e proponente | Equipe e proponente baixam o resumido de um projeto enquadrado; projeto não enquadrado ou arquivado recebe o 409 e o botão vem desabilitado com o motivo |
| **2 — Relatório completo** (**implementada**, ver §12) | Worker/fila, preparação dos arquivos (PDF, Word, imagem, aviso), sumário com links, bookmarks, verificação final, andamento na tela | O PDF completo abre com o painel de bookmarks e o sumário leva a cada documento; arquivo defeituoso vira página de aviso e aparece na lista de avisos |
| **3 — Acabamento** | `openapi.yaml`, textos, `docs/` e roteiro de aceite | Teste de aceite com um projeto real de dezenas de arquivos |

**Testes.** Unitários: renderização do texto (`SetCompression(false)` já permite extrair o texto), numeração das páginas e do sumário, regra de elegibilidade. Integração (`internal/app`, Postgres por testcontainers e armazenamento em memória): permissões por perfil, 409 antes do enquadramento, tarefa de ponta a ponta com PDFs de exemplo (inclusive protegido, corrompido, imagem e Word com conversor falso), tarefa retomada depois de "morrer", download só do próprio projeto. Verificação do PDF final: ler a árvore de bookmarks e os destinos dos links e comparar com a lista esperada.

## 8. Riscos

| Risco | Tratamento |
|---|---|
| Biblioteca de PDF não preservar os links do relatório ao juntar | **Resolvido na Fase 0:** os links são gravados depois de juntar (§10.2) |
| Memória/tempo com muitos arquivos grandes | Arquivos temporários, concorrência baixa, limites claros e falha com mensagem |
| PDF malicioso de terceiros entrar no relatório | **Confirmado na Fase 0** que a junção leva JavaScript, `Launch` e anexos para o PDF final; saneamento por arquivo e verificação final (§10.2) |
| Gotenberg fora do ar | Página de aviso para os Word (D4); o PDF sai mesmo assim |
| Relatório divergir do que foi enviado | Usa só o retrato da versão (D3) e guarda o hash |
| Proponente com dados que a equipe não vê (ou o contrário) | O relatório é o mesmo para todos; ele só traz o que o proponente e a equipe já veem no projeto |

## 9. Pontos em aberto

Respondidos e incorporados (D5 a D10): retenção, membros por CPF, folha de rosto, outras peças e numeração.

Ainda abertos (nenhum bloqueia a Fase 1):

1. **Numeração nas páginas de terceiros (P9):** o carimbo cabe nos formatos testados, mas pode ficar sobre conteúdo que um documento tenha no canto inferior direito (§10.3). Decidir o plano B se acontecer.
2. **PDF antigo guardado (P5):** manter ou não o arquivo dos relatórios completos antigos (o registro e os dados usados ficam sempre).
3. **Links externos dentro dos documentos (§10.4):** manter os links `http(s)` dos PDFs enviados (padrão proposto) ou remover todos.
4. **Limites do conjunto (§10.6):** confirmar os valores propostos e a memória do contêiner da API.

## 10. Resultado da Fase 0 (estudo técnico)

Feito num projeto isolado, fora do repositório, com PDFs de teste (retrato, paisagem, A3, Letter, página com `/Rotate 90`, com senha, truncado, falso e hostil), imagens (PNG, JPEG, WebP, foto com orientação EXIF), um Word, o Gotenberg real do ambiente de dev e 300 arquivos. Os PDFs finais foram conferidos por **dois leitores independentes do que os gerou** (`pypdf` e `poppler`): 0 erros e 0 avisos.

### 10.1 Decisão

| Tarefa | Ferramenta |
|---|---|
| Juntar, contar páginas, validar, adicionar links, carimbar a numeração, sanear | **`pdfcpu` v0.16.4** (Go puro, Apache-2.0) |
| Gerar relatório, sumário, folha de rosto, página de aviso, imagem para PDF | `fpdf` (já usado no comprovante) |
| Word para PDF | Gotenberg (já usado na visualização) |
| Bookmarks | **Código próprio** sobre a API de baixo nível do `pdfcpu` (ver 10.2) |

O endpoint de junção do Gotenberg foi **descartado**: mantém os links, mas **perde os bookmarks**.

### 10.2 O que o estudo mudou no desenho

1. **Os links do sumário são gravados depois de juntar**, com `AddAnnotationsMap`, já com as páginas finais. Não é preciso que links do `fpdf` sobrevivam à junção. Links e bookmarks de todos os testes apontaram para a página certa, inclusive com 300 documentos (os links ocuparam 7 páginas de sumário e bateram um a um com os bookmarks).
2. **Bookmarks com destino direto, não a função pronta do `pdfcpu`.** A função `AddBookmarks` grava cada bookmark como *destino nomeado* cujo nome é o título. Com o título "Ofício (Letter)" um leitor estrito não resolveu o destino (o `í` gera os bytes `C3 AD`, indefinidos na codificação de textos do PDF). Como "Ofício", "Declaração" e "Certidão" são títulos comuns, o código próprio grava `[página /Fit]` direto e o título em UTF-16. Com isso, acentos e títulos repetidos funcionaram.
3. **Sanear cada arquivo antes de juntar é obrigatório.** Juntar sem sanear **levou para o PDF final** o link com JavaScript, o link que abre programa (`Launch`), as ações da página (`/AA`) e, pela mescla das árvores de nomes, o JavaScript nomeado e os **anexos embutidos**. O saneamento removeu todos. Regras: apagar `OpenAction`, `AA` (catálogo, página e anotações), as árvores de nomes `JavaScript` e `EmbeddedFiles` (é preciso apagar do cache `ctx.Names`, senão o `pdfcpu` as reescreve), e as anotações que não sejam de marcação, link ou formulário. Link sem ação segura é removido. Depois da junção, uma **verificação final** lista o conteúdo ativo e a tarefa falha se sobrar algo além de link interno e URI.
4. **O relatório em duas passagens funciona:** o número de páginas não mudou entre a primeira e a segunda passagem.

### 10.3 Numeração das páginas (D8)

O carimbo `Página X de Y` (Helvetica 8 pt, canto inferior direito, 20 pt da lateral e 12 pt da base, sem contorno) saiu certo em **todas** as páginas: relatório, folhas de rosto e documentos, em A4 retrato, paisagem, A3, Letter e com `/Rotate 90`, em que ele acompanha a orientação visual.

Limite conhecido: se um documento já tem texto nessa faixa, os dois se sobrepõem. No teste, um rodapé a 4 mm da borda, escrito de propósito, colidiu quando o texto chegou ao carimbo. Não há como detectar isso barato. Mantenho a regra da premissa P9: o carimbo é o único acréscimo nas páginas de terceiros e fica no canto menos usado. Se ocorrer, o plano B é numerar só as páginas geradas pelo sistema.

### 10.4 Links externos

Os links `http(s)` dentro dos documentos (por exemplo "verifique a autenticidade" nas certidões) **não** são perigosos como o `Launch`, mas podem levar a um endereço enganoso. Proposta: **manter**, porque são úteis. A alternativa é remover todos. Decisão sua (§9, item 3).

### 10.5 Arquivos que não entram (D4)

| Entrada | Detecção | Motivo na página de aviso |
|---|---|---|
| PDF com senha | erro de abertura, "senha" | "O arquivo está protegido por senha" |
| PDF truncado | erro de leitura | "O arquivo está corrompido" |
| Arquivo falso com cabeçalho `%PDF-` | "sem catálogo raiz" | "O arquivo não é um PDF válido" |
| Imagem ilegível ou acima de 60 milhões de pixels | erro de decodificação | "A imagem não pôde ser lida / é grande demais" |
| Word recusado pelo Gotenberg | erro de conversão | "O documento não pôde ser convertido" |

O PDF hostil **passa** na validação (é um PDF válido), por isso o saneamento é uma etapa à parte.

### 10.6 Imagens, Word e limites

- **Imagem:** o `pdfcpu` não trata a orientação EXIF (foto de celular sairia deitada). Código próprio: lê a orientação, gira, converte WebP, tira a transparência (fundo branco), gera uma página A4 em retrato ou paisagem conforme a imagem e escala para caber nas margens. Foto de 6000 × 4500: 0,5 s e 180 MB de pico.
- **Word:** o Gotenberg converteu em 1,3 s, com acentuação correta. A página mantém o tamanho do documento original (o meu teste saiu em Letter).
- **Escala medida:**

| Cenário | Páginas | Tempo | Pico de memória | Tamanho final |
|---|---|---|---|---|
| 300 arquivos pequenos com folha de rosto | 1.058 | 0,7 s | 54 MB | 1 MB |
| os mesmos 300 com dois PDFs de 91 MB | 1.091 | 8,7 s | 397 MB | 184 MB |

  O pico fica em cerca de 2 vezes o tamanho dos arquivos, porque cada etapa (juntar, bookmarks, links, numeração) lê e regrava o PDF inteiro. Dá para fundir as etapas em uma só leitura e escrita se for preciso economizar.

- **Limites propostos** (a confirmar): soma dos arquivos de entrada de até **1 GB**, até **5.000 páginas**, tempo máximo de **15 minutos** por tarefa e **uma montagem por vez**. Isso pede cerca de 2 GB de memória livre na API durante a montagem de um conjunto no limite. Preciso conferir o limite de memória do contêiner da API em produção (§9, item 4).

### 10.7 O que fica para as fases 1 e 2

Fase 1 (resumido) não depende de nada disso além do `fpdf`. A Fase 2 reimplementa o estudo como o pacote `internal/pdfbuild` (juntar, validar, sanear, bookmarks, links, numeração, imagem para PDF), com os testes que já existem como casos: os arquivos hostis, os acentos, os 300 arquivos e a verificação final.

## 11. Fase 1 — o que foi feito

Branch `feat/project-report` (nada commitado). Ficou como o planejado, com estes ajustes:

- **PDF não é guardado (D5).** O registro (`project_reports`) guarda os **dados usados** (`data`, com CPFs mascarados e sem os documentos) e o SHA-256 do PDF. O download do histórico **refaz o PDF a partir do dado**, e sai idêntico byte a byte (data de criação do PDF fixada no instante da geração; teste confere).
- **Dados do retrato, nunca do cadastro vivo (D3).** `buildReportData` lê só o `VersionSnapshot` da última versão enviada. Os nomes de município e de tipo de entidade (o retrato guarda só códigos) são resolvidos na geração e gravados já como texto.
- **Rotas:** `POST /reports` (devolve o PDF, id no cabeçalho `X-Report-Id`), `GET /reports` (histórico), `GET /reports/{rid}` (com os dados usados), `GET /reports/{rid}/download`. `kind: "full"` responde 422 até a Fase 2. Nomes em camelCase, como o resto da API.
- **Permissão** `admin::projects.report` (migração 00038 e seed): titular e membros, Analista, Supervisor, Secretário e Revisor do mesmo departamento. Quem lê mas não tem a ação recebe 403; sem alcance, 404. O Revisor sem departamento recebe 403 em tudo, como na leitura.
- **Elegibilidade no projeto:** `report: {available, reason}` (ausente em rascunho). Enquadramento não confirmado ou projeto arquivado: 409 `report_unavailable` com o motivo.
- **Rodapé:** "Projeto CÓDIGO — Relatório resumido — gerado em DD/MM/AAAA HH:MM (Cuiabá)" e, se marcado, "Página X de Y". O nome de quem gerou aparece na identificação do relatório e no histórico (ajuste de P9: o rodapé não repete o nome).
- **Tela:** cartão "Relatório do projeto" na página da equipe (`staff-project.tsx`) e no projeto enviado do proponente (`review-step.tsx`), com botão desabilitado e o motivo quando não pode, diálogo com "Numerar as páginas" (marcado) e o completo desabilitado ("Em breve"), e a lista "Relatórios gerados" com "Baixar de novo".
- **Não feito de propósito:** o comprovante não foi refatorado para dividir funções com o relatório (o risco de mudar o comprovante não compensou nesta fase); a visualização dos "dados usados" na tela (a API já devolve; o PDF refeito mostra o mesmo conteúdo).

## 12. Fase 2 — relatório completo, rodando na própria API (com medição)

O usuário pediu para tentar primeiro **dentro da API** (sem contêiner próprio) e medir a RAM. Foi feito assim: fila no banco, um worker em segundo plano na API (uma montagem por vez), arquivos temporários em disco, `internal/pdfbuild` (juntar, sanear, bookmarks, links, numeração, imagem para PDF) e a tela com andamento. Cada montagem grava o **pico de memória do processo da API** (`stats`), e a tela o mostra.

### 12.1 Medições (dev, Docker, arquivos reais no MinIO)

Pico de RAM do **processo da API** durante a montagem. "Sem limite" é o contêiner de dev padrão; "512 MB" é o de produção (`mem_limit` 512m e `GOMEMLIMIT=400MiB`). A API em repouso usa ~35-55 MiB e **volta a 15-30 MiB** depois de cada montagem.

| Cenário | Arquivos | Tempo | Pico (sem limite) | Com 512 MB |
|---|---|---|---|---|
| 26 arquivos, projeto real de teste (pela tela) | 11 MB | ~1 s | 70-84 MiB | — |
| 300 PDFs pequenos (1.811 páginas) | 1 MB | 1,9 s | 85 MiB | — |
| 20 documentos Word (Gotenberg converte) | 0,02 MB | 6,6 s | 53 MiB | — |
| 1 PDF grande | 92 MB | 2,7 s | 171 MiB | passou (229 MiB, 5,3 s) |
| 1 PDF grande + 3 fotos de 12 MP | 125 MB | — | — | **passou** (176 MiB, 9,0 s) |
| 1 PDF grande + 5 fotos de 12 MP | 150 MB | — | — | **API morta (OOM)** |
| 300 pequenos + 2 PDFs grandes | 185 MB | 6,9 s | 440 MiB | **API morta (OOM)** |
| 2 PDFs grandes | 185 MB | — | — | **API morta (OOM)** |
| 3 PDFs grandes | 275 MB | 8,7 s | 539 MiB | — |
| 20 fotos de 12 MP | 222 MB | 20,6 s | 477 MiB | — |

Conclusões:

- **O pico é cerca de 2,2 vezes o tamanho dos arquivos**, e vale para PDF e para foto. O Word é barato (a conversão ocorre na memória do Gotenberg).
- **Com 512 MB o teto real fica entre 125 e 150 MB de arquivos.** Acima disso o kernel mata o processo da API (`OOMKilled`, saída 137). Sem limite, 185 MB dava pico de 440 MiB, abaixo de 512, mas **dentro do contêiner** os temporários em disco e o cache de páginas também contam, então a conta do pico não basta.
- **Uma morte por falta de memória derruba a API inteira** (todos os usuários), não só a montagem. Em produção o `restart: unless-stopped` reinicia o contêiner; em dev o `air` não.
- O `GOMEMLIMIT` segura o pico (o coletor trabalha mais: o tempo dobra), mas não evita a morte quando o volume é grande.

### 12.2 O que foi feito com isso

1. **O limite de arquivos é calculado a partir da RAM real do contêiner** (`inputBudget`): `(75% da memória − 60 MiB de reserva) ÷ 3`. Com 512 MB dá **~108 MB** (abaixo dos 125 MB que passaram); com 1 GB, ~236 MB; com 2 GB, ~492 MB. O limite efetivo é o **menor** entre `REPORT_MAX_INPUT_MB` (padrão 100) e esse. Passou do limite: 422 `report_too_large` na hora, com a explicação, e o resumido continua disponível. Aumentar a RAM da API aumenta o limite sozinho.
2. **Pânico não derruba a API.** O `pdfcpu` dá pânico em vez de devolver erro em alguns caminhos (por exemplo, sem diretório de configuração gravável, que é o caso do contêiner somente leitura). `pdfbuild` usa a configuração sem estado e converte qualquer pânico em erro, e o worker também se protege.
3. **Montagem interrompida não é refeita sozinha** (um arquivo que mata o processo não pode entrar em laço): ela vira `failed` com mensagem pedindo para gerar de novo.
4. **A API devolve memória ao sistema ao fim de cada montagem** (`FreeOSMemory`); sem isso o contêiner ficava em ~220 MiB parado.
5. **Produção:** a API roda com o sistema de arquivos somente leitura e sem `/tmp` gravável. Os temporários vão para o volume `report-tmp` (disco; um `tmpfs` contaria na RAM), com o diretório criado na imagem com o dono certo.

### 12.3 Comportamento

- **Fila e andamento:** `queued` → `running` (n de N arquivos, atualizado a cada 15 s e na tela a cada 3 s) → `done` ou `failed`. Uma montagem por projeto por vez (409 `report_in_progress`).
- **Cada arquivo:** PDF é saneado (sem JavaScript, abertura de programa, anexos nem ações de página); imagem vira página A4 (com a orientação EXIF); Word é convertido pelo Gotenberg. Qualquer falha (senha, corrompido, conversão recusada, imagem grande demais, arquivo ausente) vira uma **página de aviso** no lugar, com o motivo, e entra em `warnings`.
- **PDF final:** relatório com sumário (páginas e links) + documentos, folha de rosto opcional, bookmarks, numeração contínua opcional. Conferência final: nenhum conteúdo ativo e o número de páginas esperado, senão a montagem falha.
- **Entrega:** o PDF fica no armazenamento por 24 h (`REPORT_RETENTION`) e é entregue em fluxo (não passa pela memória da API nem pela do Next). Vencido, só o registro e os dados usados permanecem (D5).
- **Auditoria:** `project.report-generate` e `project.report-download`.

### 12.4 Decisão (RAM da API)

**Decidido: a API passa a rodar com 1 GB** (`API_MEM_LIMIT=1g`, `API_GOMEMLIMIT=800MiB`, `REPORT_MAX_INPUT_MB=200`). O limite efetivo fica em ~236 MB pelo cálculo da memória, e 200 MB pelo teto configurado. A hml (8 GB, 7 em uso) recebe só um teto maior; o uso real em repouso continua ~40 MiB. Um contêiner só para a montagem fica como evolução se 1 GB não bastar.

Texto original da análise:

Para relatórios completos maiores que ~108 MB com a API em 512 MB, o caminho é **aumentar a RAM da API** (o limite sobe sozinho; 1 GB dá ~236 MB). Um contêiner só para a montagem isolaria o risco de derrubar a API, mas o ganho só aparece acima disso; fica como evolução se a API em 1 GB não bastar. Também ficou aberto: se o proponente vai querer relatórios completos acima de 100 MB (o upload permite até 200 MB por arquivo).
