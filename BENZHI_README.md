# 数据契约注册中心 (Schema Registry)

本项目实现一个「数据契约（Schema）注册中心」服务：上游按主题注册结构化数据契约的多个版本，注册中心在新增版本时对候选契约与该主题最新版本做兼容性校验（向后 BACKWARD / 向前 FORWARD / 完全 FULL / 跳过 NONE），通过后落库并分配单调递增、不复用的版本号；消费方可按主题 + 版本号获取契约、校验消息是否合规，也可在不落库前提下试探候选契约的兼容性。所有契约定义、版本、兼容性配置持久化到本地 SQLite 嵌入式数据库，进程重启后可完整恢复。

主要输入：契约定义 JSON（`{"fields":[{"name","type","required","default","enum"}]}`）、消息 JSON。
主要输出：版本号、契约定义、兼容性校验结果、消息校验结果。

## 本地命令

```bash
go build ./...            # 编译
go run . --smoke-test     # 自检（不依赖外部服务，执行后自动退出）
go run .                  # 启动 HTTP 服务（默认 :8080，db=schemareg.db）
go test ./...             # 运行测试
```

启动后默认监听 `:8080`，提供健康检查 `GET /health`、注册 `POST /subjects/{subject}/versions`、查询版本 `GET /subjects/{subject}/versions/{version}`、列举版本、列举主题、删除主题 / 版本、兼容性试探 `POST /compatibility/subjects/{subject}/versions/latest`、消息校验 `POST /subjects/{subject}/validate?version=N|latest`、兼容性配置 `GET/PUT /config/{subject}`。

## Docker 构建

构建脚本 `build_benzhi_docker.sh` 接受两个参数：镜像名与目标平台。

```bash
# amd64 镜像
bash ./build_benzhi_docker.sh go-task-benzhi:amd64 linux/amd64
docker run --rm go-task-benzhi:amd64 go version

# arm64 镜像
bash ./build_benzhi_docker.sh go-task-benzhi:arm64 linux/arm64
docker run --rm go-task-benzhi:arm64 go version

# 进入容器交互
docker run -it go-task-benzhi:amd64
```

评测镜像基于 `benzhi.Dockerfile`（官方 `golang:1.26.3`），仅用于评测；交付运行镜像基于 `Dockerfile`（builder 用 `golang:1.26.3-bookworm`，runtime 用 `alpine:3.20`），必须同时支持 `linux/amd64` 与 `linux/arm64` 双架构。

## 依赖

仅依赖标准库 + 纯 Go 的 `modernc.org/sqlite`（SQLite 嵌入式数据库，无 CGO，`CGO_ENABLED=0` 可构建）。Go 版本固定 1.26.3，`GOTOOLCHAIN=local`，依赖下载使用国内代理 `GOPROXY=https://goproxy.cn,direct`、`GOSUMDB=sum.golang.google.cn`。
