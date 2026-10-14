<script setup lang="ts">
import { ref } from 'vue'
import ParameterTable from '../src/components/ParameterTable.vue'
import ExampleViewer from '../src/components/ExampleViewer.vue'
import RequestEditor from '../src/components/RequestEditor.vue'
import ResponseViewer from '../src/components/ResponseViewer.vue'
const fixture = window.jsonFixture
const draft = ref(window.jsonFixture.request), response = ref(window.jsonFixture.response)
const editor = ref<InstanceType<typeof RequestEditor> | null>(null)
const parameters = [{ name: 'student_number', type: 'string', required: true, description: '学号' }, { name: 'academic_year', type: 'string', description: '学年或 all' }]
function readDraft() { window.jsonSubmitText = editor.value?.getRequestText() ?? null }
</script>
<template><main id="main" tabindex="-1"><h1>接口组件验证</h1><ParameterTable :parameters="parameters" /><RequestEditor ref="editor" v-model="draft" :parameters="parameters" /><button @click="readDraft">读取编辑稿</button><ExampleViewer :text="fixture.request" loadable @load="editor?.loadText($event)" /><ResponseViewer :text="response" :http-status="200" /><button @click="response = fixture.response">恢复后端响应</button><button @click="response = '<img src=x onerror=alert(1)>'">验证非 JSON 文本</button></main></template>
