# Imagem de PRODUÇÃO da API: binários estáticos em imagem distroless (sem shell, sem gerenciador de pacotes).
#
#   api      (padrão)  servidor HTTP em /api, mais /cli e /healthcheck
#   tools              mesmo /cli numa imagem Alpine, para rotinas agendadas que precisam de shell (docker-compose.prod.yml)
#
# O dev usa o Dockerfile.dev (air + código montado).

FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for c in api cli healthcheck; do \
      CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/$c ./cmd/$c || exit 1; \
    done

# --- Rotinas agendadas (cleanup): precisa de shell para o laço de repetição ---
FROM alpine:3.22.8 AS tools
RUN addgroup -S -g 65532 app && adduser -S -u 65532 -G app app
COPY --from=build /out/cli /usr/local/bin/cli
USER app
ENTRYPOINT ["/usr/local/bin/cli"]

# --- API (última etapa = alvo padrão) ---
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS api
COPY --from=build /out/api /out/cli /out/healthcheck /
# Certificados públicos de CA/servidor (ex.: X-Via, X_VIA_CA_FILE=/app/certs/xvia-dev.crt). Nunca chaves privadas.
COPY certs/ /app/certs/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]
