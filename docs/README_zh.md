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
- 一键切换 **Proxy** / **TUN** 流量模式，并显示实时连接健康状态  
- 可视化 + YAML 规则编辑器、实时连接监视、诊断/高级面板  
- 已签名、fail-closed 的**应用内更新**（Windows），通过 UAC 提权运行安装程序  
- Windows 服务安装包与 Wails 打包兼容的 sidecar 布局  
- Deep link：`slothclash://`（见 `wails.json`）

## 截图

<table>
  <tr>
    <td width="50%"><img src="screenshots/home.png" alt="主页 — 连接、模式、流量、状态" /><br /><sub><b>主页</b> — 连接、Rule/Global 模式、Proxy/TUN 流量、实时状态与速度</sub></td>
    <td width="50%"><img src="screenshots/profiles.png" alt="配置 — 订阅" /><br /><sub><b>配置</b> — 导入与管理订阅、用量、自动更新</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="screenshots/proxies.png" alt="代理 — selector 与 url-test 分组" /><br /><sub><b>代理</b> — selector / url-test 分组，含延迟与搜索</sub></td>
    <td width="50%"><img src="screenshots/connections.png" alt="连接 — 实时流量" /><br /><sub><b>连接</b> — 按进程的实时流量、匹配规则、上行/下行</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="screenshots/rules.png" alt="规则 — 路由表" /><br /><sub><b>规则</b> — 完整路由表，可按类型/策略筛选</sub></td>
    <td width="50%"><img src="screenshots/edit_rules.png" alt="编辑规则 — 可视化与 YAML" /><br /><sub><b>编辑规则</b> — 可视化构建 + Advanced（YAML），订阅为只读基底</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="screenshots/advanced.png" alt="高级 — 诊断与工具" /><br /><sub><b>高级</b> — 诊断、连通性探测、恢复工具</sub></td>
    <td width="50%"><img src="screenshots/settings.png" alt="设置 — 运行时、更新、诊断" /><br /><sub><b>设置</b> — 主题、语言、连接、更新与诊断</sub></td>
  </tr>
</table>

## 下载

本应用发布：[SlothClash releases](https://github.com/Nemu-x/SlothClash/releases)。  
构建时使用的服务程序：[sloth-clash-service-ipc releases](https://github.com/Nemu-x/sloth-clash-service-ipc/releases)。

## 本地构建

需要：**Go 1.25+**、**Node 20+**、**pnpm**、Wails v2。

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

## 支持本项目

Sloth Clash 免费且遵循 **GPL-3.0**。如果它对你有帮助，加密货币捐赠能让开发与发布持续下去。谢谢！🦥

| 资产 | 地址 |
| --- | --- |
| **USDT** (TRC20) | `TPACN1kJRm2FnFF1cSqYtBnJwAmZ3qGMni` |
| **USDT** (Polygon / MATIC) | `0xD9333e859Fb74D885d22E27568589de61E4433b5` |
| **BTC** | `bc1qkkcgpqym967k2x73al6f7fpvkx52q4rzkut3we` |
| **ETH** | `0xD9333e859Fb74D885d22E27568589de61E4433b5` |

> 转账前请再次确认网络 — 选错网络的转账无法找回。

## 致谢

- **基础（上游 GUI 渊源）：** [clash-verge-rev](https://github.com/clash-verge-rev/clash-verge-rev)（Clash Verge Rev，Tauri）；本仓库用 **Wails + Go** 重新实现产品方向。
- **代理内核（Clash Meta）：** [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)。
- **桌面壳：** [Wails](https://github.com/wailsapp/wails)。

另：[zzzgydi/clash-verge](https://github.com/zzzgydi/clash-verge)（原版 Clash Verge）及 Clash 生态。

## 许可证

[GPL-3.0](../LICENSE)
