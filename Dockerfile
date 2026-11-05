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

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=api /server ./server
COPY --from=web /src/dist/web/browser ./public
ENV STATIC_DIR=/app/public APP_MODE=live PORT=8080
EXPOSE 8080
CMD ["/app/server"]
