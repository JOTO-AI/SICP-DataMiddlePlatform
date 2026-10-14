<script setup lang="ts">
import { ref } from 'vue'
import FilterBar from '../src/components/FilterBar.vue'
const busy = ref(false), count = ref(0)
</script>
<template><main><h1>控件状态验证</h1><button @click="busy = !busy">切换加载</button><FilterBar :busy="busy" @submit="count++"><div class="form-grid"><label>输入<input placeholder="请输入编号"></label><label>选择<select><option>全部</option></select></label><label>文本<textarea placeholder="请输入内容" /></label><label>错误输入<input aria-invalid="true" aria-describedby="state-error"><span id="state-error" class="field-error">请检查格式。</span></label></div><label class="check-row"><input type="checkbox">选择此项</label><label class="radio-row"><input type="radio">使用此项</label><div class="actions"><button class="primary">{{ busy ? '正在读取…' : '读取数据' }}</button><button type="button" disabled>暂不可用</button></div><p class="hint">只读校验；不发送请求。</p></FilterBar><p role="status">提交次数 {{ count }}</p><details><summary>更多说明</summary><p>状态同时通过文字表达。</p></details><a href="#main">查看说明</a><div class="update-progress"><span class="hint">浅蓝底说明文字</span></div><p class="error">读取失败，请重试。</p><p class="warning">当前结果仍待确认。</p></main></template>
