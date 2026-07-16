# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=docker.m.daocloud.io/library/node:24-alpine
ARG GO_IMAGE=docker.m.daocloud.io/library/golang:1.26-alpine
ARG RUNTIME_IMAGE=docker.m.daocloud.io/library/alpine:3.23

FROM ${NODE_IMAGE} AS frontend-build
ARG PNPM_REGISTRY=https://registry.npmmirror.com
WORKDIR /src/frontend
RUN corepack enable && pnpm config set registry ${PNPM_REGISTRY}
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM ${GO_IMAGE} AS backend-build
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend/ ./backend/
RUN rm -rf backend/internal/webui/dist
COPY --from=frontend-build /src/frontend/dist ./backend/internal/webui/dist
RUN cd backend && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ai-bot ./cmd/server

FROM ${RUNTIME_IMAGE} AS runtime
ARG ALPINE_MIRROR=https://mirrors.aliyun.com/alpine
RUN sed -i "s#https\?://dl-cdn.alpinelinux.org/alpine#${ALPINE_MIRROR}#g" /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata \
    && addgroup -S aibot \
    && adduser -S -G aibot -h /app aibot \
    && mkdir -p /app/data \
    && chown -R aibot:aibot /app
WORKDIR /app
COPY --from=backend-build --chown=aibot:aibot /out/ai-bot /app/ai-bot
ENV APP_ADDR=:8080 DATA_DIR=/app/data TZ=Asia/Shanghai
USER aibot
EXPOSE 8080
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/ai-bot"]
