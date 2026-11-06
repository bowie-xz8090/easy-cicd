# ============================================================
# 阶段 1：构建前端
# ============================================================
FROM node:18-alpine AS frontend-builder

WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm config set registry https://registry.npmmirror.com && npm ci
COPY web/ ./
RUN npm run build

# ============================================================
# 阶段 2：构建后端
# ============================================================
FROM golang:1.24-alpine AS backend-builder

# CGO 需要 gcc（SQLite 驱动依赖）
RUN apk add --no-cache gcc musl-dev

# 设置 Go 模块代理（解决国内网络问题）
ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY tools.go ./
RUN CGO_ENABLED=1 go build -ldflags '-extldflags "-static"' -o auto-deploy-platform ./cmd/

# ============================================================
# 阶段 3：最终运行镜像
# ============================================================
# Alpine 3.22 的 nodejs 是 22.23，满足 Vite 8 / rolldown 的 Node >=22.12。
# 3.20 自带的 Node 20.15 会被 pnpm 跳过 musl 原生绑定，前端构建会失败。
FROM alpine:3.22

# 安装运行时依赖
RUN apk add --no-cache \
    git openssh-client ca-certificates tzdata \
    # Java 构建环境（Maven + JDK）
    openjdk21-jdk maven \
    # Node.js 前端构建环境
    nodejs npm \
    # 其他工具
    bash curl tar

# 安装 pnpm
RUN npm config set registry https://registry.npmmirror.com && \
    npm install -g pnpm@10.34.5 && \
    pnpm config set store-dir /root/.local/share/pnpm/store

# 配置 Maven 镜像加速
RUN mkdir -p /root/.m2 && \
    cat > /root/.m2/settings.xml << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<settings>
  <mirrors>
    <mirror>
      <id>aliyun</id>
      <mirrorOf>central</mirrorOf>
      <url>https://maven.aliyun.com/repository/public</url>
    </mirror>
  </mirrors>
</settings>
EOF

# 设置时区
ENV TZ=Asia/Shanghai

WORKDIR /app

# 从构建阶段复制产物
COPY --from=backend-builder /build/auto-deploy-platform ./
COPY --from=frontend-builder /build/web/dist ./web/dist/
COPY config.example.yaml ./config.example.yaml

# 创建数据和工作目录
RUN mkdir -p /app/data /app/workspace

# 创建启动脚本：如果 config.yaml 不存在则从 example 复制
RUN printf '#!/bin/sh\nif [ ! -f /app/config.yaml ]; then\n  cp /app/config.example.yaml /app/config.yaml\n  echo "Created config.yaml from config.example.yaml"\nfi\nexec ./auto-deploy-platform\n' > /app/entrypoint.sh && chmod +x /app/entrypoint.sh

# 默认环境变量
ENV GIN_MODE=release
ENV CONFIG_PATH=/app/config.yaml
ENV DB_PATH=/app/data/deploy.db

# 暴露端口
EXPOSE 8080

# 数据卷：配置文件和数据库持久化
VOLUME ["/app/data"]

ENTRYPOINT ["/app/entrypoint.sh"]
