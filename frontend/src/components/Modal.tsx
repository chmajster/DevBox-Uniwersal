import { useEffect, useRef, type ReactNode } from 'react'

/** Native modal supplies focus containment, Escape handling and inert background. */
export function Modal({ open, onClose, labelId, className = '', children }: {
  open: boolean
  onClose(): void
  labelId: string
  className?: string
  children: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dialog = ref.current
    if (!open || !dialog) return
    const previousOverflow = document.body.style.overflow
    dialog.showModal()
    // React autoFocus runs on mount, including when a dialog is initially closed.
    // Prefer the first text field on every opening; otherwise keep native focus.
    dialog.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled])')?.focus()
    document.body.style.overflow = 'hidden'
    return () => {
      dialog.close()
      document.body.style.overflow = previousOverflow
    }
  }, [open])
  return <dialog ref={ref} className={`workspace-modal ${className}`} aria-labelledby={labelId}
    onCancel={(event) => { event.preventDefault(); onClose() }}
    onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <div className="modal-surface">{children}</div>
  </dialog>
}
