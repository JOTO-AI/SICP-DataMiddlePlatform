<script setup lang="ts">
defineProps<{ name: string }>()
</script>
<template>
<svg class="nav-icon" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
  <template v-if="name === 'overview'"><path d="M4 20V10h4v10M10 20V4h4v16M16 20v-7h4v7"/></template>
  <template v-else-if="name === 'queries'"><path d="m10 14 4-4M8 16l-1 1a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0M16 8l1-1a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0" transform="translate(1 -1) scale(.92)"/></template>
  <template v-else-if="name === 'tasks'"><circle cx="12" cy="12" r="9"/><path d="M12 7v6l4 2"/></template>
  <template v-else-if="name === 'sources'"><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0"/></template>
  <template v-else-if="name === 'logs'"><rect x="5" y="3" width="14" height="18" rx="2"/><path d="M9 7h6M9 11h6M9 15h4"/></template>
  <template v-else-if="name === 'notifications'"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/></template>
  <template v-else><circle cx="12" cy="12" r="7"/><circle cx="12" cy="12" r="2.5"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M5 19l2-2M17 7l2-2"/></template>
</svg>
</template>
<style scoped>.nav-icon { flex-shrink: 0; }</style>
