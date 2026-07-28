<template>
  <div id="app">
    <header class="app-header">
      <h2>{{ siteTitle }}</h2>
      <nav class="tabs" aria-label="主导航">
        <button :class="{ active: activeTab === 'deploy' }" @click="activeTab = 'deploy'">部署</button>
        <button :class="{ active: activeTab === 'history' }" @click="activeTab = 'history'">部署历史</button>
        <button :class="{ active: activeTab === 'config' }" @click="openConfigTab">配置管理</button>
      </nav>
    </header>

    <main v-if="activeTab === 'deploy'" class="app-layout">
      <div class="panel-left">
        <DeployPage @deployed="onDeployed" />
      </div>
      <div class="panel-right">
        <EnvironmentLinks />
        <HistoryPage ref="historyRef" />
      </div>
    </main>
    <main v-else-if="activeTab === 'history'" class="tab-content">
      <HistoryPage ref="historyRef" />
    </main>
    <main v-else class="tab-content">
      <ConfigPage :admin-token="adminToken" />
    </main>

    <div v-if="showAuthDialog" class="dialog-backdrop" @click.self="closeAuthDialog">
      <form class="auth-dialog" @submit.prevent="verifyAdminToken">
        <h3>配置管理验证</h3>
        <label for="admin-token-input">admin_token</label>
        <input
          id="admin-token-input"
          v-model="tokenInput"
          type="password"
          autocomplete="current-password"
          autofocus
          :disabled="verifyingToken"
        />
        <p v-if="dialogError" class="dialog-error">{{ dialogError }}</p>
        <div class="dialog-actions">
          <button type="button" class="secondary-btn" :disabled="verifyingToken" @click="closeAuthDialog">取消</button>
          <button type="submit" class="primary-btn" :disabled="verifyingToken || !tokenInput">
            {{ verifyingToken ? '验证中...' : '确认' }}
          </button>
        </div>
      </form>
    </div>

    <div v-if="authError" class="auth-error" role="alert">{{ authError }}</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import DeployPage from './views/DeployPage.vue'
import HistoryPage from './views/HistoryPage.vue'
import ConfigPage from './views/ConfigPage.vue'
import EnvironmentLinks from './components/EnvironmentLinks.vue'
import { fetchEditableConfig, fetchSiteInfo } from './api/index'

type Tab = 'deploy' | 'history' | 'config'

const historyRef = ref<InstanceType<typeof HistoryPage> | null>(null)
const siteTitle = ref('自动部署平台')
const activeTab = ref<Tab>('deploy')
const adminToken = ref('')
const authError = ref('')
const showAuthDialog = ref(false)
const tokenInput = ref('')
const dialogError = ref('')
const verifyingToken = ref(false)

onMounted(async () => {
  try {
    const info = await fetchSiteInfo()
    siteTitle.value = info.title
    document.title = info.title
  } catch {
    // Use the default title when site metadata is unavailable.
  }
})

function openConfigTab() {
  tokenInput.value = ''
  dialogError.value = ''
  showAuthDialog.value = true
}

function closeAuthDialog() {
  if (!verifyingToken.value) {
    showAuthDialog.value = false
  }
}

async function verifyAdminToken() {
  const token = tokenInput.value
  verifyingToken.value = true
  authError.value = ''
  dialogError.value = ''
  try {
    await fetchEditableConfig(token)
    adminToken.value = token
    activeTab.value = 'config'
    showAuthDialog.value = false
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : '未知错误'
    dialogError.value = `验证失败：${message}`
  } finally {
    verifyingToken.value = false
  }
}

function onDeployed() {
  historyRef.value?.refresh()
}
</script>

<style scoped>
#app {
  min-height: 100vh;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  color: #1a1a1a;
}

.app-header {
  display: flex;
  align-items: center;
  gap: 32px;
  padding: 0 24px;
  min-height: 56px;
  background: #f9fafb;
  border-bottom: 1px solid #e5e7eb;
}

.app-header h2 {
  flex: 0 0 auto;
  margin: 0;
  font-size: 1.1rem;
  font-weight: 600;
}

.tabs {
  align-self: stretch;
  display: flex;
  gap: 4px;
}

.tabs button {
  padding: 0 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: #4b5563;
  font-size: 0.9rem;
  cursor: pointer;
}

.tabs button:hover {
  color: #1d4ed8;
}

.tabs button.active {
  border-bottom-color: #2563eb;
  color: #1d4ed8;
  font-weight: 600;
}

.app-layout {
  display: flex;
  min-height: calc(100vh - 56px);
}

.panel-left {
  width: 40%;
  min-width: 320px;
  border-right: 1px solid #e5e7eb;
  overflow: visible;
}

.panel-right {
  width: 60%;
  overflow-y: auto;
}

.tab-content {
  min-height: calc(100vh - 56px);
  overflow-y: auto;
}

.auth-error {
  position: fixed;
  right: 24px;
  bottom: 24px;
  max-width: min(420px, calc(100vw - 48px));
  padding: 10px 14px;
  border: 1px solid #fca5a5;
  border-radius: 6px;
  background: #fef2f2;
  color: #b91c1c;
  font-size: 0.9rem;
}

.dialog-backdrop {
  position: fixed;
  z-index: 10;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgb(17 24 39 / 45%);
}

.auth-dialog {
  display: flex;
  width: min(100%, 360px);
  flex-direction: column;
  gap: 10px;
  padding: 20px;
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 12px 30px rgb(0 0 0 / 20%);
}

.auth-dialog h3 {
  margin: 0 0 4px;
  font-size: 1.05rem;
}

.auth-dialog label {
  color: #374151;
  font-size: 0.9rem;
  font-weight: 600;
}

.auth-dialog input {
  padding: 8px 10px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 0.95rem;
}

.dialog-error {
  margin: 0;
  color: #b91c1c;
  font-size: 0.85rem;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 6px;
}

.dialog-actions button {
  padding: 7px 14px;
  border-radius: 6px;
  font-size: 0.9rem;
  cursor: pointer;
}

.primary-btn {
  border: 1px solid #2563eb;
  background: #2563eb;
  color: #fff;
}

.secondary-btn {
  border: 1px solid #d1d5db;
  background: #fff;
  color: #374151;
}

.dialog-actions button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

@media (max-width: 720px) {
  .app-header {
    flex-wrap: wrap;
    gap: 0;
    padding: 8px 16px 0;
  }

  .app-header h2 {
    width: 100%;
    margin-bottom: 8px;
  }

  .app-layout {
    flex-direction: column;
  }

  .panel-left,
  .panel-right {
    width: 100%;
  }

  .panel-left {
    border-right: 0;
    border-bottom: 1px solid #e5e7eb;
  }
}
</style>
