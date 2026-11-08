# Imagem de PRODUÇÃO do frontend: build do Next.js em modo "standalone" (servidor Node mínimo, sem node_modules inteiro).
# O dev usa o Dockerfile.dev (Bun + next dev com HMR). Este arquivo substitui o do template, que era do Clerk.
#
# Contexto de build: esta pasta (frontend/). Argumentos de build:
#   NEXT_PUBLIC_APP_URL  origem pública (https://...). É EMBUTIDA no build: mudar o domínio exige reconstruir.
# Tudo o mais (API_URL, MTLOGIN_*) é lido em tempo de execução.

ARG NODE_VERSION=24-slim

# --- 1. Dependências (Bun resolve pelo bun.lock; o build roda no Node) ---
FROM node:${NODE_VERSION} AS deps
WORKDIR /app
COPY --from=oven/bun:1 /usr/local/bin/bun /usr/local/bin/bun
COPY package.json bun.lock ./
# --ignore-scripts: o "prepare" do projeto instala o husky (hooks do Git), inútil e sem .git na imagem.
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile --ignore-scripts

# --- 2. Build ---
FROM node:${NODE_VERSION} AS builder
WORKDIR /app
COPY --from=oven/bun:1 /usr/local/bin/bun /usr/local/bin/bun
COPY --from=deps /app/node_modules ./node_modules
COPY . .

ARG NEXT_PUBLIC_APP_URL
ENV NEXT_PUBLIC_APP_URL=${NEXT_PUBLIC_APP_URL} \
    NEXT_PUBLIC_SENTRY_DISABLED=true \
    NEXT_TELEMETRY_DISABLED=1 \
    BUILD_STANDALONE=true \
    NODE_ENV=production
# "prebuild" copia os arquivos do pdf.js para public/pdfjs; depois vem o next build.
RUN bun run build

# --- 3. Execução ---
FROM node:${NODE_VERSION} AS runner
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

COPY --from=builder --chown=node:node /app/public ./public
COPY --from=builder --chown=node:node /app/.next/standalone ./
COPY --from=builder --chown=node:node /app/.next/static ./.next/static
# Cache de renderização do Next: o compose o monta em tmpfs (sistema de arquivos somente leitura).
RUN mkdir -p .next/cache && chown node:node .next .next/cache

USER node
EXPOSE 3000
CMD ["node", "server.js"]
