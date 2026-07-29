<template>
  <div class="history-page">
    <h1>部署历史</h1>
    <p class="subtitle">查看历史部署记录。</p>

    <!-- Filter controls -->
    <div class="filter-bar">
      <div class="filter-group">
        <label for="filter-project">项目关键字</label>
        <input
          id="filter-project"
          v-model="filter.project_keyword"
          type="text"
          placeholder="项目名称或标签"
        />
      </div>
      <div class="filter-group">
        <label for="filter-sub-project">组件关键字</label>
        <input
          id="filter-sub-project"
          v-model="filter.sub_project_keyword"
          type="text"
          placeholder="组件名称或标签"
        />
      </div>
      <div class="filter-group">
        <label for="filter-env">环境</label>
        <select id="filter-env" v-model="filter.environment">
          <option value="">全部</option>
          <option
            v-for="env in environments"
            :key="env.key"
            :value="env.key"
          >
            {{ env.label }}
          </option>
        </select>
      </div>
      <div class="filter-group">
        <label for="filter-status">状态</label>
        <select id="filter-status" v-model="filter.status">
          <option value="">全部</option>
          <option
            v-for="option in statusOptions"
            :key="option.value"
            :value="option.value"
          >
            {{ option.label }}
          </option>
        </select>
      </div>
      <button class="search-btn" @click="handleSearch" :disabled="loading">
        查询
      </button>
      <button class="reset-btn" @click="handleReset" :disabled="loading">
        重置
      </button>
      <button class="refresh-btn" @click="handleRefresh" :disabled="loading">
        刷新
      </button>
      <button class="clear-btn" @click="handleClear" :disabled="loading || total === 0">
        &#28165;&#31354;
      </button>
    </div>

    <!-- Error alert -->
    <div v-if="errorMessage" class="error-alert" role="alert">
      {{ errorMessage }}
      <button class="error-close" @click="errorMessage = ''" aria-label="关闭错误提示">&times;</button>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="loading-state">加载中…</div>

    <!-- Records table -->
    <div v-else-if="records.length > 0" class="records-section">
      <table class="records-table">
        <thead>
          <tr>
            <th>项目名称</th>
            <th>组件</th>
            <th>分支/Tag</th>
            <th>最后 commit</th>
            <th>环境</th>
            <th>部署目标</th>
            <th>状态</th>
            <th>触发时间</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="record in records" :key="record.id">
            <tr
              class="record-row"
              :class="{ 'row-selected': selectedRecordId === record.id }"
              @click="handleRowClick(record.id)"
            >
              <td>
                <div class="name-with-tags">
                  <span class="entity-name">{{ record.project_name }}</span>
                  <span v-if="splitLabelTags(record.project_label).length" class="entity-tags">
                    <span
                      v-for="tag in splitLabelTags(record.project_label)"
                      :key="tag"
                      class="label-chip label-chip-project"
                    >
                      {{ tag }}
                    </span>
                  </span>
                </div>
              </td>
              <td>
                <div class="name-with-tags">
                  <span class="entity-name">{{ record.component || record.sub_project || '—' }}</span>
                  <span v-if="splitLabelTags(record.component_label || record.sub_project_label).length" class="entity-tags">
                    <span
                      v-for="tag in splitLabelTags(record.component_label || record.sub_project_label)"
                      :key="tag"
                      class="label-chip label-chip-subproject"
                    >
                      {{ tag }}
                    </span>
                  </span>
                </div>
              </td>
              <td>{{ record.branch }}</td>
              <td>
                <div class="commit-info">
                  <div class="commit-message">{{ record.commit_message || '-' }}</div>
                  <div class="commit-time">{{ record.commit_time ? formatTime(record.commit_time) : '暂无提交时间' }}</div>
                </div>
              </td>
              <td>{{ environmentLabel(record.environment) }}</td>
              <td>
                <div class="target-info">
                  <strong>{{ record.target_label || record.target || '—' }}</strong>
                  <small v-if="record.server">{{ record.server }}</small>
                </div>
              </td>
              <td>
                <span :class="['status-badge', statusClass(record.status)]">
                  {{ statusLabel(record.status) }}
                </span>
                <button
                  v-if="isRunningStatus(record.status)"
                  class="cancel-btn"
                  @click.stop="handleCancel(record.id)"
                  :disabled="loading"
                >
                  中断
                </button>
              </td>
              <td>{{ formatTime(record.created_at) }}</td>
            </tr>
            <tr v-if="selectedRecordId === record.id" class="log-row">
              <td colspan="8">
                <LogViewer :task-id="record.id" />
              </td>
            </tr>
          </template>
        </tbody>
      </table>

      <!-- Pagination -->
      <div class="pagination">
        <button
          class="page-btn"
          :disabled="filter.page <= 1"
          @click="handlePageChange(filter.page - 1)"
        >
          上一页
        </button>
        <span class="page-info">第 {{ filter.page }} 页 / 共 {{ totalPages }} 页（{{ total }} 条记录）</span>
        <button
          class="page-btn"
          :disabled="filter.page >= totalPages"
          @click="handlePageChange(filter.page + 1)"
        >
          下一页
        </button>
      </div>
    </div>

    <!-- Empty state -->
    <div v-else class="empty-state">暂无部署记录</div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import {
  cancelDeploy,
  clearDeployHistory,
  fetchEnvironments,
  fetchRecords,
  fetchTaskStatus,
  type DeployRecord,
  type Environment,
} from '../api/index'
import LogViewer from '../components/LogViewer.vue'

// --- Reactive state ---
const records = ref<DeployRecord[]>([])
const total = ref(0)
const environments = ref<Environment[]>([])
const environmentLabels = ref<Record<string, string>>({})
const filter = reactive({
  project_keyword: '',
  sub_project_keyword: '',
  environment: '',
  status: '',
  page: 1,
  page_size: 20,
})
const selectedRecordId = ref<string | null>(null)
const loading = ref(false)
const errorMessage = ref('')

// --- Computed ---
const totalPages = computed(() => {
  return Math.max(1, Math.ceil(total.value / filter.page_size))
})

// --- Status mapping ---
const STATUS_MAP: Record<string, { label: string; cssClass: string }> = {
  pending: { label: '等待中', cssClass: 'status-pending' },
  cloning: { label: '拉取代码中', cssClass: 'status-running' },
  building: { label: '构建中', cssClass: 'status-running' },
  deploying: { label: '部署中', cssClass: 'status-running' },
  success: { label: '成功', cssClass: 'status-success' },
  failed: { label: '失败', cssClass: 'status-failed' },
}

const statusOptions = Object.entries(STATUS_MAP).map(([value, meta]) => ({
  value,
  label: meta.label,
}))

function statusLabel(status: string): string {
  return STATUS_MAP[status]?.label ?? status
}

function statusClass(status: string): string {
  return STATUS_MAP[status]?.cssClass ?? 'status-pending'
}

function isRunningStatus(status: string): boolean {
  return status === 'cloning' || status === 'building' || status === 'deploying'
}

function environmentLabel(env: string): string {
  return environmentLabels.value[env] ?? env
}

function formatTime(timeStr: string): string {
  if (!timeStr) return '-'
  try {
    const date = new Date(timeStr)
    return date.toLocaleString('zh-CN')
  } catch {
    return timeStr
  }
}

// --- Data loading ---
async function loadRecords(showLoading = true) {
  if (showLoading) loading.value = true
  errorMessage.value = ''
  try {
    const params: Record<string, string | number> = {
      page: filter.page,
      page_size: filter.page_size,
    }
    if (filter.project_keyword) {
      params.project_keyword = filter.project_keyword
    }
    if (filter.sub_project_keyword) {
      params.sub_project_keyword = filter.sub_project_keyword
    }
    if (filter.environment) {
      params.environment = filter.environment
    }
    if (filter.status) {
      params.status = filter.status
    }
    const result = await fetchRecords(params)
    records.value = result.records ?? []
    total.value = result.total
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : '未知错误'
    errorMessage.value = `获取部署记录失败：${message}`
  } finally {
    if (showLoading) loading.value = false
  }
}

function handleSearch() {
  filter.page = 1
  selectedRecordId.value = null
  loadRecords()
}

function handleReset() {
  filter.project_keyword = ''
  filter.sub_project_keyword = ''
  filter.environment = ''
  filter.status = ''
  filter.page = 1
  selectedRecordId.value = null
  loadRecords()
}

async function handleClear() {
  if (!confirm('\u662f\u5426\u786e\u8ba4\u6e05\u7a7a\u90e8\u7f72\u5386\u53f2\uff1f')) return

  loading.value = true
  errorMessage.value = ''
  try {
    await clearDeployHistory()
    selectedRecordId.value = null
    filter.page = 1
    records.value = []
    total.value = 0
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : '\u672a\u77e5\u9519\u8bef'
    errorMessage.value = `\u6e05\u7a7a\u90e8\u7f72\u5386\u53f2\u5931\u8d25\uff1a${message}`
  } finally {
    loading.value = false
  }
}

function splitLabelTags(label: string): string[] {
  return (label || '')
    .split('/')
    .map(item => item.trim())
    .filter(Boolean)
}

function handleRefresh() {
  loadRecords(false)
}

function handleRowClick(recordId: string) {
  if (selectedRecordId.value === recordId) {
    selectedRecordId.value = null
  } else {
    selectedRecordId.value = recordId
  }
}

function handlePageChange(page: number) {
  filter.page = page
  selectedRecordId.value = null
  loadRecords()
}

async function handleCancel(taskId: string) {
  if (!confirm('确定要中断该部署任务吗？')) return
  try {
    await cancelDeploy(taskId)
    loadRecords(false)
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : '未知错误'
    errorMessage.value = `中断失败：${message}`
  }
}

// --- Lifecycle ---
let pollingTimer: ReturnType<typeof setInterval> | null = null
let pollingStatuses = false

async function refreshRunningStatuses() {
  if (pollingStatuses) return
  const runningRecords = records.value.filter(record => isRunningStatus(record.status))
  if (runningRecords.length === 0) return

  pollingStatuses = true
  try {
    await Promise.all(runningRecords.map(async (record) => {
      try {
        const taskStatus = await fetchTaskStatus(record.id)
        // Mutate only the status field. Keeping the existing record objects and
        // table tree intact prevents an open LogViewer from being remounted.
        record.status = taskStatus.status
      } catch {
        // A transient status request must not replace the whole page with an
        // error or interrupt the user's current log reading position.
      }
    }))
  } finally {
    pollingStatuses = false
  }
}

function startPolling() {
  stopPolling()
  pollingTimer = setInterval(refreshRunningStatuses, 5000)
}

function stopPolling() {
  if (pollingTimer) {
    clearInterval(pollingTimer)
    pollingTimer = null
  }
}

// Expose refresh method for parent component
function refresh() {
  loadRecords(false)
}

defineExpose({ refresh })

onMounted(async () => {
  // Load environment labels from API
  try {
    const envs = await fetchEnvironments()
    environments.value = envs
    const labels: Record<string, string> = {}
    for (const env of envs) {
      labels[env.key] = env.label
    }
    environmentLabels.value = labels
  } catch {
    // Fallback: use key as label
  }

  await loadRecords()
  startPolling()
})

onUnmounted(() => {
  stopPolling()
})
</script>

<style scoped>
.history-page {
  margin: 0;
  padding: 24px 16px;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  color: #1a1a1a;
}

h1 {
  font-size: 1.5rem;
  margin: 0 0 4px;
}

.subtitle {
  color: #666;
  margin: 0 0 24px;
  font-size: 0.95rem;
}

/* Filter bar */
.filter-bar {
  display: flex;
  align-items: flex-end;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.filter-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.filter-group label {
  font-weight: 600;
  font-size: 0.85rem;
  color: #374151;
}

.filter-group input,
.filter-group select {
  padding: 8px 12px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 0.9rem;
  background: #fff;
  color: #1a1a1a;
  min-width: 180px;
}

.search-btn {
  padding: 8px 20px;
  background: #2563eb;
  color: #fff;
  border: none;
  border-radius: 6px;
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s;
}

.search-btn:hover:not(:disabled) {
  background: #1d4ed8;
}

.search-btn:disabled {
  background: #93c5fd;
  cursor: not-allowed;
}

.reset-btn {
  padding: 8px 20px;
  background: #fff;
  color: #4b5563;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.reset-btn:hover:not(:disabled) {
  background: #f9fafb;
  color: #111827;
}

.reset-btn:disabled {
  color: #d1d5db;
  cursor: not-allowed;
}

.refresh-btn {
  padding: 8px 20px;
  background: #fff;
  color: #2563eb;
  border: 1px solid #2563eb;
  border-radius: 6px;
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.refresh-btn:hover:not(:disabled) {
  background: #eff6ff;
}

.refresh-btn:disabled {
	color: #93c5fd;
	border-color: #93c5fd;
	cursor: not-allowed;
}

.clear-btn {
	margin-left: auto;
	padding: 8px 20px;
	background: #fff;
	color: #dc2626;
	border: 1px solid #dc2626;
	border-radius: 6px;
	font-size: 0.9rem;
	font-weight: 600;
	cursor: pointer;
	transition: background 0.15s, color 0.15s;
}

.clear-btn:hover:not(:disabled) {
	background: #fef2f2;
}

.clear-btn:disabled {
	color: #fca5a5;
	border-color: #fca5a5;
	cursor: not-allowed;
}

/* Error alert */
.error-alert {
  background: #fef2f2;
  border: 1px solid #fca5a5;
  color: #b91c1c;
  padding: 12px 16px;
  border-radius: 6px;
  margin-bottom: 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.9rem;
}

.error-close {
  background: none;
  border: none;
  color: #b91c1c;
  font-size: 1.2rem;
  cursor: pointer;
  padding: 0 0 0 12px;
  line-height: 1;
}

/* Loading state */
.loading-state {
  text-align: center;
  padding: 40px 0;
  color: #6b7280;
  font-size: 0.95rem;
}

/* Empty state */
.empty-state {
  text-align: center;
  padding: 40px 0;
  color: #9ca3af;
  font-size: 0.95rem;
}

/* Records table */
.records-table {
  width: 100%;
  border-collapse: collapse;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  overflow: hidden;
}

.records-table thead {
  background: #f9fafb;
}

.records-table th {
  padding: 10px 14px;
  text-align: left;
  font-size: 0.85rem;
  font-weight: 600;
  color: #374151;
  border-bottom: 1px solid #e5e7eb;
}

.records-table td {
  padding: 10px 14px;
  font-size: 0.9rem;
  border-bottom: 1px solid #f3f4f6;
}

.name-with-tags {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  min-width: 140px;
}

.entity-name {
  color: #111827;
  font-weight: 500;
  overflow-wrap: anywhere;
}

.entity-tags {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.label-chip {
  display: inline-flex;
  align-items: center;
  border: 1px solid;
  border-radius: 3px;
  padding: 1px 6px;
  font-size: 0.72rem;
  font-weight: 500;
  line-height: 1.35;
}

.label-chip-project {
  color: #1e40af;
  border-color: #bfdbfe;
  background: #eff6ff;
}

.label-chip-subproject {
  color: #047857;
  border-color: #bbf7d0;
  background: #f0fdf4;
}

.commit-info {
  max-width: 360px;
}

.commit-message {
  color: #111827;
  line-height: 1.35;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.commit-time {
  color: #6b7280;
  font-size: 0.78rem;
  margin-top: 4px;
}

.target-info {
  display: flex;
  min-width: 120px;
  flex-direction: column;
  gap: 3px;
}

.target-info small {
  color: #6b7280;
}

.record-row {
  cursor: pointer;
  transition: background 0.1s;
}

.record-row:hover {
  background: #f0f4ff;
}

.record-row.row-selected {
  background: #eff6ff;
}

.log-row td {
  padding: 0;
  background: #f9fafb;
}

/* Status badges */
.status-badge {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 10px;
  font-size: 0.8rem;
  font-weight: 500;
}

.status-pending {
  background: #f3f4f6;
  color: #6b7280;
}

.status-running {
  background: #dbeafe;
  color: #1d4ed8;
}

.status-success {
  background: #dcfce7;
  color: #166534;
}

.status-failed {
  background: #fee2e2;
  color: #b91c1c;
}

.cancel-btn {
  margin-left: 8px;
  padding: 2px 10px;
  background: #fff;
  color: #dc2626;
  border: 1px solid #dc2626;
  border-radius: 10px;
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.cancel-btn:hover:not(:disabled) {
  background: #fef2f2;
}

.cancel-btn:disabled {
  color: #fca5a5;
  border-color: #fca5a5;
  cursor: not-allowed;
}

/* Pagination */
.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  margin-top: 16px;
}

.page-btn {
  padding: 6px 16px;
  background: #fff;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 0.85rem;
  cursor: pointer;
  color: #374151;
  transition: background 0.15s;
}

.page-btn:hover:not(:disabled) {
  background: #f3f4f6;
}

.page-btn:disabled {
  color: #d1d5db;
  cursor: not-allowed;
}

.page-info {
  font-size: 0.85rem;
  color: #6b7280;
}
</style>
