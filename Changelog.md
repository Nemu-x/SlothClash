## Sloth Clash desktop `0.1.0` — 2026-04-19

### English

- First snapshot of the **Wails + Go + React** desktop app under `apps/sloth-clash-desktop`.
- **Prebuild** targets only the Wails tree (no `src-tauri`); service EXEs are pulled from [sloth-clash-service-ipc releases](https://github.com/Nemu-x/sloth-clash-service-ipc/releases).
- ESLint: separate profile for Vite config; relaxed `import-x` for `App.tsx` → `build/appicon.png`.
- First-run spotlight onboarding, GitHub release update checks (Windows in-place installer), optional NSIS desktop shortcut.

### Русский

- Первый снимок десктопа **Wails + Go + React** в `apps/sloth-clash-desktop`.
- **Prebuild** пишет только в дерево Wails (без `src-tauri`); сервисные `.exe` качаются с [релизов sloth-clash-service-ipc](https://github.com/Nemu-x/sloth-clash-service-ipc/releases).
- ESLint: отдельный профиль для `vite.config.ts`; ослаблен резолв импорта иконки в `App.tsx`.
- Онбординг (spotlight), проверка обновлений с GitHub, опциональный ярлык на рабочем столе в установщике.

### 中文

- 首个 **Wails + Go + React** 桌面应用快照（`apps/sloth-clash-desktop`）。
- **Prebuild** 仅面向 Wails 目录（无 `src-tauri`）；Windows 服务程序从 [sloth-clash-service-ipc 发行版](https://github.com/Nemu-x/sloth-clash-service-ipc/releases) 拉取。
- ESLint：为 Vite 配置单独规则；`App.tsx` 对 `build/appicon.png` 的导入不再被 `import-x` 拦截。
- 首次使用引导、GitHub 更新检测、安装程序可选桌面快捷方式。

---

## v2.4.8

> [!IMPORTANT]
> 关于版本的说明：Clash Verge 版本号遵循 x.y.z：x 为重大架构变更，y 为功能新增，z 为 Bug 修复。

- **Mihomo(Meta) 内核升级至 v1.19.23**

### 🐞 修复问题

- 修复系统代理关闭后在 PAC 模式下未完全关闭
- 修复 macOS 开关代理时可能的卡死
- 修复修改定时自动更新后记时未及时刷新
- 修复 Linux 关闭 TUN 不立即生效

### ✨ 新增功能

- 新增 macOS 托盘速率显示
- 快捷键操作通知操作结果

### 🚀 优化改进

- 优化 macOS 读取系统代理性能
