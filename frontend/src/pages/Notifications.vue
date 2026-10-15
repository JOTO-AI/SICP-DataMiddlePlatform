<script setup lang="ts">
import { ref, onDeactivated } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import NotificationHistory from '../components/notifications/NotificationHistory.vue'
import DeliveryDetail from '../components/notifications/DeliveryDetail.vue'
import MailSettings from '../components/notifications/MailSettings.vue'
const selected = ref('')
onDeactivated(() => { selected.value = '' })
</script>
<template><section><PageHeader title="通知中心" description="分别查看异常决策与逐人投递，记录以服务端持久状态为准。" /><MailSettings /><NotificationHistory kind="decisions" /><NotificationHistory kind="deliveries" @select="selected = $event" /><section v-if="selected" class="panel" aria-label="投递详情"><div class="section-bar"><h2>投递详情</h2><button @click="selected = ''">关闭投递详情</button></div><DeliveryDetail :key="selected" :id="selected" /></section></section></template>