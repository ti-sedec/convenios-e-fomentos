# Declarações geradas pelo sistema e assinadas com o MT-ID

Objetivo: o proponente confere o texto da declaração na tela, aceita a emissão, confirma a geração, e o sistema produz o PDF e o envia para assinatura no MT-ID. Os modelos são os publicados pela SEDEC (Anexos .docx do site), usados sem alteração de texto, exceto as diferenças listadas em **Decisões** e nas perguntas à SEDEC.

## Situação

| Etapa | Estado |
|---|---|
| Modelos, prévia, PDF e resumo do conteúdo | Feito para as cinco declarações: contrapartida, não duplicidade, gratuidade, publicidade e compatibilidade de preços |
| Aceite gravado (`project_declarations`), emissão, consulta, cancelamento, declaração desatualizada | Feito, com MT-ID simulado |
| Tela de conferência e acompanhamento no frontend | Feita; ainda não verificada no navegador |
| Reconhecimento das assinaturas e do cadastro | Feito: a leitura do projeto, do checklist, o envio e o reenvio concluem as assinaturas pendentes; o aviso de declaração desatualizada diz o que mudou; as pendências de endereço vêm por parte; telas atualizam o cache. Ver [planejamento-declaracoes-assinadas-reconhecimento.md](planejamento-declaracoes-assinadas-reconhecimento.md) |
| Cliente real do MT-ID | Adiado: exige rede com acesso ao X-Via. Ver [debito-tecnico-mt-id.md](debito-tecnico-mt-id.md) |
| Dados pessoais de quem assina (nacionalidade, estado civil, endereço; sem RG) | Feito, no perfil e no projeto. Gratuidade e publicidade dependem de respostas da SEDEC (abaixo) |

## Decisões

- **Sem RG.** O CPF identifica a pessoa. Os campos de RG ("Cédula de Identidade nº") não entram no cadastro nem nas declarações. Isso retira um trecho do texto oficial de gratuidade e de publicidade, então a SEDEC precisa aprovar a versão sem ele (pergunta 9).
- **Texto dos modelos.** Os textos são os oficiais. As únicas diferenças, todas a confirmar com a SEDEC, são: sem RG; flexão "(a)" onde o modelo supõe o gênero ("portador(a)", "casado(a)"); "residente e domiciliado em" no lugar de "na Rua" (o endereço já traz o tipo do logradouro); "para" no lugar do "paro" (erro de digitação) e "convênio" no lugar de "termo de fomento" na compatibilidade de preços quando o instrumento é convênio (o modelo da prefeitura repete o texto do fomento).
- **CPF no texto.** O texto de gratuidade e de publicidade traz o CPF completo de quem assina. Para a equipe da SEDEC, que só vê o CPF mascarado no cadastro, o texto na tela também sai mascarado; o resumo do conteúdo é o mesmo para todos.
- **Uma assinatura por declaração, um arquivo por declaração.** Cada declaração tem seu próprio registro (`project_declarations`), seu próprio pedido de assinatura no MT-ID e seu próprio arquivo PDF, anexado ao item correspondente do checklist (cada declaração já é um item separado). Não existe pacote único nem assinatura em lote: assinar uma não depende nem afeta as outras. Se uma ficar defasada (`paramsSha256` mudou), só ela é refeita.

## O que cada modelo exige

| Declaração | Modelo (prefeitura / OSC) | Campos além de signatário, cargo e instituição | Cadastro tem? |
|---|---|---|---|
| Contrapartida (só convênio de município) | Anexo IV | Valor depositado na conta exclusiva | Sim (`counterpartCents`, só se financeira) |
| Não duplicidade | Anexo VI / V | Nenhum | Sim, mas a afirmação é do signatário |
| Gratuidade | Anexo VII / VI | Nacionalidade, estado civil, CPF e endereço residencial de quem assina; nome da programação; município e data do evento | Pessoais: sim (novos campos). Programação: título do projeto. Município: municípios de execução. **Data: o cadastro tem a vigência, não a data do evento** |
| Publicidade da parceria | Anexo VIII / VII | Os mesmos dados pessoais de quem assina | Sim |
| Compatibilidade de preços | Anexo V / IV | Instituição e número da proposta no SIGCon | Sim |

Regras já implementadas na prévia:
- Assina o proponente, se for o representante legal; senão, o representante informado.
- Gratuidade e publicidade usam os dados pessoais do proponente (se é o representante legal) ou do representante informado; sem eles não emitem. A gratuidade ainda pede título, município de execução, vigência e CNPJ; a compatibilidade de preços, o número da proposta no SIGCon.
- Contrapartida é bloqueada para entidade que não é município e para contrapartida em bens e serviços (o modelo diz "depositará").
- `paramsSha256` resume texto, signatário, local e versão do modelo. Se mudar depois da emissão, a declaração emitida deixou de refletir o cadastro e precisa ser refeita.

## Perguntas para a SEDEC

1. O PDF gerado pelo sistema, sem o papel timbrado da instituição (os modelos pedem "preencher em papel timbrado"), é aceito? Com cabeçalho de nome e CNPJ basta?
2. Assinatura eletrônica pelo MT-ID substitui a assinatura manuscrita nessas declarações?
3. **Gratuidade:** vale só para projeto com evento? Para objeto sem evento (equipamento, obra), a declaração é dispensada ou há outro texto?
4. **Publicidade:** o modelo cita "Plataforma Mapas… ações promovidas no museu". O texto é esse mesmo para todos os projetos ou há versão corrigida?
5. **Contrapartida em bens e serviços** (convênio): existe modelo? O atual declara depósito em conta.
6. **Contrapartida de entidade que não é município** (convênio): existe modelo?
7. O texto da contrapartida cita "Portaria vigente (disponibilizada pela SEFAZ)". Devemos citar o número da portaria em vigor?
8. A data da declaração é a da emissão ou a da assinatura?
9. Podemos omitir o RG ("Cédula de Identidade nº") dos modelos de gratuidade e de publicidade e identificar a pessoa só pelo CPF?
10. **Gratuidade, data:** o modelo diz "no dia 00/00/201X" (data do evento), e o cadastro só tem a vigência do projeto. Hoje o texto sai "no período de [início] a [término]" (ou "no dia" se forem iguais). Isso serve, ou devemos pedir a data do evento no cadastro?
11. **Gratuidade, mais de um município:** o texto sai "nos municípios de A, B e C". Está bem?
12. **Compatibilidade de preços:** o modelo da prefeitura diz "proposta do termo de fomento n.º" (parece cópia do modelo da OSC). Podemos escrever "convênio" quando o instrumento é convênio? Aceita o número da proposta do SIGCon (ex.: 3216/2026) no lugar de "XXXX/201X"?
13. O texto diz "residente e domiciliado em", com a flexão "(a)" onde o modelo supõe o gênero ("portador(a)", "casado(a)"). Podemos manter?

## Perguntas para a MTI (MT-ID)

1. Nível da assinatura (Lei 14.063/2020): simples, avançada ou qualificada (ICP-Brasil)? Formato: PAdES dentro do PDF ou arquivo separado (`detached`)?
2. Com `detached: false`, o `fileId` do retorno aponta para o PDF já assinado?
3. O backend pode chamar o X-Via com o token do usuário (escopo `signature-requester` no client do Keycloak) ou deve usar credencial de sistema? Como a API alcança a URL privada?
4. Há ambiente de desenvolvimento do X-Via e do MTCidadão para testes ponta a ponta?
5. Prazo de expiração do pedido pendente e como cancelar (`DELETE /signature/{id}`).
6. Quem assina precisa ter o MTCidadão com CPF vinculado. Como orientar o usuário que não tem?

## Próximos passos

Ver [debito-tecnico-mt-id.md](debito-tecnico-mt-id.md), que lista o que falta, onde no código e em que ordem.
