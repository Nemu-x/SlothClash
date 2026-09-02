import Editor, { DiffEditor } from '@monaco-editor/react'
import type { ComponentType } from 'react'

export type MonacoLanguage = 'yaml' | 'javascript'

type MonacoCodeEditorProps = {
  value: string
  onChange: (value: string) => void
  className?: string
  height?: string
  language?: MonacoLanguage
}

// Shared editor options: identical for every language so a YAML pane and a
// script pane feel like the same editor.
const baseOptions = {
  minimap: { enabled: false },
  fontSize: 13,
  lineNumbers: 'on' as const,
  roundedSelection: false,
  scrollBeyondLastLine: false,
  automaticLayout: true,
  tabSize: 2,
  wordWrap: 'off' as const,
  renderWhitespace: 'selection' as const,
  mouseStyle: 'text' as const,
  // Render find / suggest / hover overlays in a fixed layer attached to
  // <body> instead of inside the editor DOM. In a modal this avoids
  // clipping AND keeps the find-widget (Ctrl+F) input out of the
  // editor's mouseup handler below, so the user can actually type in it.
  fixedOverflowWidgets: true,
}

// The WebView workarounds below are load-bearing on Wails builds and are shared
// by every editor instance — see the comments inside.
function attachWebViewWorkarounds(editor: any, monaco: any) {
  const dom = editor.getDomNode()
  if (dom) {
    const ensureFocus = (e: MouseEvent) => {
      // On some WebView builds focus is lost on mouseup; re-focus the
      // editor text area. BUT do NOT steal focus from Monaco's own
      // widgets (find/replace box, suggest, hover) — clicking into the
      // Ctrl+F search field must keep its focus so the user can type.
      const target = e.target as HTMLElement | null
      if (
        target &&
        target.closest(
          '.find-widget, .suggest-widget, .monaco-inputbox, .monaco-hover',
        )
      ) {
        return
      }
      requestAnimationFrame(() => editor.focus())
    }
    dom.addEventListener('mouseup', ensureFocus, true)
    editor.onDidDispose(() => {
      dom.removeEventListener('mouseup', ensureFocus, true)
    })
  }
  // Wails/WebView can swallow native clipboard shortcuts; keep Ctrl/Cmd C/V/X reliable.
  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyC, () => {
    const model = editor.getModel()
    const sel = editor.getSelection()
    if (!model || !sel) return
    const text = model.getValueInRange(sel)
    if (!text) return
    void navigator.clipboard.writeText(text)
  })
  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyX, () => {
    const model = editor.getModel()
    const sel = editor.getSelection()
    if (!model || !sel) return
    const text = model.getValueInRange(sel)
    if (!text) return
    void navigator.clipboard.writeText(text)
    editor.executeEdits('clipboard-cut', [
      { range: sel, text: '', forceMoveMarkers: true },
    ])
  })
  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyV, () => {
    const sel = editor.getSelection()
    if (!sel) return
    void navigator.clipboard.readText().then((text) => {
      if (!text) return
      editor.executeEdits('clipboard-paste', [
        { range: sel, text, forceMoveMarkers: true },
      ])
    })
  })
}

/** Code editor for one of the languages we edit in-app (YAML or JavaScript). */
export function MonacoCodeEditor({
  value,
  onChange,
  className,
  height = '46vh',
  language = 'yaml',
}: MonacoCodeEditorProps) {
  const MonacoEditor = Editor as unknown as ComponentType<any>
  const wrapClass = `${className ? `${className} ` : ''}allowSelect monacoEditorHost`
  return (
    <div className={wrapClass}>
      <MonacoEditor
        defaultLanguage={language}
        language={language}
        value={value}
        onChange={(next: string | undefined) => onChange(String(next ?? ''))}
        options={baseOptions}
        onMount={attachWebViewWorkarounds}
        theme="vs-dark"
        height={height}
      />
    </div>
  )
}

/** YAML editor — the original component, now a thin call with language="yaml". */
export function MonacoYamlEditor(
  props: Omit<MonacoCodeEditorProps, 'language'>,
) {
  return <MonacoCodeEditor {...props} language="yaml" />
}

/** JavaScript editor for the per-profile script override. */
export function MonacoScriptEditor(
  props: Omit<MonacoCodeEditorProps, 'language'>,
) {
  return <MonacoCodeEditor {...props} language="javascript" />
}

/**
 * Read-only side-by-side diff, used by the script preview to show the config
 * generated with the candidate script against the one generated without it.
 */
export function MonacoDiffViewer({
  original,
  modified,
  className,
  height = '46vh',
  language = 'yaml',
}: {
  original: string
  modified: string
  className?: string
  height?: string
  language?: MonacoLanguage
}) {
  const Diff = DiffEditor as unknown as ComponentType<any>
  const wrapClass = `${className ? `${className} ` : ''}allowSelect monacoEditorHost`
  return (
    <div className={wrapClass}>
      <Diff
        original={original}
        modified={modified}
        language={language}
        theme="vs-dark"
        height={height}
        options={{
          ...baseOptions,
          readOnly: true,
          renderSideBySide: true,
          // Whitespace-only churn would drown the actual change.
          ignoreTrimWhitespace: true,
        }}
        onMount={(editor: any, monaco: any) => {
          // Both panes get the clipboard/focus fixes; only the modified side is
          // interactive enough to need them, but copying from either must work.
          attachWebViewWorkarounds(editor.getModifiedEditor(), monaco)
          attachWebViewWorkarounds(editor.getOriginalEditor(), monaco)
        }}
      />
    </div>
  )
}
