# Débito técnico: assinatura das declarações com o MT-ID

**Por que está pendente:** o cliente HTTP real do MT-ID exige acesso ao X-Via (rede do governo), e o desenvolvimento foi feito fora dela. Todo o resto funciona com um MT-ID simulado em memória (`backend/internal/mtid/memory.go`). **Nada foi testado contra o serviço real.** Contexto e decisões em [planejamento-declaracoes.md](planejamento-declaracoes.md); especificação do serviço em [documentacao-mti-app-cloud-mti-mt-gov-br-mt-id.md](documentacao-mti-app-cloud-mti-mt-gov-br-mt-id.md).

**Regra de desenho:** cada declaração é assinada separadamente e vira um arquivo separado (um registro, um pedido no MT-ID, um PDF por declaração).

## O que já está feito (bloco A)

| Peça | Onde |
|---|---|
| Textos oficiais, prévia, PDF, resumo `paramsSha256` | `backend/internal/project/declarations.go` |
| Aceite, pedido de assinatura, consulta, conclusão, cancelamento, declaração desatualizada | `backend/internal/project/declaration_service.go` |
| Tabela `project_declarations` (uma linha por emissão; no máximo uma ativa por documento) | migração `00018`, consultas em `database/queries/project_declarations.sql` |
| Contrato do MT-ID e versão simulada | `backend/internal/mtid/` |
| `PutObject` no armazenamento (grava o PDF gerado e o assinado) | `backend/internal/storage/` |
| Cinco declarações: contrapartida, não duplicidade, gratuidade, publicidade, compatibilidade de preços | `declarations.go`, `declaration_texts.go` |
| Dados pessoais de quem assina (nacionalidade, estado civil, endereço; sem RG), no perfil e no projeto | migração `00019`, `personal.go`; `personal-data-editor.tsx` |
| Rotas `GET`, `POST .../accept`, `DELETE` em `/admin/projects/{id}/declarations/{code}` | `handler.go`, `api/openapi.yaml` |
| Item do checklist marca `declarationStale` e volta a faltar se o cadastro mudar depois da assinatura (isso bloqueia o envio do projeto) | `markStaleDeclarations` |
| Tela: texto oficial, caixa de aceite, confirmação, situação da assinatura com atualização a cada 5 s | `frontend/src/features/projects/components/declaration-panel.tsx` |
| Testes de integração do fluxo completo | `backend/internal/app/project_declarations_test.go` |

**MT-ID simulado para o desenvolvimento:** `MTID_SIMULATE=true` (padrão no `docker-compose.dev.yml`; `false` no `backend/.env.example`) liga `mtid.Memory` em `cmd/api`, e cada pedido de assinatura é aprovado sozinho depois de `MTID_SIMULATE_DELAY` (padrão 10 s). A "assinatura" só acrescenta um carimbo de texto ao PDF e não tem validade alguma. A API recusa subir com isso em `APP_ENV=production`. Para ver o erro 503 de quando não há MT-ID, use `MTID_SIMULATE=false`. **Quando o cliente real existir, trocar o simulador por ele em `cmd/api/main.go` e remover o padrão `true` do compose.**

Sem MT-ID configurado (`app.WithMTID`), emitir responde `503 signature_unavailable` e a tela mostra o painel só com o que já foi registrado. O anexo manual do documento continua disponível.

## B. Só com acesso à rede do governo

1. **Cliente real** em `backend/internal/mtid/` (ex.: `client.go`) implementando `mtid.Client`, e ligá-lo em `backend/internal/app/app.go` (hoje só `WithMTID` injeta um cliente; falta criar o real a partir da configuração, como `NewStore`).
   - URL de desenvolvimento: `PRIVATE_URL/r1/mt-dev/GOV/MTI/SMARTPASS`; produção: `PRIVATE_URL/r1/MT/GOV/MTI/SMARTPASS`.
   - Headers: `x-road-client` (`mt-dev/GOV/MTI/MTCIDADAO` em desenvolvimento, `MT/GOV/MTI/MTCIDADAO` em produção) e `Authorization: Bearer` do MT Login.
   - Mapeamento: `Upload` → `POST /file-upload` (form-data, campo `file`, devolve `fileId`); `RequestSignature` → pedido com `userId` (CPF), `description`, `fileId`, `detached`, `coordinates`, `metadata`, e `signatureOnPages` (devolve `orderId`); `Status` → `GET /signature/{orderId}` (`PENDING`/`APPROVED`, com `fileId` e `signedIn`); `Cancel` → `DELETE /signature/{orderId}`; `Download` → `GET /file-download/{fileId}`.
   - Variáveis novas em `.env.example` e `internal/config`: URL do X-Via, `x-road-client`, ambiente. Sem elas, o cliente real não é criado e o comportamento atual (503) permanece.
2. **Token e escopo.** Hoje o escopo do login é `openid profile email` (`frontend/src/lib/auth/oidc.ts`). O MT-ID exige `signature-requester`: cadastrá-lo nos modelos de cliente do Keycloak e pedir `scope=openid profile email signature-requester`. Decidir se o backend usa o token do usuário (como a documentação descreve; hoje a API só valida o token que o BFF repassa, e `AcceptDeclaration` não o recebe) ou uma credencial de sistema. Se for o token do usuário, `mtid.Client` precisa receber o token por chamada; o contrato atual não prevê isso.
3. **Estados desconhecidos.** A documentação só mostra `PENDING` e `APPROVED`. Descobrir como o MT-ID informa recusa, vencimento e cancelamento, e mapear para `mtid.StateRejected` e `mtid.StateExpired`. Enquanto o cliente real tratar qualquer outro estado como pendente, um pedido recusado ficaria "aguardando" para sempre.
4. **Descobrir o que a documentação não diz** (perguntas à MTI no planejamento):
   - `detached: false` devolve, em `fileId`, o PDF já assinado? O código assume que sim (`mtid.Request.Detached = false`; `completeDeclaration` baixa `Status.FileID` e exige que comece com `%PDF-`). Se vier em arquivo separado (`signaturePEM`), é preciso guardar a assinatura ao lado do PDF e ajustar `completeDeclaration`.
   - Nível da assinatura (Lei 14.063/2020), validade, prazo do pedido pendente e como a SEDEC verifica o arquivo depois.
5. **Posição da assinatura.** Calibrada em 10/11/2026 contra o MT-ID de desenvolvimento: `x` é a borda esquerda e `y` o centro vertical do carimbo (cerca de 217 x 28 pt), em pontos e a partir do **topo** da página (a primeira é 0). O campo de assinatura do PDF tem 260 x 80 pt. Como cada declaração tem um texto de tamanho diferente, a posição sai do layout em `renderDeclaration` (`signatureSpotAt`, centralizada entre a data e o nome do responsável), não de constante. Falta conferir visualmente as outras quatro declarações no PDF assinado.
6. **Testes ponta a ponta** no ambiente de desenvolvimento do X-Via com o MTCidadão: assinatura aprovada, recusada, expirada, cancelada; queda do serviço (o painel mostra o erro e o anexo manual segue disponível).
7. **Erros e limites:** timeouts nas chamadas (o `Context` já chega até o cliente), repetição segura, e o que mostrar quando o signatário não tem o MTCidadão com o CPF vinculado.
8. **Atualização da situação.** Hoje o MT-ID é consultado quando alguém abre a declaração (o painel faz isso a cada 5 s enquanto está aberto). Se ninguém abrir a página, a assinatura só entra no checklist na próxima visita. Se isso incomodar, criar uma tarefa periódica (como `cleanup` na CLI) que percorra `project_declarations` com `status = 'awaiting_signature'` e chame `refreshDeclaration`.

## A. Ainda pendente e não depende da rede

- **Perguntas da SEDEC sobre os textos** (10 a 13 do planejamento): data do evento na gratuidade (hoje sai a vigência), texto da publicidade (cita "museu" e "Plataforma Mapas": foi mantido **literal**, porque é texto oficial, mas parece resto de outro modelo), "convênio" no lugar de "termo de fomento" na compatibilidade de preços, e a ausência do RG. Cada ajuste de texto é uma nova `version` do modelo em `declarations.go`/`declaration_texts.go`; as declarações já emitidas ficam como estão.
- **Contrapartida em bens e serviços e entidades que não são município** continuam bloqueadas (sem modelo oficial).
- **Verificar no navegador:** a tela foi checada por tipos, lint e formatação, mas **não foi aberta num navegador**. Com `docker compose -f docker-compose.dev.yml up --build` o MT-ID simulado já vem ligado (ver abaixo); falta só conferir o diálogo e o painel.
- **Registro de auditoria na interface:** as ações (`project.declaration-request`, `-cancel`, `-signed`, `-rejected`, `-expired`) já são gravadas, mas nenhuma tela as mostra.

## C. Depende de resposta da SEDEC

Ver as perguntas do [planejamento](planejamento-declaracoes.md): aceite do PDF sem papel timbrado, assinatura eletrônica no lugar da manuscrita, regra da gratuidade para projeto sem evento, texto da publicidade (menção a "museu" e "Plataforma Mapas"), modelo para contrapartida em bens e serviços e para entidade que não é município, e a retirada do RG dos modelos.
