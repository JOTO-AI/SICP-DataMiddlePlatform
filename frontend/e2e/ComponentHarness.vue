<script setup lang="ts">
import { ref } from 'vue'
import CursorTable from '../src/components/CursorTable.vue'
import ConnectionStatus from '../src/components/ConnectionStatus.vue'
import CopyId from '../src/components/CopyId.vue'
import Timestamp from '../src/components/Timestamp.vue'
import StatusBadge from '../src/components/StatusBadge.vue'
const fixture = window.componentFixture
const rows = ref(fixture.rows), loading = ref(false), error = ref('')
window.componentEvents = []
const record = (value: string) => { window.componentEvents.push(value) }
const columns = [{ key: 'student_number', title: '学号' }, { key: 'name', title: '姓名' }, { key: 'department.organization_name', title: '单位' }]
</script>
<template><main>
  <h1>公共组件验证</h1>
  <ConnectionStatus state="disconnected" detail="请检查连接后重新读取。" />
  <Timestamp :value="fixture.syncedAt" />
  <CopyId :value="fixture.requestId" label="请求号" />
  <StatusBadge value="future_protocol_state" />
  <div class="actions"><button @click="loading = !loading">切换等待</button><button @click="rows = []; error = '日志游标已过期，请重新查询。'">显示游标错误</button></div>
  <CursorTable :rows="rows" :columns="columns" row-key="student_number" :loading="loading" :error="error" next-cursor="opaque-protocol-cursor" previous-available @next="record($event)" @previous="record('previous')" @retry="record('retry')" />
</main></template>
