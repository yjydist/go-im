# ============================================
# 第一阶段：统一编译
# ============================================
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# 先复制依赖文件，利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 编译三个服务
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/im-api   ./cmd/api/
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/im-ws    ./cmd/ws/
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/im-push  ./cmd/push/

# ============================================
# 第二阶段：API 服务
# ============================================
FROM alpine:3.19 AS api

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /bin/im-api .
COPY config/go-im.yaml config/go-im.yaml

EXPOSE 8080
CMD ["./im-api", "-config", "config/go-im.yaml"]

# ============================================
# 第三阶段：WS 网关
# ============================================
FROM alpine:3.19 AS ws

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /bin/im-ws .
COPY config/go-im.yaml config/go-im.yaml

EXPOSE 8081 9091
CMD ["./im-ws", "-config", "config/go-im.yaml"]

# ============================================
# 第四阶段：Push 服务
# ============================================
FROM alpine:3.19 AS push

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /bin/im-push .
COPY config/go-im.yaml config/go-im.yaml

CMD ["./im-push", "-config", "config/go-im.yaml"]
