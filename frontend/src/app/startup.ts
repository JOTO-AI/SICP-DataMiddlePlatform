import { onMounted, onUnmounted, watch } from 'vue'
import { restoreSession, session } from '../session'
import { followLegacyHash, router, rememberReturnTarget, consumeReturnTarget } from './router'

export function useConsoleStartup() {
  let active = true, started = false
  onMounted(async () => {
    await router.isReady()
    if (!active) return
    await followLegacyHash()
    if (!active) return
    started = true
    window.addEventListener('hashchange', followLegacyHash)
    void restoreSession()
  })
  watch(() => [session.loading, session.operator, router.currentRoute.value.fullPath], () => {
    if (!started || session.loading) return
    const route = router.currentRoute.value
    if (!session.operator && route.name !== 'login') {
      rememberReturnTarget(route.fullPath)
      void router.replace({ name: 'login' })
    } else if (session.operator && route.name === 'login') {
      void router.replace(consumeReturnTarget(route.query.returnTo))
    }
  }, { flush: 'post' })
  onUnmounted(() => { active = false; window.removeEventListener('hashchange', followLegacyHash) })
}
