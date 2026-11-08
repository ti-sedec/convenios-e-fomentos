# Planejamento: visualização do projeto pela equipe (resumo e completo)

**Escopo:** a tela do projeto vista pela equipe (`/dashboard/projetos/[id]` quando `project.access === ''`). Hoje ela reaproveita os formulários do proponente com os campos travados. A mudança troca isso por duas visualizações próprias, de leitura: um **Resumo** enxuto e uma visualização **Completa**. O cadastro e o envio do proponente não mudam.

## 1. Situação anterior

- `StaffProjectView` ([staff-project.tsx](../frontend/src/features/projects/components/staff/staff-project.tsx)) montava as abas Proponente, Instituição, Proposta, Recursos e Documentos com `ProponentStep`, `InstitutionStep`, `ProposalStep`, `FundingStep` e `DocumentsStep` em `readOnly`. Era o mesmo formulário do cadastro, só desabilitado.
- Todos os perfis da equipe (Analista, Revisor, Supervisor, Secretário) caíam nessa tela. O Revisor do Departamento só precisa decidir se o projeto se enquadra no departamento, e recebia cinco abas de cadastro.
- O `Summary` de `review-step.tsx` já era um resumo enxuto, mas privado e usado só na tela do proponente depois do envio.

## 2. Decisões

| # | Decisão | Origem |
|---|---|---|
| V1 | **Revisor** abre no **Resumo**. O **Completo** fica na aba "Projeto completo". | Pedido |
| V2 | **Analista, Supervisor, Secretário e Super Admin** passam a usar o **Completo** novo. Os formulários em leitura deixam de ser usados pela equipe. | Resposta 1 |
| V3 | O Resumo mostra identificação e proposta, e recursos. Do proponente, só nome e cargo. | Respostas 3 e 4 |
| V4 | Banner de situação e barra de ações (Confirmar/Recusar etc.) ficam acima das abas, para todos os perfis. Sem eles o Revisor não decide. | Premissa nossa |
| V5 | O Resumo mostra só a **contagem** de documentos. A lista com arquivos fica no Completo. O histórico de rodadas fica na aba Enquadramento. | Premissa nossa |
| V6 | O perfil "Revisor" é detectado por permissão: `can(reviewFraming) && !can(reviewItem)`. Quem tem as duas (Super Admin) vê o Completo. A regra fica num único lugar (`view-mode.ts`). | Técnica |

## 3. Componentes

Em `frontend/src/features/projects/components/project-view/`:

| Arquivo | Função |
|---|---|
| `primitives.tsx` | `ViewRow`, `ViewCard`, `ViewValue` (rótulo + valor, com "Não informado"). Extraídos do `Row` do review-step; o `Summary` do proponente passa a usá-los. |
| `project-summary.tsx` | Resumo enxuto. |
| `project-full.tsx` | Visualização completa em seções empilhadas com âncoras (`secao-proponente`, `secao-instituicao`, `secao-proposta`, `secao-recursos`, `secao-documentos`) e índice no topo. |
| `project-documents.tsx` | Lista de documentos somente leitura (checklist e adicionais), com Visualizar e Baixar. Reaproveita `ChecklistItemCard` (`editable=false`) e `ExtraDocuments`. |
| `view-mode.ts` | `staffViewModeOf(can)` devolve `'summary'` ou `'full'`. |

## 4. Mudanças no `StaffProjectView`

- Revisor: abas **Resumo** (padrão), **Enquadramento**, **Projeto completo**, **Histórico**.
- Demais: **Conferência** (se houver), **Enquadramento**, **Projeto completo**, **Histórico**.
- `?aba=completo` abre o Completo. O `ReviewPanel` do Analista ainda chama `onGoTab('proponente' | 'instituicao' | 'proposta' | 'recursos' | 'documentos')` ("Ver dados"): o `StaffProjectView` traduz a chave em aba `completo` + rolagem até a âncora da seção.
- O padrão do Analista continua sendo a Conferência e o do Revisor passa a ser o Resumo (antes: Enquadramento). O Revisor tem o Enquadramento a um clique, e o banner e os botões de decisão ficam sempre visíveis.
- Título da página (`pageDescription`) do Revisor: "Resumo do projeto para a decisão de enquadramento."

## 5. Etapas

1. Extrair as primitivas e fazer o `Summary` do proponente usá-las, sem mudar o visual.
2. Criar `ProjectSummary`.
3. Criar `ProjectDocuments` e `ProjectFull`.
4. Criar `view-mode.ts` e religar o `StaffProjectView`, incluindo a tradução do `onGoTab`.
5. Ajustar o título da página.
6. `bun run typecheck`, `lint`, `format:check`.
7. Atualizar `roteiro-aceite-fluxo-equipe.md` e a tabela de telas de `planejamento-fluxo-analise-enquadramento.md`.

## 6. Fora do escopo e pontos em aberto

- **A restrição do Resumo é visual.** `GET /api/admin/projects/{id}` continua devolvendo ao Revisor todos os dados do proponente (que o Completo mostra). O Resumo não os exibe, mas o Revisor os alcança no Completo e pela API. Esconder isso de fato exige decidir, com o cliente, se o Revisor pode ver contatos e dados pessoais; se não puder, o Completo dele também perde essas seções e o backend passa a omitir os campos para quem não tem `review-item` (com `openapi.yaml` e testes de matriz por perfil). **Não foi feito**, porque contradiz a decisão V1 de dar o Completo ao Revisor.
- A tela do proponente e dos membros convidados não muda.
- Sem suíte de testes de frontend: a verificação é `typecheck`/`lint` e o roteiro manual com os perfis do seed.

## 7. Verificação manual

| Perfil | Esperado |
|---|---|
| Revisor do departamento certo | Abre no Resumo (sem CPF, telefone e e-mail do proponente); botões Confirmar/Recusar visíveis; "Projeto completo" abre as seções e os documentos. |
| Revisor de outro departamento / sem departamento | Não abre o projeto (403/404, como hoje). |
| Analista | Abre na Conferência; "Ver dados" leva à seção certa no Completo; aprova documentos pelo visualizador. |
| Supervisor, Secretário | Abrem no Projeto completo; Secretário vê "Negar projeto". |
| Super Admin | Completo, com todas as ações. |
| Projeto arquivado | Banner de arquivamento e as duas visualizações continuam acessíveis. |
