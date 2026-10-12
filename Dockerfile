FROM node:26-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN npm install --global "$(node -p 'require("./package.json").packageManager')" \
    && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /nailist ./cmd/server

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 nailist \
    && adduser -S -D -H -u 10001 -G nailist nailist \
    && mkdir -p /app/web/dist \
    && chown -R nailist:nailist /app
COPY --from=backend --chown=nailist:nailist /nailist /app/nailist
COPY --from=frontend --chown=nailist:nailist /src/web/dist/ /app/web/dist/
WORKDIR /app
USER nailist
ENV LISTEN_ADDR=:8080 WEB_DIR=/app/web/dist
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["/app/nailist"]
