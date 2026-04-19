<h1 align="center">
  <img src="../apps/sloth-clash-desktop/build/appicon.png" alt="Sloth Clash" width="128" />
  <br />
  Sloth Clash
  <br />
</h1>

<p align="center">
  <b>Clash Meta (Mihomo)</b> — десктоп-клиент на <b>Wails · Go · React</b><br />
  Windows · macOS · Linux
</p>

<p align="center">
  <a href="../README.md">English</a> ·
  <a href="./README_en.md">English (docs)</a> ·
  <a href="./README_ru.md">Русский</a> ·
  <a href="./README_zh.md">简体中文</a>
  ·
  <a href="../Changelog.md">Changelog</a>
</p>

<p align="center"><sub>Полный английский текст — в корне: <a href="../README.md"><code>README.md</code></a>.</sub></p>

---

## Обзор

**Sloth Clash** — GUI под **GPL-3.0** вокруг **Mihomo** (Clash Meta). В этом репозитории — оболочка **Wails** (`apps/sloth-clash-desktop`). Слой **системного сервиса / IPC** для Windows вынесен в отдельный проект: [sloth-clash-service-ipc](https://github.com/Nemu-x/sloth-clash-service-ipc) (артефакты релизов подтягивает `pnpm run prebuild`).

## Возможности (кратко)

- Профили, прокси, правила и сценарии merge / скриптов в интерфейсе  
- Встраивание ядра Mihomo (stable и опционально alpha через prebuild)  
- Установщик сервиса Windows и раскладка sidecar под упаковку Wails  
- Схема deep link `slothclash://` (см. `wails.json`)

## Сборки

Релизы приложения: [SlothClash releases](https://github.com/Nemu-x/SlothClash/releases).  
Бинарники сервиса для сборки: [sloth-clash-service-ipc releases](https://github.com/Nemu-x/sloth-clash-service-ipc/releases).

## Сборка локально

Нужны: **Go 1.23+**, **Node 20+**, **pnpm**, Wails v2.

```bash
pnpm install
pnpm run desktop:resources
pnpm run wails:dev
```

Каталог `desktop:resources`: `apps/sloth-clash-desktop/build/` (в git не входит). На Windows в цепочку входит **`pnpm run icons:windows`** — обновляет **`build/windows/icon.ico`** из `build/appicon.png`.

## CI

GitHub Actions: `.github/workflows/desktop-artifacts.yml` — матрица по тегу `v*` или ручной запуск.

## Участие

См. [CONTRIBUTING.md](../CONTRIBUTING.md).

## Благодарности

- **База (откуда выросла концепция GUI):** [clash-verge-rev](https://github.com/clash-verge-rev/clash-verge-rev) — Clash Verge Rev (Tauri); в этом репозитории продукт перенесён на **Wails + Go**.
- **Ядро прокси (Clash Meta):** [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo).
- **Десктопная оболочка:** [Wails](https://github.com/wailsapp/wails).

Также: [zzzgydi/clash-verge](https://github.com/zzzgydi/clash-verge) (оригинальный Clash Verge) и экосистема Clash.

## Лицензия

[GPL-3.0](../LICENSE)
