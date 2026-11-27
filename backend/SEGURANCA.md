# Revisão de segurança e checklist de pendências

Revisão feita ao final da Fase 8, sobre o código e os testes existentes. Complementa o [README](README.md).
Convenção: ✅ verificado (há teste ou verificação automatizada) · 🔧 corrigido nesta revisão · ⚠️ risco aceito/limitação · ⏳ pendente.

## 1. Escopo e método

- Leitura dirigida do código por área (autenticação, autorização, entrada/saída, banco, segredos, logs, infraestrutura).
- `golangci-lint` com `gosec`, `errorlint` e `noctx` (sem avisos) e `govulncheck` nas dependências.
- Todos os achados abaixo que puderam virar teste viraram teste de regressão.
- Verificação de ponta a ponta com a imagem Docker real (migração → API → `/api/ready`, `/api/docs`, `seed`, `cleanup`).
- **Não** foi feito: teste de intrusão externo, fuzzing, análise de carga nem revisão por terceiros.

## 2. Resultado por área

### Autenticação
**Administradores (MT Login / OIDC):**
- ✅ Sem senha local: a API só valida o access token do MT Login (RS256 pelas chaves do JWKS, `iss` e `exp` obrigatórios, tipo `Bearer`,
  client conferido por `azp`/`aud`). Algoritmo `none`/HS256, tokens de ID ou refresh, de outro emissor, de outro client, expirados ou
  assinados por outra chave são rejeitados (testes em `auth/oidc_test.go` e `app/admin_auth_test.go`).
- ✅ Identidade = CPF validado (dígitos verificadores) vindo do token assinado; não há vínculo por e-mail, então não há tomada de conta por
  e-mail não verificado.
- ✅ Primeiro acesso cria o administrador **sem nenhuma role** (nada é liberado automaticamente), de forma atômica (`ON CONFLICT`) e auditada.
- ✅ Bloqueio e desativação valem na hora: cada requisição confere a conta no banco (sem cache).
- ✅ CPF é dado pessoal: mascarado em listagens e na auditoria, completo apenas para o próprio usuário em `/api/admin/auth/me`; não vai para logs.
- ⚠️ **Qualquer pessoa que autentique no client do MT Login ganha uma conta de administrador sem permissões.** Se o realm for aberto a
  cidadãos, a tabela `admins` acumula contas sem perfil; restrinja o client no Keycloak (role/grupo) ou exija um claim para criar o registro.
- ⚠️ Encerrar a sessão no MT Login não derruba, na hora, o access token já emitido (até a expiração dele, curta). Desativar a conta aqui
  tem efeito imediato.
- ⚠️ MFA, bloqueio por tentativas e política de senha são do MT Login (Keycloak), não desta aplicação.

**Contas locais, usuários finais e tokens de API não existem mais** (removidos pela migração `00043`): a única porta de entrada é o MT Login. Isso tirou da superfície
de ataque o cadastro aberto, o login com senha, o "esqueci a senha" e os tokens `gat_`.

### Autorização
- ✅ Ação não registrada sempre nega; Super Admin também.
- ✅ Filtros de condição são tipados e viram SQL somente parametrizado e apenas com colunas do `Schema`; a injeção em `sort`, `q`, `status`
  e nos filtros de auditoria foi testada.
- ✅ Campos: leitura remove o não permitido (chaves fora do schema nunca saem); escrita recusa (422) campos não permitidos e **todos** os
  campos de sistema, inclusive para o Super Admin.
- ✅ Registro único (leitura/edição/exclusão/publicação) é conferido contra o filtro da permissão (403 fora dele).
- ✅ Só um Super Admin concede/retira a role Super Admin ou convida com ela; ninguém desativa/exclui a própria conta; o último Super Admin
  ativo é protegido por lock consultivo (testado com requisições concorrentes).
- ✅ Cada rota de `/admin` tem teste de 401 (sem token) e 403 (sem permissão).
- 🔧 Um teste unitário revelou (Fase 4) que `fields: []` ("nenhum campo") virava `null` ("todos os campos") ao normalizar — corrigido, com regressão.
- ✅ **Sem escalonamento de privilégio** (quem não é Super Admin): só concede ou amplia permissões que possui (ação, campos e condições:
  `authz.Covers`/`Widened`, 403 `privilege_escalation` com a lista do que faltou); só atribui, pré-cadastra ou duplica roles cujas permissões
  cobre; não altera as permissões de uma role que possui (409 `cannot_modify_own_role`) nem os perfis ou a lotação da própria conta
  (409 `cannot_modify_self`); e não age sobre administradores com permissões que não possui (403 `target_more_privileged`). Remover
  permissões é sempre permitido. Cada regra tem teste de integração (`TestEscalonamentoDePrivilegioEBloqueado`) e unitário (`covers_test.go`).
- ⚠️ Os seletores do painel liberam leituras auxiliares a quem administra o recurso (lista de departamentos e de roles com `users.read`).
  Elas expõem só nomes e contagens; as permissões de cada role continuam exigindo `settings.roles.read`.

### Entrada e saída
- ✅ Corpo limitado (`MAX_BODY_BYTES`, 413); JSON rejeita campos desconhecidos; validação com mensagens em português.
- ✅ Erros 500 nunca vazam detalhes (só o `requestId`); pânicos são capturados.
- ✅ Cabeçalhos de e-mail recusam quebra de linha (sem injeção de cabeçalho).
- 🔧 `X-Request-ID` do cliente agora é validado (`[A-Za-z0-9._-]{1,64}`); antes qualquer texto de até 64 bytes era refletido em cabeçalho e log.
- 🔧 Todas as respostas levam `Cache-Control: no-store` (dados dinâmicos/pessoais não devem ir para caches compartilhados).
- ✅ CSRF não se aplica: a autenticação é por cabeçalho `Authorization`, sem cookies. CORS restrito às origens configuradas.

### Disponibilidade
- 🔧 Limite geral por IP em todas as rotas (`RATE_LIMIT_GLOBAL_PER_MINUTE`, padrão 600).
- ✅ Timeouts do servidor HTTP e corpo limitado (anti-DoS).
- ✅ IP real só via `X-Forwarded-For` quando a conexão vem de um proxy em `TRUSTED_PROXIES`.
- ⚠️ O rate limit é em memória, por instância. Com várias instâncias o limite efetivo é multiplicado; use também o proxy.
- 🔧 O comando `cleanup` (`make cleanup`) remove os e-mails já enviados e os envios de arquivo nunca confirmados.

### Banco de dados
- ✅ SQL via sqlc; SQL montado à mão em apenas três listagens, com valores em `$n` e colunas/ordenação de listas permitidas.
- ✅ Chaves estrangeiras com `ON DELETE` coerente, `CHECK`s de domínio (status, tipos, arrays JSON) e índices únicos parciais.
- ✅ Operações compostas em transação (substituição de permissões, exclusão de role movendo usuários, convite, lote de exclusões).
- ⚠️ Sem criptografia em repouso, backups ou TLS para o PostgreSQL por conta da aplicação: configure `sslmode=verify-full` na `DATABASE_URL` em produção.

### Segredos e logs
- ✅ A aplicação não guarda segredo de assinatura nem sal: não há JWT nem token próprio, só a validação dos tokens do MT Login pelo JWKS.
- ✅ O log de acesso registra só método, caminho (sem query string), status e duração; senhas, tokens e hashes nunca são registrados.
- ✅ Auditoria sem senhas, tokens nem hashes.
- ⚠️ Em desenvolvimento, o `LogMailer` registra o corpo dos e-mails (com links). Isso só ocorre fora de produção; em produção sem SMTP os e-mails são descartados sem o corpo.

### Dependências
- ✅ `govulncheck`: nenhuma vulnerabilidade alcançável. Atualizados `golang.org/x/crypto`, `moby/go-archive` e `moby/sys/user`
  (esta última pelo `testcontainers`, só em testes).
- ⚠️ Restou o aviso GO-2026-5932 (`x/crypto/openpgp`, sem correção): a aplicação **não importa** esse pacote.
- Reexecute `govulncheck ./...` e `go list -m -u all` periodicamente (ex.: no CI).

### Arquivos dos projetos (MinIO/S3)
- ✅ Bucket privado; nenhum acesso anônimo. Envio e download só por URLs pré-assinadas de curta duração (envio 15 min, download 2 min), emitidas depois da
  checagem de permissão sobre o **projeto** (condições por registro valem para os arquivos).
- ✅ O envio usa POST policy: o armazenamento recusa tipo diferente do declarado, tamanho acima do limite (PDF/DOCX 200 MB; imagens 25 MB), troca da
  chave do objeto e uso fora do prazo. A chave do objeto é gerada pelo servidor (derivada do projeto e do arquivo), nunca derivada do nome do cliente, e não sai da API.
- ✅ A confirmação confere existência, tipo, tamanho real e a assinatura do formato (magic bytes); o que não passa é apagado. Downloads saem como
  `attachment`, com o tipo gravado no servidor,; só se baixam arquivos confirmados.
- ✅ Excluir o documento ou o arquivo apaga os objetos; envios nunca confirmados são removidos pelo `cleanup` após 24 h.
- ⚠️ Fora a assinatura do formato, a varredura do conteúdo é opcional (ClamAV, `CLAMAV_ADDR`), ver "Projetos dos proponentes".
- ⚠️ O console do MinIO só é exposto (`/minio-console`) no HAProxy de desenvolvimento; não o publique em produção e troque `MINIO_ROOT_USER/PASSWORD`.

### Projetos dos proponentes (convênios e termos de fomento)
- ✅ **Acesso por registro**: todas as rotas de `/admin/projects/{id}/…` passam pelo motor de permissões com a condição "é o criador" (`admin::is-creator`);
  proponente alheio recebe 403/404 em todas as rotas, inclusive rascunho, enviado, arquivos, comprovante e ações da equipe (matriz testada rota a rota em
  `TestIDORDeTodasAsRotasDeProjeto`). Não existe rota que aceite um id de arquivo sem o id do projeto: o arquivo é sempre buscado `WHERE id AND project_id`.
- ✅ **Mass assignment**: corpos com campos desconhecidos (`status`, `code`, `createdById`…) são recusados (400); status, etapa e código só mudam por ações próprias do servidor.
- ✅ **Concorrência**: cada seção é gravada com `version` otimista (409 em conflito); transições (enviar, retirar, arquivar, complementar) travam a linha do projeto
  (`FOR UPDATE`); o código AAAA-NNNNNN vem de contador por ano, sem repetição. Testado com 8 gravações simultâneas na mesma versão (só 1 vence) e 30 pedidos
  simultâneos de envio de arquivo (exatamente o limite do documento).
- 🔧 **Deadlock do pool encontrado nos testes de carga**: dentro de uma transação com o projeto travado, a leitura dos parâmetros de regra pedia *outra* conexão ao
  pool; com o pool cheio de transações esperando o lock, quem segurava o lock nunca conseguia a conexão e a API travava. Corrigido (`rules.SnapshotAt` usa a
  transação do chamador). **Regra do código**: dentro de `database.WithTx` só se usa o `q` da transação, nunca `s.q`/`s.pool`.
- ✅ **Injeção**: busca e filtros usam parâmetros `$n` (curingas `%`/`_`/`\` escapados); ordenação só por colunas permitidas; testado com `' OR 1=1 --`, `DROP TABLE`, unicode e 5 000 caracteres.
- 🔧 O caractere NUL (`\u0000`, `%00`) derrubava a requisição com 500, porque o PostgreSQL não o aceita em texto. Agora o corpo JSON com NUL em qualquer string
  é 422 e a URL/query com NUL é 400 (`httpapi.RejectNUL`, `httpapi.Decode`).
- ✅ E-mails com quebra de linha são recusados (sem injeção de cabeçalho); campos livres são guardados e devolvidos como texto (o front escapa na renderização);
  o PDF do comprovante é montado a partir do retrato imutável da versão enviada e foi testado com acentos, emoji, CJK, barras e parênteses.
- ✅ Versões enviadas são imutáveis (trigger bloqueia `UPDATE` em `project_versions`); o aceite da declaração guarda data, hora, IP e texto.
- 🔧 **Taxas por usuário** (chave = id do administrador, não o IP, por isso um IP compartilhado como o de uma prefeitura não esgota a cota alheia):
  envio e confirmação de arquivos `RATE_LIMIT_UPLOAD_PER_MINUTE` (padrão 120; 0 desliga); enviar, reenviar, comprovante e duplicar 20/min. Excesso → 429.
- 🔧 **Limites por projeto**: 10 arquivos por documento, 20 envios em andamento, 300 arquivos e 2 GiB no total (409 `too_many_pending`, `project_files_limit`,
  `project_storage_full`). Impede que um único proponente encha o armazenamento.
- 🔧 **Integridade e antivírus**: na confirmação o servidor lê o objeto, calcula o **SHA-256** (gravado em `project_files.sha256` e no retrato da versão) e confere
  o tamanho. Com `CLAMAV_ADDR` definido, o mesmo fluxo de bytes passa pelo ClamAV (protocolo INSTREAM): arquivo infectado é recusado (422), apagado do
  armazenamento e registrado na auditoria (`project.file-infected`, com a assinatura). **Falha fechada**: se o scanner estiver fora do ar, a confirmação responde 503
  `scan_unavailable`, o objeto fica pendente e nada é liberado; o proponente tenta de novo. Sem `CLAMAV_ADDR` o hash continua sendo calculado, mas não há varredura.
  Configure o `clamd` com `StreamMaxLength` ≥ 200M (o maior PDF aceito).

### Compartilhamento do projeto por CPF
- ✅ **Só o titular concede**: `admin::projects.share` com a condição "é o criador"; convidado (mesmo com edição), outro proponente e a equipe sem a permissão recebem 403/404 em listar, convidar, alterar e revogar (matriz de IDOR em `TestIDORDeTodasAsRotasDeProjeto` e `TestSoOTitularGereOAcesso`).
- ✅ **Acesso conferido a cada requisição, pelo CPF da conta** (não por um vínculo gravado no login): revogação vale na hora com a sessão aberta (`TestRevogacaoValeComASessaoAberta`) e não existe estado "meio ativado" para explorar. Leitura nunca altera: `update`/`submit` só passam pela condição de edição; excluir e desistir só pela de criador.
- ✅ **Sem oráculo de CPF**: a API nunca diz se um CPF tem conta nem devolve o nome; o CPF sai mascarado na lista e na auditoria. "Convite pendente" vale tanto para quem não tem conta quanto para quem nunca abriu o projeto, então não distingue os dois casos.
- ✅ Limite de 10 pessoas por projeto, rótulo de até 100 caracteres, nível validado (`read`/`edit`), corpos com campos desconhecidos recusados, convite ao próprio titular recusado, duplicidade 409 (também com o CPF formatado).
- ✅ **Aviso do convite por e-mail**: vai para o e-mail da conta que tem o CPF (lido na hora e nunca devolvido ao titular) ou, sem conta, para o e-mail informado (validado, sem quebra de linha, guardado em minúsculas e devolvido mascarado). O texto traz só o CPF mascarado. Conta bloqueada ou inativa não recebe. O titular pode usar o campo para mandar e-mail a um terceiro qualquer, mas o texto é fixo (sem conteúdo livre), o convite é limitado a 20 por minuto por usuário (429) e é auditado.
- ⚠️ O convidado com edição **envia o projeto em nome do titular** (decisão do órgão). A declaração de veracidade registra quem aceitou (id, IP e data) e a auditoria identifica o autor; a responsabilidade perante a SEDEC é do titular.
- ✅ Não existe troca de titularidade (decisão do órgão): não há rota nem tela, e o titular é sempre `projects.created_by_id`.

### LGPD (dados pessoais dos projetos)
- **Dados tratados**: CPF, nome, cargo, telefone e e-mail do proponente e do representante legal; CNPJ e endereço da instituição; documentos anexados (podem conter
  dados pessoais de terceiros, p. ex. certidões e procurações). Base legal: execução de política pública / obrigação legal (Lei 13.019/2014, IN 001/2015 e 001/2016).
- 🔧 **Minimização na exibição**: quem não é o criador do projeto (equipe) vê o CPF **mascarado** (`***.123.456-**`) no proponente e no representante legal; as
  listagens nunca trazem CPF. O CPF completo só aparece para o titular e para quem ele convidou (e continua no retrato imutável da versão enviada, para fins de prova).
- 🔧 **Rastreabilidade**: download de arquivo (`project.file-download`) e do comprovante (`project.receipt-download`) são auditados com autor, projeto, documento e arquivo;
  tentativas negadas não geram registro de download. Os logs de acesso não gravam a query string, então termos buscados (que podem ser CPF/CNPJ) não vão para o log.
- 🔧 **Retenção de rascunhos**: `cli cleanup --draft-days N` apaga rascunhos (e seus arquivos) sem alteração há mais de N dias e audita cada exclusão (`project.draft-expired`).
  Desligado por padrão (0). Sugestão: 365 dias, com aviso ao proponente antes de ativar.
- ⏳ **Pendente de decisão do órgão**: prazo de guarda dos projetos enviados e arquivados e de seus arquivos (hoje ficam indefinidamente, por serem processo administrativo);
  atendimento a pedidos de titulares (acesso/correção/eliminação) e o encarregado (DPO) que responderá por eles.
- ⚠️ O hash e o antivírus não detectam documento falso ou adulterado de forma legítima; a conferência de conteúdo continua sendo da equipe da SEDEC.

### Infraestrutura
- ✅ Imagem multi-stage, binário estático em `distroless/static:nonroot` (sem shell, sem gerenciador de pacotes).
- ⚠️ O `/api/docs` carrega a UI (Scalar) de um CDN, sem SRI, e fica público; a CSP da página libera só esse CDN. Em produção, bloqueie
  `/api/docs` no proxy se a documentação não deve ser pública, ou sirva a UI localmente.
- ⚠️ `Strict-Transport-Security` é enviado sempre, mas só tem efeito em HTTPS: termine o TLS no proxy.

## 3. Riscos aceitos e limitações conhecidas

| Tema | Descrição | Mitigação / quando tratar |
|---|---|---|
| E-mail síncrono | O envio SMTP acontece dentro da requisição; falha de envio é registrada no log, mas não chega ao cliente. | Fila/worker de e-mail quando o volume pedir. |
| Auditoria | Sem retenção/rotação, sem assinatura de integridade; o conteúdo dos projetos não é auditado. | Política de retenção e arquivamento conforme a necessidade. |
| Antivírus opcional | Sem `CLAMAV_ADDR` os arquivos dos projetos não são varridos (só têm hash e checagem de formato). | Em produção, subir um `clamd` e configurar `CLAMAV_ADDR`; a confirmação falha fechada se ele cair. |
| Arquivo dentro do limite, mas só confirmado depois | Entre o envio ao armazenamento e a confirmação o objeto existe sem varredura; ele só passa a valer (e a poder ser baixado) depois de confirmado. | Aceito; envios não confirmados são apagados pelo `cleanup` após 24 h. |
| Retenção de projetos enviados | Sem prazo de guarda definido. | Definir com a SEDEC/jurídico; ver "LGPD". |
| Rate limit por instância | Ver "Disponibilidade". | Redis ou proxy quando houver várias instâncias. |

## 4. Checklist de pendências

**Funcionalidades fora do escopo definido** (decisões já documentadas no README):
- ⏳ i18n/locales: preparado (`SupportsLocales`, `properties`), sem conteúdo localizado.
- ✅ Frontend administrativo: usuários, perfis e permissões, departamentos e auditoria.

**Melhorias identificadas (nenhuma bloqueia o uso):**
- ⏳ Remover `admins.password_hash` e a tabela `admin_sessions` (sem uso desde o login pelo MT Login) numa migração, depois de validado o novo fluxo.
- ✅ Bloquear/desbloquear administradores (`POST /admin/users/{id}/block|unblock`).
- ⏳ Opção para desabilitar ou servir localmente o `/api/docs`.
- ⏳ Fila de e-mails; retenção da auditoria; MFA para administradores.
- ⏳ Pipeline de CI executando `make lint`, `make test`, `make cover-authz` e `govulncheck`.
- ⏳ Métricas/observabilidade (Prometheus, traces); hoje há logs JSON com `request_id` e `/api/ready`.
- ⏳ `make dev` depende do `air`, que é opcional e não vem instalado.

## 5. Checklist para colocar em produção

1. `APP_ENV=production`.
   `JWKS_URI`, `OIDC_ISSUER`, `OIDC_CLIENT_ID` e `OIDC_CPF_CLAIM` do MT Login de **produção** (a API não sobe sem eles); no frontend, as `MTLOGIN_*`
   de produção, com a URL de retorno e a de pós-logout cadastradas no client.
2. `DATABASE_URL` com `sslmode=verify-full`; usuário do banco com o mínimo de privilégios; backups agendados.
3. TLS no proxy reverso, `TRUSTED_PROXIES` com o CIDR dele e `CORS_ALLOWED_ORIGINS` só com o(s) domínio(s) do front.
4. `PUBLIC_URL` correto e SMTP configurado (`SMTP_*`); sem SMTP os e-mails são descartados.
5. Criar o primeiro Super Admin pela CLI (`cli admin:create --cpf ...`) ou `SEED_SUPER_ADMIN_CPF`.
6. Rodar `cli migrate:up` antes de subir a nova versão; **não** rodar o `seed` em produção, a menos que se queira criar as roles de exemplo
   (os usuários de exemplo só existem com `APP_ENV=development`).
7. Agendar `cli cleanup` (diário; com `--draft-days` para a retenção de rascunhos, se adotada) e acompanhar `GET /ready`.
   Subir um `clamd` e configurar `CLAMAV_ADDR` (com `StreamMaxLength` ≥ 200M); ajustar `RATE_LIMIT_UPLOAD_PER_MINUTE` ao perfil de uso.
8. Decidir sobre a exposição de `/api/docs` e revisar as permissões das roles (principalmente `settings.roles.*` e `settings.users.*`).
9. Rodar `govulncheck ./...` e a suíte completa no pipeline antes de cada release.
