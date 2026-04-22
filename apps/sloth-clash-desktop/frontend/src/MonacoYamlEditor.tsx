import Editor from '@monaco-editor/react'
import type { ComponentType } from 'react'

type MonacoYamlEditorProps = {
  value: string
  onChange: (value: string) => void
  className?: string
  height?: string
}

export function MonacoYamlEditor({
  value,
  onChange,
  className,
  height = '46vh',
}: MonacoYamlEditorProps) {
  const MonacoEditor = Editor as unknown as ComponentType<any>
  const wrapClass = `${className ? `${className} ` : ''}allowSelect monacoEditorHost`
  return (
    <div className={wrapClass}>
      <MonacoEditor
        defaultLanguage="yaml"
        language="yaml"
        value={value}
        onChange={(next: string | undefined) => onChange(String(next ?? ''))}
        options={{
          minimap: { enabled: false },
          fontSize: 13,
          lineNumbers: 'on',
          roundedSelection: false,
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 2,
          wordWrap: 'off',
          renderWhitespace: 'selection',
          mouseStyle: 'text',
        }}
        onMount={(editor: any, monaco: any) => {
          const dom = editor.getDomNode()
          if (dom) {
            const ensureFocus = () => {
              // On some WebView builds focus is lost on mouseup; re-focus editor text area.
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
        }}
        theme="vs-dark"
        height={height}
      />
    </div>
  )
}
