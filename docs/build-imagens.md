# Build das imagens no GitHub (GHCR)

Os servidores **não compilam mais nada**. O GitHub Actions constrói as imagens e as publica no GitHub Container Registry; o Coolify (hml e produção) só baixa as imagens prontas. Motivo: o `next build` precisa de ~2 GB de RAM livres e o servidor de homologação não tem folga para isso.

```
git push develop ─┐                            ┌─> Coolify (hml): baixa :hml
                  ├─> GitHub Actions ──> GHCR ─┤
git push main ────┘   (build-images.yml)        └─> Coolify (prod): baixa :prod
```

## Imagens

Cinco imagens em `ghcr.io/ti-sedec/`, todas `linux/amd64`:

| Imagem | Origem | Usada por |
|---|---|---|
| `convenios-api` | `backend/Dockerfile`, alvo `api` | `convenios-api`, `migrate`, `seed` |
| `convenios-tools` | `backend/Dockerfile`, alvo `tools` | `cleanup` |
| `convenios-web` | `frontend/Dockerfile` | `convenios-web` |
| `convenios-proxy` | `haproxy/Dockerfile` | `convenios-proxy` |
| `convenios-backup` | `ops/backup/Dockerfile` | `backup` |

Tags de cada imagem: `:hml` (branch `develop`), `:prod` (branch `main`) e `:sha-<7 primeiros do commit>` (uma por build, para fixar ou voltar a uma versão). A stack escolhe a versão com **`IMAGE_TAG`** (padrão `prod`); no `.env` do hml use `IMAGE_TAG=hml`.

> **O `web` é uma imagem por ambiente.** O `NEXT_PUBLIC_APP_URL` é embutido no build do Next (`metadataBase`), então a imagem `:hml` só serve para o domínio de hml e a `:prod` só para o de produção. As demais imagens são idênticas entre ambientes.

## Configuração (uma vez)

1. **Variáveis do repositório** (Settings > Secrets and variables > Actions > *Variables*):
   - `PROD_APP_URL`: origem pública da produção, em https e sem barra final. Opcional: o padrão é `https://convenios-fomento.sedec.mt.gov.br`. Só defina se o domínio mudar.
   - `HML_APP_URL`: opcional; o padrão é `https://convenios-fomento-hml.sedec.mt.gov.br`.
2. **Primeiro push**: rode o workflow (push em `develop` ou *Actions > Build das imagens > Run workflow*). Os pacotes nascem **privados**.
3. **Tornar os pacotes públicos** (uma vez por pacote): GitHub > organização `ti-sedec` > *Packages* > cada `convenios-*` > *Package settings* > *Change visibility* > Public. O repositório é público, então não há segredo nas imagens, e assim o Coolify e o Portainer baixam sem credencial. (Se preferir manter privadas, é preciso um *registry login* com um PAT `read:packages` em cada servidor.)
4. Se a organização restringir a publicação de pacotes públicos, libere em *Organization settings > Packages*.

## Deploy automático no hml (Coolify)

O firewall da rede do servidor só deixa o GitHub chegar na porta 80, então o workflow não consegue chamar a API do Coolify (portas 8000/443). Em vez de abrir uma rota nova com token, o **servidor é quem pergunta**: um timer do systemd roda [`ops/coolify-autodeploy/autodeploy.sh`](../ops/coolify-autodeploy/autodeploy.sh) a cada 2 minutos, consulta a API pública do GitHub e, quando há uma execução bem-sucedida de um commit novo em `develop`, chama a **API local** do Coolify (`127.0.0.1:8000`). Nada novo fica exposto.

Instalação na VM (como root):

```bash
apt-get install -y jq curl
install -m 0755 ops/coolify-autodeploy/autodeploy.sh /usr/local/bin/convenios-autodeploy
install -m 0644 ops/coolify-autodeploy/convenios-autodeploy.{service,timer} /etc/systemd/system/

install -m 0600 /dev/null /etc/convenios-autodeploy.env
cat > /etc/convenios-autodeploy.env <<'EOF'
COOLIFY_TOKEN=<token de API do Coolify com permissão de deploy>
COOLIFY_APP_UUID=<uuid da aplicação>
BRANCH=develop
EOF

systemctl daemon-reload
systemctl enable --now convenios-autodeploy.timer
journalctl -u convenios-autodeploy -f      # acompanhar
```

No Coolify, na aplicação:

- **Desligue o deploy automático no push** (*Advanced > Auto Deploy*) e/ou remova o evento *push* do webhook do GitHub. Senão o Coolify faria o deploy no instante do push, antes de o workflow terminar de publicar as imagens, e o deploy certo só viria minutos depois (o resultado final é correto, mas há um deploy a mais).
- Acrescente `IMAGE_TAG=hml` às variáveis de ambiente.

Fluxo resultante: push em `develop` → o workflow publica as imagens (~5 min na primeira vez, menos com cache) → em até 2 minutos o timer vê o commit novo → o Coolify baixa as imagens `:hml` (`pull_policy: always`) e recria os serviços. O `docker-compose.prod.yml` também vem do commit mais novo, pois o Coolify clona o repositório a cada deploy.

Para forçar um deploy sem push novo: apague `/var/lib/convenios-autodeploy/last-sha` e rode `systemctl start convenios-autodeploy`, ou use o botão *Deploy* do painel.

## Produção (Coolify)

A produção também roda no Coolify (VM própria, painel em `http://201.49.171.163:8000`), com a mesma stack, `IMAGE_TAG=prod` e a branch `main`. O deploy automático é o mesmo timer do hml, instalado com `BRANCH=main` e o `COOLIFY_APP_UUID` da aplicação de produção (o `/etc/convenios-autodeploy.env` é por servidor). O Coolify lê o certificado público do X-Via de produção em `backend/certs/xvia.crt` (nome conferido: `XV-SS-SEDEC`, não o host da URL).

Se o seu ambiente de produção for o Portainer: o `main` não tem deploy automático; espere o workflow da `main` terminar (aba *Actions*) e, na stack, **Pull and redeploy** com *Re-pull image* ligado (o compose já tem `pull_policy: always`).

**Voltar atrás**: ponha `IMAGE_TAG=sha-<commit anterior>` nas variáveis da stack e refaça o deploy (as migrações não são desfeitas sozinhas; veja [deploy-producao.md](deploy-producao.md)).

## Construir localmente (sem GitHub)

Para testar uma alteração no Dockerfile antes do push, ou num servidor sem acesso ao GHCR:

```bash
docker compose -f docker-compose.prod.yml -f docker-compose.prod.build.yml up -d --build
```

O override [`docker-compose.prod.build.yml`](../docker-compose.prod.build.yml) restaura os `build:`. Aqui o `next build` volta a precisar de ~2 GB de RAM livres.

## Problemas comuns

| Sintoma | Causa e solução |
|---|---|
| `pull access denied` / `unauthorized` no servidor | O pacote ainda está privado: torne-o público (acima). |
| `manifest unknown` | A tag ainda não existe: o workflow da branch não terminou (ou falhou). Veja a aba *Actions*. |
| Build do `web` falha com "APP_URL inválida" | `PROD_APP_URL` ou `HML_APP_URL` malformada no repositório (precisa ser origem https, sem barra final). |
| Mudou o domínio | Altere `PROD_APP_URL`/`HML_APP_URL` e rode o workflow de novo (o `web` é reconstruído com o endereço novo). |
| O timer não dispara o deploy | `journalctl -u convenios-autodeploy`. Causas típicas: token do Coolify inválido, `COOLIFY_APP_UUID` errado, API do Coolify desabilitada, limite de 60 consultas/hora da API do GitHub sem token (o timer usa 30/h). |
