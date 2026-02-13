# Synology Drive Watcher (Go 重构版)

这是一个轻量、高效的 macOS 服务，旨在自动管理 Synology Drive 的忽略目录（例如 `node_modules`, `venv`, `target` 等）。

## 为什么要开发这个工具？

macOS 版本的 Synology Drive 客户端目前无法方便地设置“全局忽略模式”（例如在所有同步任务中忽略名为 `node_modules` 的文件夹）。这导致严重的问题：

-   **海量文件阻塞**：前端项目的 `node_modules` 通常包含成千上万个小文件。
-   **资源消耗极高**：Synology Drive 需要为每个文件建立索引和上传，导致 CPU/内存飙升，风扇狂转。
-   **同步异常**：频繁变动的临时文件、缓存文件（如 `Build` 目录或 IDE 缓存）会导致同步队列堵塞，甚至引发客户端报错或崩溃。
-   **网络拥堵**：大量无用数据上传占用上行带宽。

本工具完美解决了这个问题：
1.  **实时监控**：监听 Synology Drive 的配置文件（`blacklist.filter` 和 `filter-v4150`）。
2.  **自动修复**：一旦发现忽略规则丢失或被覆盖，会自动还原规则。
3.  **智能重启**：在修复配置后，自动重启 Synology Drive 客户端以立即生效。

## 核心特性

-   **零配置开箱即用**：默认已包含主流开发栈的忽略规则：
    -   Node.js: `node_modules`, `.next`, `.nuxt`
    -   Python: `venv`, `.venv`, `__pycache__`
    -   Go/Rust: `vendor`, `target`
    -   移动开发: `Pods`, `.gradle`, `DerivedData`
    -   其他: `.DS_Store`, `.git` 等
-   **极低资源占用**：使用 macOS 原生的文件系统事件 (`fsnotify`)，CPU 和内存占用几乎为零。
-   **安全关闭机制**：在写入配置前确保 Synology Drive 完全退出，防止数据损坏或配置被覆盖。
-   **原生通知**：当规则被修复或应用重启时，发送 macOS 系统通知（中文）。

## 安装指南

### 方法 1：一键安装（推荐）

只需在终端运行以下命令，即可自动下载最新版本并安装为后台服务：

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/MyLeoLan/synology-drive-ignore/main/install.sh)"
```

该脚本会自动检测你的芯片架构（Intel/Apple Silicon），下载对应二进制文件，并设置为开机自启。

### 卸载服务

如果你想卸载服务并删除程序，只需运行：

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/MyLeoLan/synology-drive-ignore/main/install.sh)" -- --uninstall
```

### 方法 2：手动从 Release 安装

1.  前往 [Releases 页面](https://github.com/MyLeoLan/synology-drive-ignore/releases) 下载适合你架构的二进制文件。
2.  将其移动到 `/usr/local/bin` 或其他 PATH 路径下。
3.  手动运行或配置 Launch Agent。

## 自定义忽略规则

如果你想添加自定义的文件夹到忽略列表，需要自行编译：

1.  克隆本仓库。
2.  修改 `main.go` 中的 `defaultIgnoreDirs` 变量。
3.  运行 `go build -o drive-conf-watch`。
4.  停止现有服务并替换二进制文件。

```bash
launchctl unload ~/Library/LaunchAgents/com.user.synologywatch.plist
# 替换二进制文件...
launchctl load ~/Library/LaunchAgents/com.user.synologywatch.plist
```

## 构建指南

本项目使用 Go 语言编写。请确保你已安装 Go 环境 (推荐 1.20+)。

### 为当前机器构建

```bash
go build -o drive-conf-watch
```

### 交叉编译 (用于发布)

**构建 Apple Silicon (M系列芯片) 版本:**
```bash
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o drive-conf-watch-darwin-arm64
```

**构建 Intel Mac 版本:**
```bash
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o drive-conf-watch-darwin-amd64
```

## CI/CD (GitHub Actions)

本仓库已配置 GitHub Actions 工作流 (`.github/workflows/build.yml`)。
每当你推送新的 Tag (例如 `v1.0.0`) 或推送到 `main` 分支时，它会自动构建适用于 macOS (Intel & Apple Silicon) 的二进制文件并发布 Release。

## 致谢

本项目基于原 Python 版本重构，改用 Go 语言以获得更好的性能和稳定性。
