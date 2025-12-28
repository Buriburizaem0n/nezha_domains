# 哪吒面板 (Nezha Dashboard) - 个人编译指南
这份文档记录了在 Arch Linux 环境下，使用 VS Code Dev Containers 编译自定义主题版本的哪吒面板的完整流程。

## ⚠️ 核心前置条件 (不做会卡死)
网络环境：必须开启 TUN 模式 (透明代理)。

原因：Dev Container 内部的 Go/Apt/Wget 默认不走普通代理，只有宿主机开启 TUN 模式才能让容器内网络畅通无阻。

工具：v2rayA / NekoRay / Hiddify。

环境清理：确保 .devcontainer/devcontainer.json 是纯净版（去除了 docker-in-docker，去除了 sed 换源，去除了 ghproxy）。

## 🛠️ 编译步骤
### 1. 进入开发环境
在 VS Code 中打开本项目，按 F1 -> 选择 Dev Containers: Reopen in Container。 等待容器启动完成。

### 2. 准备 GeoIP 数据库
编译前必须确保 IP 数据库存在，且文件名必须正确。

```Bash

# 检查是否存在，没有则下载
# 注意：必须重命名为 geoip.db，Go 编译器不认 .mmdb 后缀
if [ ! -f "pkg/geoip/geoip.db" ]; then
    wget -O pkg/geoip/geoip.db https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-Country.mmdb
fi
```

### 3. 替换自定义前端 (关键)
我有自定义的 V1 主题，需要手动编译并替换进去，否则编译出来是官方默认皮。

前端项目路径：../nezha-dash-v1 (假设在上一级目录，根据实际调整)

后端资源路径：cmd/dashboard/user-dist

操作命令：

```Bash

# 1. 清空旧的资源目录
rm -rf ./nezha_domains/cmd/dashboard/user-dist/*
rm -rf ./nezha_domains/cmd/dashboard/admin-dist/*

# 2. 复制自定义的前台主题 (注意 -r 和结尾的 *)
# 将 nezha-dash-v1/dist 下的所有内容 -> 复制到 user-dist
cp -r ./nezha-dash-v1/dist/* ./nezha_domains/cmd/dashboard/user-dist/

# 3. 复制自定义的后台管理 (注意 -r 和结尾的 *)
# 将 admin-frontend-domain/dist 下的所有内容 -> 复制到 admin-dist
cp -r ./admin-frontend-domain/dist/* ./nezha_domains/cmd/dashboard/admin-dist/
```
### 4. 生成后端代码
确保 API 文档和 gRPC 代码是最新的。

```Bash

# 生成 Swagger 文档
swag init --pd -d . -g ./cmd/dashboard/main.go -o ./cmd/dashboard/docs --requiredByDefault

# 生成 gRPC 代码
protoc --go-grpc_out="require_unimplemented_servers=false:." --go_out="." proto/*.proto
```
### 5. 最终编译
生成可执行文件 dashboard。

```Bash

# -s -w: 去除调试符号，减小体积
go build -ldflags="-s -w" -o dashboard cmd/dashboard/main.go
```
## 🚀 快速验证
编译完成后，运行以下命令测试：

```Bash

# 运行面板
./dashboard

# 如果能看到 Logo 输出，或者提示 config.yaml 不存在，说明编译成功。
# 如果提示 user-dist 404 之类的，说明前端文件没复制对。
```