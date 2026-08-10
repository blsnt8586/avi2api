FROM node:24-alpine AS web
WORKDIR /src/admin-web
COPY admin-web/package.json admin-web/pnpm-lock.yaml admin-web/pnpm-workspace.yaml admin-web/tsconfig.json admin-web/tsconfig.app.json admin-web/vite.config.ts admin-web/index.html ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY admin-web/src ./src
RUN pnpm run build

FROM golang:1.25-alpine AS go-build
RUN apk add --no-cache ca-certificates git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aiv2api ./cmd/leonardo2api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app app
COPY --from=go-build /out/aiv2api /usr/local/bin/aiv2api
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/aiv2api"]
