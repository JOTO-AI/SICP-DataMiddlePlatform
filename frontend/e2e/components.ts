import { createApp } from 'vue'
import Harness from './ComponentHarness.vue'
import '../src/styles/tokens.css'
import '../src/style.css'
declare global {
  interface Window { componentFixture: { rows: Record<string, unknown>[]; syncedAt: string; requestId: string }; componentEvents: string[] }
}
createApp(Harness).mount('#app')
