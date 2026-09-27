# syntax=docker/dockerfile:1

# ------------------------------------------------------------
# Stage 1: build Vue web frontend
# ------------------------------------------------------------
FROM node:24-alpine AS web-build

WORKDIR /src

# package.json / lockfile 单独复制，让 npm 依赖层能够缓存。
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci

# gen:api 需要 ../api/openapi.yaml。
COPY api ./api
COPY web ./web

RUN cd web \
    && npm run gen:api \
    && npm run build


# ------------------------------------------------------------
# Stage 2: build Go binaries
# ------------------------------------------------------------
FROM golang:1.27-alpine AS go-build

RUN apk add --no-cache ca-certificates git

WORKDIR /src

# 与 npm 同理：先下载 Go modules，业务源码变化时可以复用依赖层。
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

COPY server ./server

# 用刚才真正构建出的 Web 覆盖仓库里的 .gitkeep 占位目录。
COPY --from=web-build /src/web/dist ./server/webdist/dist

RUN cd server \
    && CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/astral-server \
        ./cmd/astral-server \
    && CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/astral-bootstrap \
        ./cmd/astral-bootstrap


# ------------------------------------------------------------
# Stage 3: minimal runtime
# ------------------------------------------------------------
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S astral \
    && adduser -S -D -H -G astral astral \
    && mkdir -p /app \
    && chown astral:astral /app

ARG VERSION=dev
ARG VCS_REF=unknown

LABEL org.opencontainers.image.title="Astral Modulator" \
      org.opencontainers.image.source="https://github.com/The-Astral-Express-Family/astral-modulator" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}"

WORKDIR /app

COPY --from=go-build --chown=astral:astral \
    /out/astral-server \
    /out/astral-bootstrap \
    /usr/local/bin/

USER astral

EXPOSE 8080

STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/astral-server"]
