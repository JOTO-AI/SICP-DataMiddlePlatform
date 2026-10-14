import { createApp } from 'vue'
import Harness from './JsonHarness.vue'
import '../src/styles/tokens.css'
import '../src/style.css'
declare global { interface Window { jsonFixture: { request: string; response: string }; jsonCopied?: string; jsonSubmitText?: string | null } }
createApp(Harness).mount('#app')
