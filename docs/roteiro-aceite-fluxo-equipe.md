# Roteiro de aceite: fluxo da equipe (análise, enquadramento e arquivamento)

Percurso para validar no navegador o que as fases 1 a 5 entregaram. Os testes automáticos cobrem a API; **as telas só foram verificadas por `typecheck`, lint, formato e `build`**, nunca abertas num navegador por quem as escreveu. Este roteiro é essa verificação.

## Antes de começar

1. Ambiente de desenvolvimento no ar com tudo aplicado: `docker compose -f docker-compose.dev.yml up -d`, migrações na versão 27 (`docker compose -f docker-compose.dev.yml run --rm migrate`) e `seed` rodado (os departamentos de exemplo, Financeiro e Jurídico, nascem como área finalística em desenvolvimento).
2. **Pessoas com login real.** Os administradores de exemplo do seed (`analista@dev.local` etc.) têm CPFs fictícios e **não entram** pelo MT Login. Em *Administração > Usuários*, pré-cadastre CPFs de gente que consegue entrar e atribua os perfis. O mínimo para o roteiro:

| Papel | Perfil | Observação |
|---|---|---|
| Proponente | Proponente (automático no primeiro acesso) | quem envia o projeto |
| Analista | Analista | |
| Revisor 1 | Revisor do Departamento | **com departamento Financeiro** |
| Revisor 2 | Revisor do Departamento | **com departamento Jurídico** |
| Secretário | Secretário de Estado | |
| Supervisor | Supervisor Administrador | |

   Dá para usar menos gente trocando os perfis de quem testa entre os passos (cada troca vale na próxima requisição). O **Super Admin** vê e faz tudo; serve para percorrer as telas, mas **não prova** as restrições de cada perfil.
3. Sem SMTP em desenvolvimento, os e-mails aparecem no log: `docker compose -f docker-compose.dev.yml logs -f api`.

## Passos

Marque cada item ao concluir. Em qualquer divergência, anote a tela, o perfil e o que apareceu.

### A. Menu e filas
- [ ] **Proponente:** o menu mostra "Meus projetos" e **não** "Análise de projetos".
- [ ] **Analista, Revisor, Secretário, Supervisor:** o menu mostra "Análise de projetos" e **não** "Meus projetos".
- [ ] Quem acumula Proponente e Analista vê os dois.
- [ ] As abas mudam por perfil (Analista: 7 abas; Revisor: Pendentes e Confirmados; Secretário e Supervisor: Todos, Em análise, Aguardando parecer, Arquivados). Os contadores batem com a lista de cada aba.

### B. Envio e início da análise
- [ ] O Proponente envia um projeto completo. Ele aparece em *Aguardando análise* do Analista (e só então).
- [ ] O Analista abre o projeto: vê o cabeçalho, as abas do cadastro **sem nenhum botão de edição**, e **Iniciar análise**.
- [ ] Ao iniciar, o projeto vai para *Em análise*, e o nome do Analista aparece como responsável.

### C. Conferência
- [ ] Aba **Conferência**: seções, documentos e (se houver) adicionais, com o progresso.
- [ ] **Analisar documento** abre um modal largo com o arquivo dentro dele (nada é baixado). O rodapé tem **Baixar original**, **Reprovar** e **Aprovar**.
- [ ] **Aprovar** começa desabilitado, com a dica "Role até o fim do arquivo para liberar Aprovar", e só liga quando a rolagem chega ao fim do documento (última página). Documento de uma página que cabe na tela libera na hora.
- [ ] **Reprovar** liga assim que a primeira página (ou a imagem) aparece; não precisa rolar até o fim.
- [ ] Documento com vários arquivos: a lista lateral marca cada arquivo (círculo vazio = não visto, olho = visto, check verde = lido até o fim); **Aprovar** só liga depois de ler todos.
- [ ] Reabrir o documento depois de ler até o fim mantém **Aprovar** ligado (a leitura fica registrada para quem leu; outro Analista precisa ler de novo).
- [ ] PDF: rolar, zoom (50% a 300%), setas de página, "Página N de M". PDF grande (mais de 100 páginas) abre sem travar. PDF com senha ou corrompido mostra a mensagem com **Baixar o arquivo**.
- [ ] PDF de scanner (páginas que são só imagem, como o "Plano de Trabalho e anexos do SIGCon") aparece com o conteúdo, não em branco.
- [ ] Imagem (PNG, JPEG, WebP) aparece ajustada à largura, com zoom.
- [ ] Word (.docx) aparece convertido em PDF, com o aviso "Exibido como PDF (convertido do Word)". **Baixar original** entrega o .docx. Com o Gotenberg desligado, aparece a mensagem de que não foi possível converter, com o botão de baixar.
- [ ] **Reprovar** abre o campo de justificativa (obrigatório, até 500 caracteres); **Confirmar reprovação** só liga com texto. O item vira "Reprovado" com o motivo, e o painel conta os reprovados.
- [ ] Baixar o arquivo **não** conta como visto nem como lido.
- [ ] Seção (sem arquivo) aprova e reprova direto na linha; reprovar pede a justificativa.
- [ ] Um item reprovado impede **Indicar departamento** (a mensagem cita os reprovados). **Aprovar** depois substitui a reprovação.
- [ ] **Indicar departamento** fica desligado, com o texto de quantos itens faltam, até aprovar tudo.

### D. Devolver e reenviar
- [ ] **Devolver para complementação**: os itens reprovados já vêm marcados, com a justificativa como observação. Escolha 2 itens com observação. O Proponente recebe o e-mail (log) e vê só esses itens editáveis, com o prazo.
- [ ] Devolva também um **documento adicional** do Proponente. Ele troca o arquivo e edita título e descrição, mas não consegue excluí-lo.
- [ ] Ao abrir o projeto devolvido, o Proponente cai na **primeira etapa devolvida**; as abas devolvidas têm o selo "Devolvido" (e "Alterado" depois de mexer). Nas outras abas aparece o aviso de que ficam só para consulta.
- [ ] Em Documentos, "Mostrar só o que falta ou foi devolvido" já vem ligado e lista os documentos devolvidos (com a observação da equipe), mesmo os que já têm arquivo.
- [ ] Na Revisão e envio, os itens devolvidos ainda não alterados são listados; reenviar sem alterá-los pede confirmação, citando-os.
- [ ] O Proponente corrige e reenvia. De volta à análise, **só os 2 itens** estão pendentes; o resto segue aprovado.
- [ ] Um arquivo novo exige ser visto de novo. Os itens devolvidos voltam a pendentes, sem reprovação.

### E. Enquadramento
- [ ] Com tudo aprovado, **Indicar departamento** abre o diálogo: só departamentos finalísticos; um sem Revisor ativo aparece desabilitado com o motivo.
- [ ] Indica o Financeiro. Os Supervisores recebem o e-mail (log) **sem CPF, telefone ou endereço**.
- [ ] O projeto some das ações do Analista (devolver, arquivar, aprovar) enquanto está pendente.
- [ ] **Revisor 1** vê o projeto em *Pendentes*; **Revisor 2** não vê o projeto nem na lista nem pelo endereço.
- [ ] **Revisor 1**, na aba *Documentos* do projeto pendente: cada arquivo (do checklist e dos adicionais) tem **Visualizar** ao lado de **Baixar**. Abre o arquivo no modal, sem decisão (só **Baixar original** e **Fechar**), com a lista lateral quando o documento tem vários arquivos. PDF, imagem e Word (convertido) aparecem; nada é baixado.
- [ ] **Analista** em análise: o **Visualizar** da aba *Documentos* abre o mesmo modal da Conferência, com **Aprovar** e **Reprovar**. Fora da análise (ou com enquadramento pendente) o modal é só de leitura.
- [ ] **Proponente**, nas abas *Documentos* e em projeto devolvido: **Visualizar** abre o próprio arquivo no modal, sem decisão e sem registrar "visto".
- [ ] Revisor 1 **recusa** com justificativa: o projeto volta ao Analista, aparece no histórico com a justificativa, o Supervisor recebe o e-mail. O Analista **não** recebe e-mail.
- [ ] O Analista indica o Jurídico (o diálogo lembra a última recusa). Revisor 2 **confirma**: etapa vira *Aguardando parecer*; o Proponente vê "Projeto enquadrado".

### F. Concorrência
- [ ] Dois Revisores do mesmo departamento, em dois navegadores, decidem ao mesmo tempo: um vence; o outro vê "Outra pessoa já decidiu" e a página recarrega.

### G. Saídas
- [ ] **Secretário nega** um projeto: o diálogo exige o motivo e avisa que arquiva na hora. Proponente, Analista, Supervisores (e o Revisor do departamento, se já havia) recebem o e-mail (log), uma vez cada.
- [ ] O Proponente **desiste** de outro projeto: todos são avisados.
- [ ] O Analista **arquiva por falta de documentos** um projeto sem enquadramento pendente.
- [ ] **Supervisor:** vê tudo e não tem nenhum botão de ação.

### H. Administração
- [ ] *Departamentos*: colunas de Revisores ativos e Projetos; "Sem revisor" nos finalísticos sem Revisor; a exclusão fica bloqueada com projeto vinculado.
- [ ] *Usuários*: dar o perfil Revisor a alguém sem departamento mostra o aviso (novo administrador, liberar acesso e página do administrador).

## Ao terminar

Anote o que falhou. O que for defeito entra como correção; o que for decisão entra em [debitos-tecnicos.md](debitos-tecnicos.md).
