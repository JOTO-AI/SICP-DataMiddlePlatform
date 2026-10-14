import { onMounted, onUnmounted } from 'vue'
import { restoreSession } from '../session'
import { followLegacyHash } from './router'

export function useConsoleStartup() {
  onMounted(() => {
    followLegacyHash()
    window.addEventListener('hashchange', followLegacyHash)
    void restoreSession()
  })
  onUnmounted(() => window.removeEventListener('hashchange', followLegacyHash))
}
