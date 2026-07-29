<template>
  <div class="log-viewer">
    <div class="log-header">
      <h3>执行日志</h3>
      <button class="refresh-btn" @click="loadLogs" :disabled="loading">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <div v-if="loading && !logs" class="log-loading">
      加载日志中…
    </div>

    <div v-if="errorMessage" class="log-error" role="alert">
      {{ errorMessage }}
    </div>

    <pre v-if="logs !== null" ref="logContentRef" class="log-content">{{ logs }}</pre>
    <pre v-else-if="!loading && !errorMessage" class="log-content log-empty">暂无日志内容</pre>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref, onMounted } from 'vue'
import { fetchTaskLogs } from '../api/index'

const props = defineProps<{
  taskId: string
}>()

const logs = ref<string | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const logContentRef = ref<HTMLElement | null>(null)

async function loadLogs() {
  const previousScrollTop = logContentRef.value?.scrollTop ?? 0
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await fetchTaskLogs(props.taskId)
    logs.value = result.logs || ''
    await nextTick()
    if (logContentRef.value) {
      logContentRef.value.scrollTop = previousScrollTop
    }
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : '未知错误'
    errorMessage.value = `获取日志失败：${message}`
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadLogs()
})
</script>

<style scoped>
.log-viewer {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  overflow: hidden;
  margin-top: 16px;
}

.log-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background: #f9fafb;
  border-bottom: 1px solid #e5e7eb;
}

.log-header h3 {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: #1a1a1a;
}

.refresh-btn {
  padding: 4px 14px;
  background: #2563eb;
  color: #fff;
  border: none;
  border-radius: 4px;
  font-size: 0.85rem;
  cursor: pointer;
  transition: background 0.15s;
}

.refresh-btn:hover:not(:disabled) {
  background: #1d4ed8;
}

.refresh-btn:disabled {
  background: #93c5fd;
  cursor: not-allowed;
}

.log-loading {
  padding: 24px 16px;
  text-align: center;
  color: #6b7280;
  font-size: 0.9rem;
}

.log-error {
  padding: 10px 16px;
  background: #fef2f2;
  border-bottom: 1px solid #fca5a5;
  color: #b91c1c;
  font-size: 0.85rem;
}

.log-content {
  margin: 0;
  padding: 16px;
  background: #1e1e1e;
  color: #d4d4d4;
  font-family: 'Courier New', Courier, monospace;
  font-size: 0.85rem;
  line-height: 1.6;
  max-height: 400px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.log-empty {
  color: #6b7280;
  font-style: italic;
}
</style>
