## Sloth Clash desktop `0.2.1` — 2026-04-22

### English

- Fixed process-close handling in the Windows installer (target executable + neutral "not found" log wording).
- Added Monaco editor for advanced YAML editing (merge/config/rules/proxy groups), with improved clipboard/focus behavior.
- Improved subscription import/connect responsiveness by reducing blocking probe steps and avoiding extra warmup contention.
- Added stronger config pipeline validation and normalization (`rules -> policy` checks, escaped Unicode normalization for readable emojis).
- Persisted traffic mode (`proxy` / `tun`) across restarts.

### Русский

- Исправлена обработка закрытия процессов в Windows-инсталляторе (целевой exe + нейтральные сообщения "process not found").
- Добавлен Monaco для advanced YAML-редактирования (merge/config/rules/proxy groups), улучшено поведение буфера обмена и фокуса.
- Ускорены импорт подписки и первичное подключение за счет уменьшения блокирующих probe-шагов и лишних фоновых прогревов.
- Усилена валидация и нормализация config-pipeline (`rules -> policy`, нормализация escaped Unicode для читаемых emoji).
- Добавлено сохранение traffic mode (`proxy` / `tun`) между перезапусками.

### 中文

- 修复 Windows 安装器的进程关闭逻辑（目标可执行文件 + 中性“未找到进程”日志文案）。
- 为高级 YAML 编辑（merge/config/rules/proxy groups）接入 Monaco，并改进剪贴板与焦点行为。
- 通过减少阻塞式探测与不必要预热竞争，提升订阅导入与首次连接响应速度。
- 加强配置管线校验与规范化（`rules -> policy` 校验、escaped Unicode 规范化以显示可读 emoji）。
- 持久化 `traffic mode`（`proxy` / `tun`），重启后保持上次状态。

---

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
