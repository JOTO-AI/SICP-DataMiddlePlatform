export type CloseReason = 'dismiss' | 'escape' | 'backdrop' | 'programmatic' | 'deactivated'
const openDialogs: HTMLDialogElement[] = []
let previousOverflow = ''
export function registerDialog(dialog: HTMLDialogElement, opener: HTMLElement | null) {
  if (!openDialogs.length) { previousOverflow = document.documentElement.style.overflow; document.documentElement.style.overflow = 'hidden' }
  openDialogs.push(dialog)
  let released = false
  return () => {
    if (released) return
    released = true
    const wasTop = openDialogs.at(-1) === dialog
    const index = openDialogs.indexOf(dialog)
    if (index >= 0) openDialogs.splice(index, 1)
    if (!openDialogs.length) document.documentElement.style.overflow = previousOverflow
    if (!wasTop) return
    const remaining = openDialogs.at(-1)
    if (opener?.isConnected && !opener.matches(':disabled') && opener.getClientRects().length > 0 && (!remaining || remaining.contains(opener))) opener.focus({ preventScroll: true })
    else (remaining ?? document.querySelector<HTMLElement>('#main'))?.focus({ preventScroll: true })
  }
}
export function trapDialogTab(event: KeyboardEvent, dialog: HTMLDialogElement) {
  if (event.key !== 'Tab') return
  const targets = [...dialog.querySelectorAll<HTMLElement>('a[href],button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex]:not([tabindex="-1"])')].filter(node => node.getClientRects().length && node.tabIndex >= 0 && !node.matches(':disabled'))
  const first = targets[0], last = targets.at(-1), current = document.activeElement
  if (!first || !last) { event.preventDefault(); dialog.focus(); return }
  if (event.shiftKey && (current === first || !targets.includes(current as HTMLElement))) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && (current === last || !targets.includes(current as HTMLElement))) { event.preventDefault(); first.focus() }
}
