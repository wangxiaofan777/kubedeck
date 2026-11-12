# syntax=docker/dockerfile:1

# 1) 前端构建
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2) 后端构建（前端产物经 go:embed 打入二进制）
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kubedeck ./cmd/server

# 3) 运行时：无发行版镜像，非 root
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kubedeck /kubedeck
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/kubedeck"]