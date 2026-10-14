import { onDeactivated, onUnmounted } from 'vue'
import { createRequestScope } from '../app/requestScope'
export function useRequestScope() {
  const scope = createRequestScope()
  onDeactivated(scope.cancel)
  onUnmounted(scope.cancel)
  return scope
}
