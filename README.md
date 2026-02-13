# Synology Drive Watcher (Go 重构版)

这是一个轻量、高效的 macOS 服务，旨在自动管理 Synology Drive 的忽略目录（例如 `node_modules`, `venv`, `target` 等）。

## 为什么要开发这个工具？

macOS 版本的 Synology Drive 客户端目前无法方便地设置“全局忽略模式”（例如在所有同步任务中忽略名为 `node_modules` 的文件夹）。这导致数以万计的依赖文件被上传到 NAS，严重拖慢同步速度并占用大量网络和系统资源。

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

1.  克隆本仓库或直接下载 Release 中的二进制文件。
2.  运行安装脚本：

```bash
chmod +x install.sh
./install.sh
```

该脚本会自动执行以下操作：
-   检查二进制文件是否存在（如果不存在会提示你先构建）。
-   将应用注册为 **macOS Launch Agent**（开机自启服务）。
-   立即启动服务并在后台静默运行。

### 方法 2：手动运行

你也可以直接在终端运行二进制文件来测试效果：

```bash
./drive-conf-watch
```

## 自定义忽略规则

如果你想添加自定义的文件夹到忽略列表：

1.  打开 `main.go` 文件。
2.  修改文件顶部的 `defaultIgnoreDirs` 变量：

```go
var defaultIgnoreDirs = []string{
    // ... 现有规则 ...
    "my-custom-folder",   // 添加你的文件夹
    "another-ignored-dir",
}
```

3.  重新编译程序（参考下方的“构建指南”）。
4.  重启服务以应用更改：

```bash
launchctl unload ~/Library/LaunchAgents/com.user.synologywatch.plist
launchctl load ~/Library/LaunchAgents/com.user.synologywatch.plist
```

## 构建指南

本项目使用 Go 语言编写。请确保你已安装 Go 环境 (推荐 1.20+)。

### 为当前机器构建

```bash
go build -o drive-conf-watch
```

### 交叉编译 (用于发布)

**构建 Apple Silicon (M1/M2/M3) 版本:**
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
