<template>
  <div id="app">
    <header class="app-header">
      <div class="app-brand">
        <img class="app-logo" :src="logoURL" alt="" width="36" height="36" />
        <h2>{{ siteTitle }}</h2>
      </div>
      <nav v-if="canAccess" class="tabs" aria-label="主导航">
        <button :class="{ active: activeTab === 'deploy' }" @click="activeTab = 'deploy'">部署</button>
        <button :class="{ active: activeTab === 'history' }" @click="activeTab = 'history'">部署历史</button>
        <button :class="{ active: activeTab === 'config' }" @click="openConfigTab">配置管理</button>
      </nav>
      <span v-if="accessSession?.authenticated" class="current-user" aria-label="当前用户">{{ accessSession.display_name }}</span>
    </header>

    <main v-if="!accessSession?.authenticated" class="login-gate">
      <section class="login-card" aria-labelledby="login-heading">
        <template v-if="accessLoading"><h1 id="login-heading">正在检查访问权限</h1><p>请稍候…</p></template>
        <template v-else>
          <h1 id="login-heading">登录后才可操作</h1>
          <p>{{ accessError || loginError || '请使用统一身份认证登录，继续访问自动部署平台。' }}</p>
          <button v-if="accessError" class="primary-btn" @click="refreshSession">重试</button>
          <a v-else class="primary-btn login-button" :href="loginURL">登录</a>
        </template>
      </section>
    </main>
    <main v-else-if="!canAccess" class="login-gate">
      <div class="dialog-backdrop">
        <section class="login-card" role="alertdialog" aria-modal="true" aria-labelledby="denied-heading" aria-describedby="denied-description">
          <h1 id="denied-heading">没有访问权限</h1>
          <p id="denied-description">当前账号不在访问白名单内，请联系管理员。加入白名单后刷新即可访问，无需重新登录。</p>
          <button class="primary-btn" :disabled="accessLoading" @click="refreshSession">{{ accessLoading ? '检查中…' : '刷新权限' }}</button>
        </section>
      </div>
    </main>
    <main v-else-if="activeTab === 'deploy'" class="app-layout">
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

    <div v-if="showAuthDialog && canAccess" class="dialog-backdrop" @click.self="closeAuthDialog">
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
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import DeployPage from './views/DeployPage.vue'
import HistoryPage from './views/HistoryPage.vue'
import ConfigPage from './views/ConfigPage.vue'
import EnvironmentLinks from './components/EnvironmentLinks.vue'
import logoURL from './assets/cicd-logo.png'
import { fetchEditableConfig, fetchSiteInfo, oidcLoginURL } from './api/index'
import { accessSession, accessLoading, accessError, refreshSession, requireLogin, denyAccess } from './auth'

type Tab = 'deploy' | 'history' | 'config'

const historyRef = ref<InstanceType<typeof HistoryPage> | null>(null)
const siteTitle = ref('自动部署平台')
const canAccess = computed(() => !!accessSession.value?.authenticated && accessSession.value.authorized)
const activeTab = ref<Tab>('deploy')
const adminToken = ref('')
const authError = ref('')
const showAuthDialog = ref(false)
const tokenInput = ref('')
const dialogError = ref('')
const verifyingToken = ref(false)
const loginError = ref('')
const loginURL = computed(() => oidcLoginURL(window.location.pathname + window.location.search + window.location.hash))
let expiryTimer: ReturnType<typeof setTimeout> | undefined

function onLoginRequired() {
  requireLogin()
  showAuthDialog.value = false
  adminToken.value = ''
  tokenInput.value = ''
  activeTab.value = 'deploy'
}

function onAccessDenied() {
  denyAccess()
  void refreshSession()
}

function scheduleExpiry() {
  clearTimeout(expiryTimer)
  const expires = accessSession.value?.expires_at
  if (!expires || !accessSession.value?.authenticated) return
  const remaining = new Date(expires).getTime() - Date.now()
  if (remaining <= 0) { onLoginRequired(); return }
  // Browser timers are limited to a signed 32-bit millisecond interval.
  expiryTimer = setTimeout(scheduleExpiry, Math.min(remaining, 2_147_000_000))
}

function checkOnFocus() { void refreshSession() }
watch(accessSession, (session) => {
  if (!session?.authenticated || !session.authorized) {
    showAuthDialog.value = false
    adminToken.value = ''
    tokenInput.value = ''
    activeTab.value = 'deploy'
  }
  scheduleExpiry()
})

onMounted(async () => {
  window.addEventListener('cicd-login-required', onLoginRequired)
  window.addEventListener('cicd-access-denied', onAccessDenied)
  window.addEventListener('focus', checkOnFocus)
  const current = new URL(window.location.href)
  if (current.searchParams.has('auth_error')) {
    loginError.value = 'SSO 登录未完成，请重试；如仍失败，请联系管理员核对应用准入和手机号授权。'
    current.searchParams.delete('auth_error')
    window.history.replaceState(null, '', current.pathname + current.search + current.hash)
  }
  await refreshSession()
  try {
    const info = await fetchSiteInfo()
    siteTitle.value = info.title
    document.title = info.title
  } catch {
    // Use the default title when site metadata is unavailable.
  }
})

onUnmounted(() => {
  window.removeEventListener('cicd-login-required', onLoginRequired)
  window.removeEventListener('cicd-access-denied', onAccessDenied)
  window.removeEventListener('focus', checkOnFocus)
  clearTimeout(expiryTimer)
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
.current-user { margin-left: auto; font-size: .9rem; color: #374151; white-space: nowrap; }
.login-gate { min-height: calc(100vh - 56px); display: grid; place-items: center; padding: 24px; box-sizing: border-box; background: #f5f7fb; }
.login-card { width: min(100%, 420px); box-sizing: border-box; padding: 36px; text-align: center; border: 1px solid #e5e7eb; border-radius: 12px; background: #fff; box-shadow: 0 8px 30px rgb(0 0 0 / 5%); }
.login-card h1 { font-size: 1.3rem; margin: 0 0 14px; }
.login-card p { color: #6b7280; line-height: 1.7; margin-bottom: 24px; }
.login-card .primary-btn { display: inline-block; padding: 10px 32px; border-radius: 6px; font-size: 1rem; text-decoration: none; cursor: pointer; }
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

.app-brand {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
}

.app-logo {
  display: block;
  width: 36px;
  height: 36px;
  flex: 0 0 auto;
  object-fit: contain;
}

.app-brand h2 {
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
  min-width: 0;
  overflow-y: auto;
}

.tab-content {
  min-width: 0;
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

  .app-brand {
    width: 100%;
    margin-bottom: 8px;
  }

  .app-layout {
    flex-direction: column;
  }

  .panel-left,
  .panel-right {
    width: 100%;
    min-width: 0;
  }

  .panel-left {
    border-right: 0;
    border-bottom: 1px solid #e5e7eb;
  }
}
</style>
