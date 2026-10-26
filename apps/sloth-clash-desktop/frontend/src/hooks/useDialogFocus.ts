import { type RefObject, useEffect, useRef } from 'react'

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"]):not([type="file"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

function focusables(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(
    (el) => !el.closest('[hidden]') && el.style.display !== 'none',
  )
}

/**
 * Keyboard behavior of a modal dialog, while `open`:
 *
 * - focus moves into the dialog: the element marked `data-autofocus`, else
 *   the dialog element itself (give it `tabIndex={-1}`);
 * - Tab / Shift+Tab cycle inside the dialog;
 * - Escape calls `onEscape` (pass undefined while the dialog must stay, e.g.
 *   during a save);
 * - on close, focus returns to the element that had it before opening.
 */
export function useDialogFocus(
  ref: RefObject<HTMLElement | null>,
  open: boolean,
  onEscape?: () => void,
): void {
  // Latest handler without re-running the open effect (that would steal
  // focus back to the autofocus target on every render).
  const escapeRef = useRef(onEscape)
  useEffect(() => {
    escapeRef.current = onEscape
  })

  useEffect(() => {
    if (!open) return
    const node = ref.current
    if (!node) return
    const previous =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
    const initial = node.querySelector<HTMLElement>('[data-autofocus]') ?? node
    initial.focus()

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        const handler = escapeRef.current
        if (!handler) return
        e.preventDefault()
        e.stopPropagation()
        handler()
        return
      }
      if (e.key !== 'Tab') return
      const items = focusables(node)
      if (items.length === 0) {
        e.preventDefault()
        node.focus()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      const inside = active instanceof Node && node.contains(active)
      if (e.shiftKey && (!inside || active === first || active === node)) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && (!inside || active === last)) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      if (previous?.isConnected) previous.focus()
    }
  }, [open, ref])
}
