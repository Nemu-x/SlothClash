# CONTRIBUTING — i18n (Sloth Clash desktop)

The Wails desktop UI uses **react-i18next** with **one JSON file per language** — no separate Tauri/Rust locale bundles.

## Where strings live

```
apps/sloth-clash-desktop/frontend/src/locales/
  en.json    ← canonical keys and English copy
  ru.json
  zh.json
```

- **`en.json`** is the source of truth: add or change keys here first, then mirror the same key structure in `ru.json` and `zh.json`.
- Nested keys use the usual JSON object shape (e.g. `settings.title`, `tour.connectTitle`).
- Runtime wiring: `apps/sloth-clash-desktop/frontend/src/i18n.ts` imports these files and registers them with i18next.

## Language selection

The app persists the choice in `localStorage` under **`sloth-lang`** (`en` | `ru` | `zh`). The Settings screen and `readStoredLang()` / `changeLanguage()` must stay consistent when adding a new locale code.

## Adding or updating translations

1. Edit **`en.json`** with the new or changed keys.
2. Copy the same keys into **`ru.json`** and **`zh.json`** with translated values (do not leave missing keys in non-English files if the key exists in `en.json`).
3. Run **`pnpm run lint`** and **`pnpm run typecheck`** from the repo root (ensures TS/ESLint still pass).
4. Run **`pnpm run wails:dev`**, switch languages in Settings, and click through the affected screens.

## Adding a new language (e.g. `de`)

1. Add `apps/sloth-clash-desktop/frontend/src/locales/de.json` (duplicate structure from `en.json`).
2. Extend the `resources` object and `readStoredLang()` / language picker types in **`i18n.ts`** and the Settings UI in **`App.tsx`** so `de` is selectable and persisted.
3. Document the new code in this file.

There is no separate `pnpm i18n:format` script in this repo — keep JSON sorted consistently by hand or with your editor.

## Guidelines

- Prefer **stable, semantic key names** (`settings.checkUpdates`) over opaque IDs.
- Keep strings **short** where possible; long Russian/Chinese lines can break narrow layouts.
- Use **i18next interpolation** only when the app already passes variables (e.g. `{{version}}`).

## Screenshots

For PRs that change visible copy or layout, attach before/after screenshots for at least one non-English locale if the change is language-sensitive.
