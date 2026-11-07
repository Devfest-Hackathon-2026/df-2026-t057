FROM node:24.22.1-bookworm-slim AS web
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY angular.json tsconfig.json ./
COPY web web
COPY shared shared
RUN npx ng build

FROM golang:1.27.3-bookworm AS api
WORKDIR /src
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /app
COPY --from=api /server ./server
COPY --from=web /src/dist/web/browser ./public
ENV STATIC_DIR=/app/public APP_MODE=live PORT=8080
EXPOSE 8080
CMD ["/app/server"]
