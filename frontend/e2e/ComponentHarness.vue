<script setup lang="ts">
import { ref } from 'vue'
import Dialog from '../src/components/Dialog.vue'
import Drawer from '../src/components/Drawer.vue'
import AsyncActionDialog from '../src/components/AsyncActionDialog.vue'
import CursorTable from '../src/components/CursorTable.vue'
import ConnectionStatus from '../src/components/ConnectionStatus.vue'
import CopyId from '../src/components/CopyId.vue'
import Timestamp from '../src/components/Timestamp.vue'
import StatusBadge from '../src/components/StatusBadge.vue'
const modalOpen = ref(false), drawerOpen = ref(false), actionOpen = ref(false), actionBusy = ref(false), unconfirmed = ref(false)
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
  <div class="actions"><button @click="modalOpen = true">打开弹窗</button><button @click="actionOpen = true; actionBusy = false; unconfirmed = false">打开异步弹窗</button></div>
  <p v-if="unconfirmed" role="status">已停止等待，受理结果仍待确认。</p>
  <Dialog v-model:open="modalOpen" title="组件弹窗" description="核对编号并查看详细记录。" @close="record(`dialog:${$event}`)">
    <label>筛选编号<input :value="fixture.rows[0]?.student_number"></label><button @click="drawerOpen = true">打开抽屉</button>
    <template #footer><button @click="modalOpen = false">完成查看</button></template>
  </Dialog>
  <Drawer v-model:open="drawerOpen" title="记录详情" @close="record(`drawer:${$event}`)"><div style="min-height: 1200px"><CursorTable :rows="fixture.rows" :columns="columns" row-key="student_number" /></div></Drawer>
  <AsyncActionDialog v-model:open="actionOpen" title="确认异步操作" description="确认后提交一次操作。" :busy="actionBusy" @confirm="actionBusy = true; record('confirm')" @close="record(`action:${$event}`); unconfirmed = actionBusy" />
</main></template>
