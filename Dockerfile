FROM node:26-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN npm install --global "$(node -p 'require("./package.json").packageManager')" \
    && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

FROM golang:1.25-alpine AS backend
ARG BUILD_VERSION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
COPY web/assets.go ./web/assets.go
COPY --from=frontend /src/web/dist/ ./web/dist/
RUN test -n "$BUILD_VERSION" \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.buildVersion=$BUILD_VERSION" -o /nailist ./cmd/server

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 nailist \
    && adduser -S -D -H -u 10001 -G nailist nailist \
    && mkdir -p /app \
    && chown -R nailist:nailist /app
COPY --from=backend --chown=nailist:nailist /nailist /app/nailist
WORKDIR /app
USER nailist
ENV PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:${PORT}/api/health || exit 1
ENTRYPOINT ["/app/nailist"]
