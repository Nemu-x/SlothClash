<h1 align="center">
  <img src="../apps/sloth-clash-desktop/build/appicon.png" alt="Sloth Clash" width="128" />
  <br />
  Sloth Clash
  <br />
</h1>

<p align="center">
  <b>Clash Meta（Mihomo）</b> 桌面客户端 — <b>Wails · Go · React</b><br />
  Windows · macOS · Linux
</p>

<p align="center">
  <a href="../README.md">English</a> ·
  <a href="./README_en.md">English (docs)</a> ·
  <a href="./README_ru.md">Русский</a> ·
  <a href="./README_zh.md">简体中文</a>
  ·
  <a href="../Changelog.md">更新日志</a>
</p>

<p align="center"><sub>完整英文说明见仓库根目录 <a href="../README.md"><code>README.md</code></a>。</sub></p>

---

## 简介

**Sloth Clash** 是基于 **GPL-3.0** 的 **Mihomo（Clash Meta）** 图形客户端。本仓库提供 **Wails** 桌面应用（`apps/sloth-clash-desktop`）。Windows **系统服务 / IPC** 在独立仓库维护：[sloth-clash-service-ipc](https://github.com/Nemu-x/sloth-clash-service-ipc)（构建时由 `pnpm run prebuild` 拉取发行文件）。

## 功能概览

- 配置项、代理、规则及 merge / 脚本类工作流  
- 集成 Mihomo 内核（stable，可选 alpha 通道由 prebuild 提供）  
- Windows 服务安装包与 Wails 打包兼容的 sidecar 布局  
- Deep link：`slothclash://`（见 `wails.json`）

## 下载

本应用发布：[SlothClash releases](https://github.com/Nemu-x/SlothClash/releases)。  
构建时使用的服务程序：[sloth-clash-service-ipc releases](https://github.com/Nemu-x/sloth-clash-service-ipc/releases)。

## 本地构建

需要：**Go 1.23+**、**Node 20+**、**pnpm**、Wails v2。

```bash
pnpm install
pnpm run desktop:resources
pnpm run wails:dev
```

`desktop:resources` 写入 `apps/sloth-clash-desktop/build/`（不提交到 git）。在 Windows 上包含 **`pnpm run icons:windows`**，从 `build/appicon.png` 更新 **`build/windows/icon.ico`**。

## CI

GitHub Actions：`.github/workflows/desktop-artifacts.yml` — 标签 `v*` 或手动触发。

## 贡献

见 [CONTRIBUTING.md](../CONTRIBUTING.md)。

## 致谢

- **基础（上游 GUI 渊源）：** [clash-verge-rev](https://github.com/clash-verge-rev/clash-verge-rev)（Clash Verge Rev，Tauri）；本仓库用 **Wails + Go** 重新实现产品方向。
- **代理内核（Clash Meta）：** [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)。
- **桌面壳：** [Wails](https://github.com/wailsapp/wails)。

另：[zzzgydi/clash-verge](https://github.com/zzzgydi/clash-verge)（原版 Clash Verge）及 Clash 生态。

## 许可证

[GPL-3.0](../LICENSE)
