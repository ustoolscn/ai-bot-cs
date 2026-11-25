ARG NODE_IMAGE=docker.m.daocloud.io/library/node:24-alpine
ARG GO_IMAGE=docker.m.daocloud.io/library/golang:1.26-alpine
ARG RUNTIME_IMAGE=docker.m.daocloud.io/library/alpine:3.23

FROM ${NODE_IMAGE} AS frontend-build
ARG PNPM_REGISTRY=https://registry.npmmirror.com
WORKDIR /src/frontend
ARG PNPM_VERSION=10.11.0
RUN npm install --global pnpm@${PNPM_VERSION} --registry=${PNPM_REGISTRY} && pnpm config set registry ${PNPM_REGISTRY}
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
COPY backend/internal/statuscard/templates/ /src/backend/internal/statuscard/templates/
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
    && apk add --no-cache ca-certificates tzdata su-exec chromium font-noto-cjk \
    && addgroup -S aibot \
    && adduser -S -G aibot -h /app aibot \
    && mkdir -p /app/data \
    && chown -R aibot:aibot /app
WORKDIR /app
COPY --from=backend-build --chown=aibot:aibot /out/ai-bot /app/ai-bot
COPY deploy/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod 755 /app/docker-entrypoint.sh
ARG VCS_REF=unknown
LABEL org.opencontainers.image.revision=$VCS_REF
ENV APP_ADDR=:8080 DATA_DIR=/app/data TZ=Asia/Shanghai GIN_MODE=release
EXPOSE 8080
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/docker-entrypoint.sh"]
