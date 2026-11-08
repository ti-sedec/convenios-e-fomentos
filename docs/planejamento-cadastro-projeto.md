# Planejamento — Cadastro de projeto pelo proponente

> Status: **planejamento (sem código)** · Atualizado em 23/10/2026
> Escopo: do login do proponente até o **envio** do projeto e o ciclo de **complementação/arquivamento** que dependa dele.
> Fora deste documento: telas e fluxo da equipe interna (kanban, notas, pareceres, SIGADOC), PGE, assinatura, Diário Oficial, conta bancária e prestação de contas.
> **Fluxo da equipe (análise, enquadramento, arquivamento):** o diagrama [bpmn/convenios-e-fomento.bpmn](bpmn/convenios-e-fomento.bpmn) é a fonte da verdade, e o plano de execução está em [planejamento-fluxo-analise-enquadramento.md](planejamento-fluxo-analise-enquadramento.md). Onde este documento divergir deles quanto a esse fluxo, valem o diagrama e aquele plano.

---

## 1. Decisões já tomadas

| # | Tema | Decisão |
|---|---|---|
| 1 | Instrumentos | **Convênio** (IN Conjunta 001/2015) e **Termo de Fomento** (Lei 13.019 + IN Conjunta 001/2016 alterada pelas INs 004/2024 e 002/2026). |
| 2 | Tipos de entidade | Lista padrão com **Prefeitura Municipal** pré-selecionada e **OUTRO** (texto livre + informar se é pública ou privada). |
| 3 | SIGCon | O sistema **não integra** com o SIGCon: reproduz o fluxo. O proponente lança o plano lá e anexa os PDFs aqui. |
| 4 | Lei 12.082/2023 | Todas as regras valem quando o objeto for evento com contratação de artistas. |
| 5 | Checklist | Convênio usa o checklist de **prefeituras**, para qualquer entidade. Fomento usa o de **OSC**. |
| 6 | Número do termo = nº da proposta no SIGCon | **Um campo só** (`NNNN/AAAA`, ex.: 3216/2026). |
| 7 | "Recursos próprios" | É **recurso orçamentário da SEDEC**. A contrapartida do proponente é um campo separado. |
| 8 | Chamamento | Exigido sempre que houver recurso próprio (inclusive misto com emenda). O analista pode dispensar, com registro. |
| 9 | Resultado do parecer | **Deferido/Indeferido** é o parecer técnico do analista. **Arquivado** é a situação do projeto. São coisas separadas. |
| 10 | Arquivamento | Definitivo. Quem quiser tentar de novo usa **Duplicar projeto** (rascunho novo, com a origem registrada). |
| 11 | Área do projeto | Definida pelo **analista** na conferência, nunca pelo proponente. |
| 12 | Entidade de fora de MT | Pode dar entrada. Exige **execução em MT** e **credenciamento no SIGCon**. |
| 13 | Compartilhamento | Por **CPF**, nível Leitura ou Edição, por projeto. **Só o titular** concede. |
| 14 | Acesso | Login pelo **MT-Login**. Todo login vira **Proponente**. A equipe interna é atribuída por um administrador. |
| 15 | Parlamentares | Lista mantida pelo admin (estaduais e federais) com opção de texto livre. |
| 16 | Fim do escopo geral | Termina no Deferido ou Indeferido (ou Arquivado). |

---

## 2. Identificadores

| Identificador | Quem gera | Quando |
|---|---|---|
| **Código do projeto** (`2026-000123`, sequencial por ano) | Sistema | No envio (rascunho não tem). É o comprovante de recebimento, com data e hora do servidor. |
| **Nº da proposta/termo no SIGCon** (`NNNN/AAAA`) | Proponente | No cadastro. |
| **Nº do processo no SIGADOC** | Analista | Depois do envio. É o protocolo oficial (a Portaria 247 só gera o protocolo após a conferência). |

---

## 3. Acesso, papéis e compartilhamento

### 3.1 Como se encaixa no que já existe
O backend já autentica por **MT-Login** via CPF, cria a conta no primeiro acesso (sem roles, tela "Aguardando liberação"), e tem um motor de permissões (ação + recurso + campos + condições, com filtro aplicado no SQL) e **departamentos**.

**Decisão técnica (recomendada, pode vetar):** o proponente usa a **mesma identidade e o mesmo login** da equipe (tabela de administradores), recebendo automaticamente uma **role Proponente**. Os motivos:
- Reaproveita o MT-Login, a auditoria e o motor de permissões já prontos.
- O fluxo de usuário final atual (e-mail e senha, JWT próprio) não é o que você pediu.

Isso traz cinco consequências:
1. **Role padrão no auto-cadastro:** ao criar a conta, o sistema atribui Proponente. A tela "Aguardando liberação" some para esse papel.
2. **Separar proponente de equipe** na lista de administradores (campo `tipo` ou filtro), senão a lista enche de proponentes.
3. **O que a role Proponente pode:** apenas as ações de projeto, restritas aos projetos onde ele é titular ou membro. Nenhuma ação de configuração.
4. **Área = departamento** existente. O Revisor do Departamento usa a condição `same-department` que já existe.
5. **Visibilidade por projeto:** projeto sem área ainda não aparece para o Revisor do Departamento.

### 3.2 Papéis
| Papel | No cadastro de projeto |
|---|---|
| **Proponente** | Cria, edita (rascunho/devolvido) e envia projetos próprios. Gerencia acesso de terceiros se for titular. |
| **Analista** | Vê todos os projetos enviados. Assume a análise, confere documento a documento, devolve para complementação, arquiva por falta de documentos obrigatórios e indica o departamento (a área). |
| **Supervisor Administrador** | Vê todos os projetos enviados, somente leitura. Recebe e-mails (documentação regular, enquadramento recusado, arquivamento). Não decide nada no fluxo. |
| **Revisor do Departamento** | Vê só os da sua área. Confirma ou recusa o enquadramento do projeto no departamento. |
| **Secretário de Estado** | Vê todos os projetos enviados. Pode negar (arquiva) a qualquer momento. |

### 3.3 Compartilhamento
- **Titular** é quem criou o projeto. Só ele concede, altera e revoga acesso. Convidado não repassa.
- O convite é por **CPF**, com nível **Leitura** ou **Edição**. Se o CPF ainda não tem conta, o convite fica **pendente** e vale no primeiro login com aquele CPF.
- Convidado com Edição altera os dados, mas não gerencia acesso e não desiste do projeto.
- Revogar vale na hora. Tudo vai para a auditoria (CPF mascarado nas listas).
- Troca de titular: só por administrador.
- Edição simultânea: controle de versão do registro. Quem salvar atrasado recebe aviso de conflito em vez de sobrescrever.

### 3.4 Como ficou implementado (Fase 4)
- **Tabela `project_members`** (projeto, CPF, identificação opcional, nível `read`/`edit`, situação `active`/`revoked`, quem convidou, datas). Um registro por projeto e CPF; reconvidar quem foi revogado reativa o mesmo registro.
- **O acesso vem do CPF da conta** (`admins.cpf`) e é conferido a cada requisição pelas condições `admin::project-member` (qualquer nível) e `admin::project-editor` (só edição), que se somam a `admin::is-creator` na role Proponente. Por isso o convite feito antes do primeiro login já vale quando a pessoa entra, e a revogação vale na hora, mesmo com a sessão aberta. Não há tabela de "ativação": nada precisa rodar no login.
- **Quem faz o quê**: leitura vê projeto, arquivos e comprovante; edição também altera, anexa, envia e reenvia (inclusive durante a complementação). Excluir, desistir, duplicar e gerir acesso são só do titular (permissão `admin::projects.share` com a condição "é o criador"). O convidado vê o CPF completo do proponente (o titular o escolheu); a equipe continua vendo mascarado.
- **Decisão sobre privacidade**: a API não informa se um CPF tem conta nem o nome da pessoa (isso permitiria descobrir nomes a partir de CPFs). O titular identifica o convidado por um rótulo livre e vê o CPF mascarado. "Convite pendente" = a pessoa ainda não abriu o projeto (`accepted_at`).
- **Limites e regras**: até 10 pessoas por projeto; o CPF do próprio titular é recusado; CPF duplicado é 409. Auditoria: `project.member-invited`, `project.member-updated`, `project.member-revoked` (CPF mascarado).
- **Aviso por e-mail**: ao convidar, o titular pode informar o e-mail da pessoa (opcional, guardado em `project_members.email`). A API procura uma conta com aquele CPF: se existe (ativa e não bloqueada), o aviso vai para o e-mail **da conta**, que nunca é mostrado ao titular; se não existe, vai para o e-mail informado; sem nenhum dos dois, o convite vale, mas sem aviso. O e-mail diz quem deu o acesso, o nível, o link do projeto e o CPF mascarado. Falha de envio não desfaz o convite.
- **Decisão sobre o campo de e-mail**: o campo fica sempre visível e opcional, em vez de só abrir quando o CPF não tem conta. Abrir sob condição exigiria a API dizer se o CPF tem conta, que é justamente o que evitamos.
- **Quem envia**: o convidado com Edição pode enviar o projeto em nome do titular (decisão do órgão); a declaração de veracidade e a auditoria registram quem aceitou, e a responsabilidade perante a SEDEC é do titular.
- **Troca de titularidade**: não existirá por enquanto (decisão do órgão). O titular é sempre quem criou o projeto.
- **Role Proponente já existente**: a migração 00016 acrescenta as condições novas e a permissão de gestão às permissões que ainda eram "só o criador"; roles personalizadas não são alteradas.

---

## 4. Modelo conceitual de dados

Banco: PostgreSQL. Dinheiro em **centavos**. Datas no fuso de Cuiabá.

### 4.1 Perfil do proponente (um por usuário, reaproveitado)
Telefones (≥1 celular), e-mails (≥1, um principal), UF e município, cargo padrão. Nome e CPF vêm do MT-Login, somente leitura.

### 4.2 Projeto
Os dados da instituição são **copiados para dentro do projeto** (não existe tabela compartilhada de entidades). Assim ninguém sobrescreve dados de outro proponente que use o mesmo CNPJ. O sistema só sugere, a partir dos projetos anteriores do próprio usuário.

| Grupo | Campos |
|---|---|
| **Identificação** | código (no envio), instrumento, titular, situação (ativo/arquivado), etapa, versão atual, enviado em, área (departamento, definida pelo analista) |
| **Proponente (cópia)** | nome, CPF, telefones, e-mails, município/UF, cargo; é representante legal? (se não: nome, CPF e cargo do representante legal) |
| **Instituição (cópia)** | CNPJ, nome, tipo de entidade (+ texto se OUTRO), natureza (pública/privada), sem fins lucrativos?, UF e município da sede, endereço, e-mail e telefone institucionais |
| **Habilitação** | status declarado (Plena/Parcial), validade, nº da certidão (anexo no checklist) |
| **Proposta** | nº SIGCon, data de cadastro no SIGCon, título, objeto, justificativa, **períodos e locais de execução** (1..50 linhas; cada uma com início e término previstos, município de execução de MT e detalhe do local opcional; a vigência do projeto é o menor início e o maior término), natureza do objeto, evento com artistas?, calamidade/emergência? |
| **Recursos** | tipo (Emenda / Recurso próprio / Misto), valor recurso próprio, valor emenda (calculado), **total solicitado (calculado)**, contrapartida (valor + tipo), custos indiretos (Fomento) |
| **Chamamento** | situação (realizado / dispensa / inexigível / não se aplica), nº do edital, data de homologação, hipótese legal, justificativa |
| **Evento com artistas** | valor total de cachês, valor de cachês para artistas regionais |
| **Preços (Fomento)** | data de coleta mais antiga das referências de preço |

### 4.3 Tabelas filhas
- **Emendas:** parlamentar (catálogo ou texto livre), nº da emenda, ano, valor. Cada linha tem os dois ofícios anexados.
- **Membros:** CPF, usuário (preenchido no primeiro login), nível, quem convidou, status (pendente/ativo/revogado), datas.
- **Documentos:** requisito do checklist, arquivo, hash, enviado por/em, substituído por, texto de justificativa de não envio, emenda associada (opcional), situação de conferência (pelo analista, fase seguinte).
- **Versões:** cada envio e cada reenvio guarda um retrato imutável (campos + lista de documentos com hash). Motivo: envio inicial ou complementação.
- **Histórico:** reaproveita o módulo de auditoria existente.

### 4.4 Catálogos e parâmetros
- **Tipos de entidade:** código, nome, natureza, sem fins lucrativos?, ativo.
- **Municípios:** IBGE, todos os do Brasil (a sede pode ser fora de MT). Os de execução são filtrados para MT.
- **Parlamentares:** nome, casa (estadual/federal), ativo. Texto livre cai numa fila para o admin organizar.
- **Requisitos de documento** (checklist): versionado por vigência, ver seção 7.
- **Parâmetros de regra**, por vigência/ano e **nunca fixos no código**: data-limite eleitoral, teto da Lei 12.082 (atualizado por IPCA), contrapartida mínima, percentual da LDO, faixas de custos indiretos, validade das referências de preço (60 dias), prazo de complementação (3 dias corridos).

---

## 5. Ciclo de vida

Duas dimensões separadas, mais o parecer:

1. **Etapa** (coluna do kanban da equipe): Rascunho → Aguardando análise de checklist → Em análise → Aguardando proponente → Aguardando parecer → Parado → Concluído. O projeto passa para "Aguardando parecer" quando o Revisor do Departamento confirma o enquadramento.
2. **Situação:** Ativo ou **Arquivado** (com motivo, quem e quando).
3. **Parecer técnico** (entidade própria, do analista): Deferido ou Indeferido.

| De → Para | Quem | Regra |
|---|---|---|
| Rascunho → Aguardando análise | Titular ou membro com Edição | Só sem pendências bloqueantes. Gera código, versão 1 e comprovante em PDF. |
| Em análise → Aguardando proponente | Analista | Lista os itens devolvidos. Prazo de **3 dias corridos** (fim do 3º dia, 23:59, Cuiabá). |
| Aguardando proponente → Em análise | Titular ou Edição | Reenvio cria uma nova versão. Só os itens devolvidos ficam editáveis. |
| Aguardando proponente → Arquivado | Sistema | Prazo vencido. Motivo: prazo de complementação. |
| Qualquer etapa → Arquivado | Secretário | A qualquer momento, com motivo, sem passar por parecer. |
| Qualquer etapa → Arquivado | Titular (desistência) | Com confirmação e motivo. |
| Em análise → Arquivado | Analista | Falta de documentos obrigatórios (Portaria 71, art. 1º §2º). Com motivo. |
| Aguardando análise de checklist → Em análise | Analista | O Analista assume a análise e passa a ser o responsável pelo projeto. |
| Em análise → enquadramento pendente | Analista | Só com todos os documentos e grupos de informações aprovados. Seleciona o departamento (área finalística); o projeto aparece para os Revisores daquele departamento. |
| Enquadramento pendente → Aguardando parecer | Revisor do Departamento | Confirma o enquadramento. Vale a primeira decisão de qualquer Revisor do departamento. |
| Enquadramento pendente → Em análise | Revisor do Departamento | Recusa o enquadramento, com justificativa. O departamento é limpo e o projeto volta ao Analista. Sem limite de recusas. |

**Regras gerais:**
- Depois do envio o projeto é somente leitura, exceto nos itens devolvidos.
- Projeto arquivado é terminal e somente leitura.
- **Duplicar projeto** (qualquer projeto do proponente, inclusive arquivado) cria rascunho novo copiando os dados. Os documentos são copiados só se o proponente marcar, porque certidões vencem. O novo projeto guarda o vínculo "originado de". O nº SIGCon copiado pode ser trocado.
- Rascunho pode ser apagado pelo titular. Rascunhos abandonados são limpos após N dias (valor a definir).

---

## 6. Regras de negócio e validações

**Severidade:** B = bloqueia o envio · A = avisa o proponente (o analista vê o aviso) · I = só informa ao analista.

| ID | Regra | Fonte | Sev. |
|---|---|---|---|
| RN-01 | CNPJ com dígitos verificadores válidos. Vale o formato numérico e o alfanumérico (IN RFB 2.229/2024: 12 posições de 0-9 e A-Z + 2 dígitos verificadores numéricos; cálculo módulo 11 com o valor de cada caractere igual ao código ASCII − 48). | — | B |
| RN-02 | **Fomento** só para entidade privada sem fins lucrativos. | Lei 13.019 | B |
| RN-03 | **Convênio** vale para qualquer natureza, com o checklist de prefeituras. | IN 001/2015 | — |
| RN-04 | Todos os municípios de **execução** (um por período/local) são de MT. A sede pode ser de qualquer UF. | Decisão #12 | B |
| RN-05 | Nº SIGCon no formato `NNNN/AAAA`, com ano coerente com a data de cadastro. Único entre projetos **não arquivados**. | Decisão #6 | B |
| RN-06 | Data de cadastro no SIGCon não pode ser futura. | — | B |
| RN-07 | Vigência: em cada período/local, o término não é anterior ao início (períodos podem se sobrepor). Aviso se o início for incompatível com a assinatura até o último dia útil de dezembro. | Manual SIGCon | B / A |
| RN-08 | Telefones: pelo menos 1 celular válido. E-mails: pelo menos 1 válido. | Pedido | B |
| RN-09 | Valores positivos. Total solicitado = recurso próprio + emenda, sempre calculado no servidor. | — | B |
| RN-10 | Tipo com emenda: pelo menos 1 linha (parlamentar, nº, valor > 0). Soma das linhas = valor em emenda. Cada linha com os 2 ofícios. | Pedido | B |
| RN-11 | Tipo "Recurso próprio": sem linhas de emenda e valor de emenda zero. Tipo "Emenda": valor próprio zero. | — | B |
| RN-12 | **Contrapartida no Convênio:** declaração (Anexo IV) e percentual da LDO (parâmetro). Dispensada em calamidade ou emergência. | IN 001/2015 art. 16 | B |
| RN-13 | **Contrapartida no Fomento:** só em bens e serviços, com valor em reais. Nunca em dinheiro. | IN 001/2016 art. 28 §1º | B |
| RN-14 | **Chamamento obrigatório** para: Fomento com recurso próprio (puro ou misto) e Convênio com entidade privada. Sem chamamento: pode informar dispensa/inexigibilidade com justificativa e anexo. Fomento só com emenda: não se aplica. | IN 001/2016 arts. 14, 19, 20; IN 001/2015 arts. 13-14 | B |
| RN-15 | **Custos indiretos (Fomento):** até 20% (teto R$ 15 mil) para parcerias até R$ 100 mil; 15% (R$ 50 mil) até R$ 500 mil; 10% (R$ 200 mil) acima. | IN 002/2026 art. 36-A | B |
| RN-16 | **Evento com artistas:** repasse ≤ teto (R$ 600 mil, atualizado por IPCA); contrapartida ≥ 10% quando o convenente é município; cachês ≤ 50% do repasse; cachês regionais ≥ 30% do valor de cachês. Vale inclusive para emenda. Pode ser afastada com autorização expressa do Governador (anexo). | Lei 12.082/2023 | B |
| RN-17 | **Prazo eleitoral:** envio após a data-limite do ano **não é bloqueado**, mas o projeto entra marcado e o proponente é avisado. Só em ano eleitoral. | Portaria Conjunta 71/2026 | A |
| RN-18 | Certidão de Habilitação Plena anexa, com validade futura. Parcial ou vencida bloqueia. | IN 001/2016 art. 8º; Habilitação SIGCon | B |
| RN-19 | Referências de preço (Fomento): data de coleta mais antiga com no máximo 60 dias na data do envio. | IN 002/2026 art. 10-A §6º | A |
| RN-20 | Todo item obrigatório do checklist tem arquivo **ou** justificativa de não envio (Anexo X/XVII). | Portaria 71 art. 1º §2º | B |
| RN-21 | Título e objeto com tamanho mínimo. | — | B |
| RN-22 | Objeto/município/CNPJ parecidos com outro projeto ativo: aviso interno ao analista. | Declaração de não duplicidade | I |
| RN-23 | Após o envio, só os itens devolvidos podem ser alterados. | Portaria 247 art. 2º | B |
| RN-24 | Custos e sublimites de LDO e IPCA vêm dos parâmetros, não do código. | — | — |

---

## 7. Documentos (checklist)

**Mecânica:**
- O catálogo é **versionado e guiado por dados**. As normas mudam todo ano, e a lista não pode ficar escrita no código.
- Cada requisito tem: código, nome, instrumento, condição de aplicação, se aceita vários arquivos, modelo para baixar (os Anexos da SEDEC) e se aceita justificativa de não envio.
- Condições possíveis: instrumento, natureza da entidade, tipo de recurso, natureza do objeto, evento com artistas, calamidade, situação de chamamento.
- Por item, o proponente envia arquivo(s) **ou** a justificativa de não envio.
- Substituir um arquivo mantém o antigo no histórico (com hash).
- Reaproveita o upload em 3 passos que o sistema já tem (autoriza, envia direto ao storage, confirma), com tipo, tamanho e verificação de vírus validados no backend.
- Os itens de **depois da análise técnica favorável** (conta bancária) ficam fora.

**Catálogo inicial** (C = item do checklist de prefeituras, F = de OSC):

| Cód. | Documento | Conv. | Fom. | Quando se aplica |
|---|---|---|---|---|
| D01 | Ofício de solicitação ao Secretário da SEDEC | C1 | F1 | Sempre |
| D02 | Requerimento de Apoio Financeiro (modelo) | C2 | F2 | Sempre |
| D03 | Ofício do deputado (nº e valor da emenda) | C2.1 | F2.1 | Por emenda |
| D04 | Ofício da Casa Civil (nº e valor da emenda) | C2.2 | — | Por emenda (exigência para os dois instrumentos) |
| D05 | Justificativa de dispensa/inexigibilidade de chamamento | — | F2.2 | Chamamento dispensado/inexigível |
| D06 | Edital e homologação do chamamento | — | — | Chamamento realizado |
| D07 | Plano de Trabalho e anexos do SIGCon (cadastro, dados do projeto, cronogramas, plano de aplicação, desembolso, equipamentos, memória de cálculo) | C3, 3.1-3.6 | F3 | Sempre |
| D08 | Comprovante de envio do Plano de Trabalho no SIGCon | C3.7 | F3 | Sempre |
| D09 | Certidão de Habilitação Plena | C4 | F4 | Sempre |
| D10 | Termo de Referência (modelo) | C5 | F5 | Sempre (bens/serviços) |
| D11 | Mapa comparativo dos orçamentos | C18 | F6 | Sempre |
| D12 | Análise crítica dos orçamentos | C19 | F6.1 | Sempre |
| D13 | Avaliação de mercado, 3 orçamentos ou Ata de Registro de Preços | C7.1 | — | Sempre |
| D14 | Declaração de compatibilidade de preços | C7 | F7 | Sempre |
| D15 | Exclusividade: 3 notas fiscais + contrato (Conv.) / documentação de exclusividade artística (Fom.) | C7.2 | F8 | Quando houver exclusividade |
| D16 | Declaração de não duplicidade de objeto | C8 | F10 | Sempre |
| D17 | Declaração de gratuidade | C9 | F11 | Sempre |
| D18 | Declaração de publicidade da parceria | C10 | F12 | Sempre |
| D19 | Declaração de contrapartida | C6 | — | Convênio |
| D20 | Quadro de Detalhamento de Despesa | C6.1 | — | Convênio, entidade pública |
| D21 | LOA publicada em Diário Oficial | C6.2 | — | Convênio, entidade pública |
| D22 | Comprovante de equipe técnica / capacidade técnica e operacional (+ declaração de instalações) | C11 | F14 | Sempre |
| D23 | Experiência prévia (declaração e comprovação) | — | F15 | Fomento |
| D24 | Comprovante de endereço da instituição | C12 | F17 | Sempre |
| D25 | Cartão do CNPJ | C13 | — | Convênio |
| D26 | RG e CPF do dirigente | C14 | — | Convênio |
| D27 | Ata/ato de nomeação ou posse do dirigente | C15 | F16 | Sempre |
| D28 | Diploma de nomeação | C16 | — | Convênio, cargo eletivo |
| D29 | Comprovante de endereço do dirigente | C17 | — | Convênio |
| D30 | Estatuto, atas e documentação da diretoria | — | F16 | Fomento |
| D31 | Comprovação de domicílio | — | F9 | Fomento |
| D32 | Declaração de não impedimento (art. 39 da Lei 13.019) | — | F18 | Fomento |
| D33 | Atesto de execução direta do objeto | — | F19 | Fomento |
| D34 | Compatibilidade institucional da OSC | — | F20 | Fomento |
| D35 | Declaração de ausência de conflito de interesses | — | F21 | Fomento |
| D36 | Declaração sobre despesas indiretas | — | F22 | Fomento |
| D37 | Declaração sobre remuneração de dirigentes | — | F23 | Fomento |
| D38 | Termo de ciência e responsabilidade dos dirigentes | — | F24 | Fomento |
| D39 | Autorização para uso de espaço público | — | F13 | Atividade em espaço público |
| D40 | Justificativa de redução de prazo | C20 | — | Quando solicitar prazo reduzido |
| D41 | Projeto básico da obra/serviço de engenharia, com ART (CREA/CAU): arquitetônico, elétrico, hidrossanitário, estrutural, climatização, incêndio, SPDA, mapa de danos, memorial, planilha, cronograma físico-financeiro | C21, 21.1-21.12 | Art. 29 II | Natureza do objeto = obra |
| D42 | Licenças ambientais ou dispensa | C23 | Art. 29 IV | Obra |
| D43 | Certificado de propriedade do imóvel | C24 | Art. 29 IX | Obra |
| D44 | Ato de calamidade pública/emergência | — | Art. 29 VI | Calamidade |
| D45 | Autorização do Governador (Lei 12.082, art. 5º) | — | — | Evento fora dos limites |

Observações:
- D04 não consta no checklist de OSC publicado no site, mas você pediu para os dois instrumentos.
- D25 a D29 do Fomento não estão no checklist de OSC, porque a SEFAZ/SIFI já valida esses dados na habilitação.
- Os números de item (C#, F#) vêm das páginas da SEDEC e da IN 001/2016, art. 29.

---

## 8. Telas do proponente

**Lista "Meus projetos":** código, título, instrumento, situação, etapa simplificada, prazo de complementação (se houver), papel do usuário no projeto (titular, edição, leitura). Filtros por situação e instrumento. Botões: Novo, Duplicar.

**Assistente de cadastro** (salva sozinho a cada alteração, mostra o que falta):

1. **Proponente:** perfil. Representante legal.
2. **Instituição:** CNPJ, nome, tipo de entidade, instrumento, sede, habilitação. Bloqueio imediato da combinação inválida (Fomento + entidade pública).
3. **Proposta:** nº SIGCon, data, título, objeto, justificativa, períodos e locais de execução (uma linha por período, com município e detalhe do local), natureza do objeto, perguntas condicionais (evento com artistas, calamidade).
4. **Recursos:** tipo, valores, emendas (linha a linha com anexos), contrapartida, custos indiretos, chamamento. O total é mostrado em tempo real, só leitura.
5. **Evento com artistas:** só aparece se marcado. Mostra o quanto falta para cumprir os limites.
6. **Documentos:** checklist guiado por condições, com modelo para baixar, upload, justificativa e indicador de progresso.
7. **Revisão e envio:** resumo, pendências, declaração de veracidade (registrada com data, hora e IP) e confirmação.

**Depois do envio:** tela de acompanhamento com a etapa, a linha do tempo visível ao proponente, solicitações de complementação com o prazo regressivo, o comprovante em PDF e a aba **Acesso** (titular).

**Notificações por e-mail** (nos e-mails do projeto): recebimento, devolução para complementação (com prazo), prazo prestes a vencer, arquivamento, parecer emitido.

---

## 9. Permissões previstas (motor existente)

Recurso `projetos`, com ações como: listar, ler, criar, editar, enviar, arquivar, gerir acesso, duplicar.

| Papel | Escopo |
|---|---|
| Proponente | Condição `projeto-membro` (titular ou membro ativo). Edição só nos estados permitidos, validada pela regra de estado do serviço. Gerir acesso só como titular. |
| Analista | Todos os projetos enviados. Edita só os campos internos (área, conferência). |
| Supervisor Administrador | Todos os projetos enviados, somente leitura. |
| Revisor do Departamento | Só os projetos enviados da sua área (`same-department` combinado com o status, numa condição própria). |
| Secretário de Estado | Todos os projetos enviados. Pode negar (arquivar) a qualquer momento. |

Detalhe técnico: a condição `projeto-membro` precisa do conjunto de IDs dos projetos do usuário. Como um proponente tem poucos projetos, o serviço levanta essa lista e usa o filtro `In` que o motor já suporta, sem mexer no motor.

---

## 10. Segurança e LGPD

- RG, CPF e comprovantes de endereço são dados pessoais. Arquivos privados, download autenticado e registrado, CPFs mascarados em listas.
- Personificação de entidade: mitigada por validação de CNPJ, declaração de responsabilidade, conferência do analista (certidão, CNPJ e dirigente batem) e isolamento dos dados por projeto.
- Limites de tamanho, tipos de arquivo e taxa de upload. Verificação de vírus.
- Auditoria de tudo: campos, documentos, acessos concedidos/revogados, envios, decisões.
- Prazo de retenção de rascunhos e de arquivos de projetos arquivados: a definir.

---

## 11. Entrega em fatias

| Fase | Conteúdo | Pronto quando |
|---|---|---|
| 0 | Catálogos (entidades, municípios, parlamentares), parâmetros de regra, role Proponente e auto-cadastro | Login cria Proponente sem passar pela tela de espera |
| 1 | Perfil + rascunho com etapas 1 a 3 | Salvar/retomar rascunho |
| 2 | Recursos, emendas, regras RN-09 a RN-16 | Valores e limites validados no servidor |
| 3 | Catálogo de requisitos + upload + justificativa | Checklist correto por combinação |
| 4 | Compartilhamento por CPF | Convite pendente que ativa no primeiro login. **Feito**: ver 3.4 |
| 5 | Envio: versão imutável, código, comprovante PDF, e-mails | Projeto aparece para a equipe em "Aguardando análise" |
| 6 | Complementação, arquivamento, desistência, duplicar | Prazo vencido arquiva sozinho |
| 7 | Endurecimento: testes, carga de upload, revisão LGPD | Matriz de acesso por rota, concorrência e limites testados; hash + antivírus opcional nos arquivos; CPF mascarado para a equipe; downloads auditados; retenção de rascunhos (ver `backend/SEGURANCA.md`) |
| 8 | Fluxo de análise, enquadramento e arquivamento da equipe (perfis, conferência, departamento, e-mails, telas) | Ver [planejamento-fluxo-analise-enquadramento.md](planejamento-fluxo-analise-enquadramento.md) e [planejamento-fases-0-e-1.md](planejamento-fases-0-e-1.md) |

**Testes que não podem faltar:** combinação instrumento × natureza; cada regra RN com valor no limite e acima; convite antes do primeiro login; revogação com sessão aberta; dois editores ao mesmo tempo; reenvio só dos itens devolvidos; arquivamento por prazo; duplicar a partir de arquivado; proponente tentando ler projeto alheio por ID.

**Cobertos na Fase 7** (`internal/app/project_hardening_test.go`): proponente alheio em todas as rotas do projeto (rascunho e enviado); busca/ordenação/paginação com entradas hostis; NUL, e-mail com quebra de linha, corpo grande e campos desconhecidos; PDF do comprovante com texto hostil; 30 pedidos de envio simultâneos no mesmo documento; 8 gravações simultâneas da mesma versão; ciclo completo de upload em paralelo; limites de envios pendentes e de armazenamento por projeto; taxa de envio por usuário (429); hash e antivírus (limpo, infectado, scanner fora do ar); CPF mascarado para a equipe; auditoria de downloads; retenção de rascunhos. Os testes de carga acharam um deadlock do pool de conexões (leitura de regras fora da transação), já corrigido.

---

## 12. Premissas que adotei (dúvidas menores)

- Prazo de 3 dias corridos de complementação vale para os dois instrumentos (a norma é de OSC, mas o fluxo é o mesmo). Configurável.
- O analista pode arquivar por falta de documentos obrigatórios (Portaria 71).
- Convênio com entidade privada exige comprovação de chamamento (IN 001/2015 art. 13), com as exceções do art. 14. O analista pode dispensar, com registro.
- Entidade de outro estado é aceita com sede fora de MT; as certidões das normas de outros estados são conferidas pelo analista.
- Habilitação Parcial bloqueia o envio, porque o checklist pede "Plena".
- Enviado não pode ser editado nem pelo titular; só desistir.
- Troca de titular só pelo administrador.
- O sistema grava o código do projeto no envio. O nº SIGADOC é preenchido pelo analista depois.

## 13. Itens que ficam para os próximos documentos
Kanban e papéis internos em detalhe (notas, atividades, SIGADOC, pareceres), a ordem exata das autorizações do Revisor do Departamento e do Secretário, textos de e-mail, catálogo final dos modelos de documentos, política de retenção.
