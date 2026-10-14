import { ref, onMounted, onUnmounted } from 'vue'
export function usePageVisibility(hidden: () => void, visible: () => void) {
  const pageVisible = ref(document.visibilityState !== 'hidden')
  const changed = () => {
    pageVisible.value = document.visibilityState !== 'hidden'
    if (pageVisible.value) visible()
    else hidden()
  }
  onMounted(() => document.addEventListener('visibilitychange', changed))
  onUnmounted(() => document.removeEventListener('visibilitychange', changed))
  return pageVisible
}
