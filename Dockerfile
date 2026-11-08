# バージョンは mise.toml と同じ値にする。
FROM node:24.22.1-bookworm-slim AS web
WORKDIR /src
RUN npm install --global pnpm@12.15.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc tsconfig.base.json ./
COPY apps/web/package.json apps/web/
COPY packages/contract/package.json packages/contract/
COPY packages/tokens/package.json packages/tokens/
COPY packages/messages/package.json packages/messages/
RUN pnpm install --frozen-lockfile --filter @dopagaki/web...
COPY packages packages
COPY apps/web apps/web
RUN pnpm --filter @dopagaki/web build

FROM golang:1.27.3-bookworm AS api
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /app
COPY --from=api /server ./server
COPY --from=web /src/apps/web/dist/web/browser ./public
ENV STATIC_DIR=/app/public APP_MODE=live PORT=8080
EXPOSE 8080
CMD ["/app/server"]
