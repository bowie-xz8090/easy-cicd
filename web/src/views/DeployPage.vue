<template>
  <div class="deploy-page">
    <header class="page-heading">
      <div>
        <h1>部署操作</h1>
        <p>依次选择项目、代码引用、环境和部署服务器。</p>
      </div>
      <button class="secondary-btn" type="button" @click="reloadPage">刷新</button>
    </header>

    <div v-if="errorMessage" class="alert error-alert" role="alert">{{ errorMessage }}</div>
    <div v-if="deployResult && !showDeployStatus" class="alert success-alert" role="status">
      部署已触发，任务 ID：{{ deployResult.task_id }}
    </div>

    <DeployStatus
      v-if="showDeployStatus && deployResult"
      :task-id="deployResult.task_id"
      @complete="onDeployComplete"
      @close="showDeployStatus = false"
    />

    <form class="deploy-form" @submit.prevent="handleDeploy">
      <section class="form-step">
        <div class="step-title"><span>1</span><strong>项目</strong></div>
        <div ref="projectCascaderRef" class="project-cascader">
          <button
            id="project-cascader"
            class="cascader-trigger"
            type="button"
            aria-label="项目"
          :disabled="loadingProjects"
          @click="toggleProjectCascader"
          >
            <span v-if="selectedProject" class="selected-project-value">
              <strong>{{ selectedUnitName }}</strong>
              <span
                class="selected-unit-chip"
                :class="{ component: Boolean(selectedComponentInfo) }"
              >
                {{ selectedUnitLabel }}
              </span>
            </span>
            <span v-else class="cascader-placeholder">
              {{ loadingProjects ? '加载项目中...' : '请选择项目' }}
            </span>
            <span class="chevron">›</span>
          </button>

          <div v-if="isProjectCascaderOpen" class="cascader-panel">
            <div class="cascader-column">
              <button
                v-for="project in projects"
                :key="project.full_name"
                type="button"
                :class="['cascader-option', { active: activeProject?.full_name === project.full_name }]"
                @click="selectProjectLevel(project)"
              >
                <span>
                  <strong>{{ project.name }}</strong>
                  <small>{{ project.label }}</small>
                </span>
                <span v-if="project.mode === 'monorepo'">›</span>
                <span v-else class="leaf-mark">选择</span>
              </button>
            </div>
            <div v-if="activeProject?.mode === 'monorepo'" class="cascader-column component-column">
              <div v-if="activeProject.components.length === 0" class="empty-option">暂无可部署组件</div>
              <button
                v-for="component in activeProject.components"
                :key="component.key"
                type="button"
                :class="['cascader-option', { active: selectedComponent === component.key && selectedProject?.full_name === activeProject?.full_name }]"
                @click="selectComponent(activeProject, component)"
              >
                <span>
                  <strong>{{ component.key }}</strong>
                  <small>{{ component.label }}</small>
                </span>
                <span class="leaf-mark">选择</span>
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="form-step" :class="{ muted: !selectedProject }">
        <div class="step-title"><span>2</span><strong>分支或标签</strong></div>
        <div class="ref-type-toggle">
          <label><input v-model="refType" type="radio" value="branch" :disabled="!selectedProject" /> 分支</label>
          <label><input v-model="refType" type="radio" value="tag" :disabled="!selectedProject" /> 标签</label>
        </div>
        <label class="field-label" for="ref-select">{{ refType === 'branch' ? '分支' : '标签' }}</label>
        <select id="ref-select" v-model="selectedRef" :disabled="!selectedProject || loadingRefs">
          <option value="" disabled>{{ loadingRefs ? '加载中...' : `请选择${refType === 'branch' ? '分支' : '标签'}` }}</option>
          <option v-for="item in refList" :key="item.name" :value="item.name">{{ item.name }}</option>
        </select>
        <div v-if="selectedRefInfo" class="commit-card">
          <CommitMessage :key="`${refType}:${selectedRef}:${selectedRefInfo.commit_id}`" :message="selectedRefInfo.commit_message || '暂无提交说明'" :limit="100" />
          <span class="commit-meta">{{ shortCommitId(selectedRefInfo.commit_id) }} · {{ formattedSelectedCommitTime }}</span>
        </div>
      </section>

      <section class="form-step" :class="{ muted: !selectedRef }">
        <div class="step-title"><span>3</span><strong>环境</strong></div>
        <div class="choice-grid" role="radiogroup" aria-label="环境">
          <label
            v-for="env in environments"
            :key="env.key"
            :class="['choice-card', { selected: selectedEnvironment === env.key, disabled: env.disabled }]"
          >
            <input
              v-model="selectedEnvironment"
              type="radio"
              name="environment"
              :value="env.key"
              :disabled="!selectedRef || env.disabled"
              @change="selectEnvironment(env)"
            />
            <strong>{{ env.label }}</strong>
            <small v-if="env.disabled">当前项目未配置部署规则</small>
            <small v-else>{{ env.targets.length }} 个可用部署目标</small>
          </label>
        </div>
      </section>

      <section v-if="selectedEnvironment" class="form-step">
        <div class="step-title"><span>4</span><strong>部署服务器</strong></div>
        <div v-if="selectedEnvironmentInfo?.targets.length === 1" class="auto-target">
          <span class="auto-badge">已自动选择</span>
          <strong>{{ selectedEnvironmentInfo.targets[0].label }}</strong>
          <small>{{ formatServerAddress(selectedEnvironmentInfo.targets[0]) }}</small>
        </div>
        <div v-else class="choice-grid target-grid" role="radiogroup" aria-label="部署服务器">
          <label
            v-for="target in selectedEnvironmentInfo?.targets ?? []"
            :key="target.key"
            :class="['choice-card target-card', { selected: selectedTarget === target.key }]"
          >
            <input v-model="selectedTarget" type="radio" name="target" :value="target.key" />
            <strong>{{ target.label }}</strong>
            <small>{{ formatServerAddress(target) }}</small>
          </label>
        </div>
      </section>

      <div class="deploy-actions">
        <button class="deploy-btn" type="submit" :disabled="!canDeploy || deploying" :title="deployButtonHint">
          {{ deploying ? '部署中…' : '确认部署' }}
        </button>
        <span>{{ deployButtonHint }}</span>
      </div>
    </form>

    <div v-if="showConfirmModal" class="modal-backdrop">
      <form class="deployment-confirm-modal" @submit.prevent="confirmDeployment">
        <h2>确认部署</h2>
        <p>请确认本次部署信息，确认后才会创建部署任务。</p>
        <div class="deployment-summary">
          <div><span>项目</span><strong>{{ selectedUnitName }}</strong></div>
          <div v-if="selectedComponentInfo"><span>子项目</span><strong>{{ selectedUnitLabel }}</strong></div>
          <div><span>{{ refType === 'branch' ? '分支' : '标签' }}</span><strong>{{ selectedRef }}</strong></div>
          <div><span>环境</span><strong>{{ selectedEnvironmentInfo?.label }}</strong></div>
          <div><span>部署目标</span><strong>{{ selectedTargetInfo?.label }}</strong></div>
          <div><span>服务器</span><strong>{{ formatServerAddress(selectedTargetInfo) }}</strong></div>
        </div>
        <label v-if="selectedEnvironmentInfo?.need_password">
          部署密码
          <input
            ref="passwordInputRef"
            v-model="passwordInput"
            :type="showPassword ? 'text' : 'password'"
            name="deploy-verification-password"
            autocomplete="new-password"
            spellcheck="false"
          />
        </label>
        <label v-if="selectedEnvironmentInfo?.need_password" class="show-password">
          <input v-model="showPassword" type="checkbox" /> 显示密码
        </label>
        <div v-if="confirmationError" class="confirmation-error" role="alert">{{ confirmationError }}</div>
        <div class="modal-actions">
          <button type="button" class="secondary-btn" @click="cancelDeploymentConfirmation">取消</button>
          <button class="deploy-btn" type="submit" :disabled="deploying">
            {{ deploying ? '提交中…' : '确认部署' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import DeployStatus from '../components/DeployStatus.vue'
import CommitMessage from '../components/CommitMessage.vue'
import {
  fetchBranches,
  fetchEnvironments,
  fetchProjects,
  fetchTags,
  triggerDeploy,
  type Branch,
  type DeployResponse,
  type Environment,
  type DeploymentTarget,
  type Project,
  type ProjectComponent,
  type Tag,
} from '../api/index'

const emit = defineEmits<{ deployed: [] }>()

function reloadPage() {
  window.location.reload()
}
const projects = ref<Project[]>([])
const branches = ref<Branch[]>([])
const tags = ref<Tag[]>([])
const environments = ref<Environment[]>([])
const selectedProject = ref<Project | null>(null)
const activeProject = ref<Project | null>(null)
const selectedComponent = ref('')
const refType = ref<'branch' | 'tag'>('branch')
const selectedRef = ref('')
const selectedEnvironment = ref('')
const selectedTarget = ref('')
const loadingProjects = ref(false)
const loadingRefs = ref(false)
const deploying = ref(false)
const errorMessage = ref('')
const deployResult = ref<DeployResponse | null>(null)
const showDeployStatus = ref(false)
const isProjectCascaderOpen = ref(false)
const projectCascaderRef = ref<HTMLElement | null>(null)
const showConfirmModal = ref(false)
const passwordInput = ref('')
const showPassword = ref(false)
const confirmationError = ref('')
const passwordInputRef = ref<HTMLInputElement | null>(null)

const refList = computed(() => refType.value === 'branch' ? branches.value : tags.value)
const selectedRefInfo = computed(() => refList.value.find(item => item.name === selectedRef.value) ?? null)
const formattedSelectedCommitTime = computed(() => {
  if (!selectedRefInfo.value?.commit_time) return '暂无提交时间'
  return new Date(selectedRefInfo.value.commit_time).toLocaleString('zh-CN')
})
const selectedComponentInfo = computed(() => {
  if (!selectedProject.value || !selectedComponent.value) return null
  return selectedProject.value.components.find(item => item.key === selectedComponent.value) ?? null
})
const selectedUnitName = computed(() => {
  if (!selectedProject.value) return ''
  if (!selectedComponentInfo.value) return selectedProject.value.name
  return `${selectedProject.value.name} / ${selectedComponentInfo.value.key}`
})
const selectedUnitLabel = computed(() => {
  return selectedComponentInfo.value?.label ?? selectedProject.value?.label ?? ''
})
const selectedEnvironmentInfo = computed(() => environments.value.find(item => item.key === selectedEnvironment.value) ?? null)
const selectedTargetInfo = computed(() => selectedEnvironmentInfo.value?.targets.find(item => item.key === selectedTarget.value) ?? null)
const canDeploy = computed(() => Boolean(selectedProject.value && selectedRef.value && selectedEnvironment.value && selectedTarget.value))
const deployButtonHint = computed(() => {
  if (canDeploy.value) return '配置已完整，可以触发部署'
  const missing: string[] = []
  if (!selectedProject.value) missing.push('项目')
  if (!selectedRef.value) missing.push(refType.value === 'branch' ? '分支' : '标签')
  if (!selectedEnvironment.value) missing.push('环境')
  if (selectedEnvironment.value && !selectedTarget.value) missing.push('部署服务器')
  return `请先选择${missing.join('、')}`
})

function compareText(left: string, right: string) {
  return left.localeCompare(right, 'zh-CN')
}

function shortCommitId(value: string) {
  return value.length > 8 ? value.slice(0, 8) : value
}

function formatServerAddress(target: DeploymentTarget | null | undefined) {
  if (!target) return ''
  const host = target.server_host || target.server
  return target.server_port ? `${host}:${target.server_port}` : host
}

function toggleProjectCascader() {
  isProjectCascaderOpen.value = !isProjectCascaderOpen.value
  if (isProjectCascaderOpen.value && !activeProject.value) {
    activeProject.value = selectedProject.value ?? projects.value[0] ?? null
  }
}

function handleDocumentClick(event: MouseEvent) {
  if (!(event.target instanceof Node)) return
  if (!projectCascaderRef.value?.contains(event.target)) isProjectCascaderOpen.value = false
}

async function selectProjectLevel(project: Project) {
  activeProject.value = project
  if (project.mode === 'single') await commitUnitSelection(project, '')
}

async function selectComponent(project: Project, component: ProjectComponent) {
  await commitUnitSelection(project, component.key)
}

async function commitUnitSelection(project: Project, component: string) {
  selectedProject.value = project
  selectedComponent.value = component
  selectedRef.value = ''
  selectedEnvironment.value = ''
  selectedTarget.value = ''
  branches.value = []
  tags.value = []
  deployResult.value = null
  isProjectCascaderOpen.value = false
  await Promise.all([loadRefs(), loadDeploymentOptions()])
}

async function loadProjects() {
  loadingProjects.value = true
  try {
    projects.value = (await fetchProjects()).sort((a, b) => compareText(a.label, b.label) || compareText(a.name, b.name))
  } catch (error) {
    errorMessage.value = `获取项目列表失败：${error instanceof Error ? error.message : '未知错误'}`
  } finally {
    loadingProjects.value = false
  }
}

let refsRequestId = 0

async function loadRefs() {
  const project = selectedProject.value
  if (!project) return
  const type = refType.value
  const requestId = ++refsRequestId
  loadingRefs.value = true
  try {
    const refs = (type === 'branch'
      ? await fetchBranches(project.owner, project.name)
      : await fetchTags(project.owner, project.name)).sort((a, b) => compareText(a.name, b.name))
    if (requestId !== refsRequestId) return
    if (type === 'branch') {
      branches.value = refs
    } else {
      tags.value = refs
    }
    selectedRef.value = refs.length === 1 ? refs[0].name : ''
  } catch (error) {
    if (requestId === refsRequestId) errorMessage.value = `获取${type === 'branch' ? '分支' : '标签'}失败：${error instanceof Error ? error.message : '未知错误'}`
  } finally {
    if (requestId === refsRequestId) loadingRefs.value = false
  }
}

async function loadDeploymentOptions() {
  if (!selectedProject.value) return
  try {
    environments.value = await fetchEnvironments(selectedProject.value.name, selectedComponent.value || undefined)
  } catch (error) {
    errorMessage.value = `获取部署环境失败：${error instanceof Error ? error.message : '未知错误'}`
  }
}

function selectEnvironment(environment: Environment) {
  selectedTarget.value = environment.targets.length === 1 ? environment.targets[0].key : ''
}

watch(refType, async () => {
  selectedRef.value = ''
  if (selectedProject.value) await loadRefs()
})

async function handleDeploy() {
  if (!canDeploy.value || !selectedProject.value || !selectedEnvironmentInfo.value || !selectedTargetInfo.value) return
  errorMessage.value = ''
  passwordInput.value = ''
  showPassword.value = false
  confirmationError.value = ''
  showConfirmModal.value = true
  if (selectedEnvironmentInfo.value.need_password) {
    await nextTick()
    passwordInputRef.value?.focus()
  }
}

function cancelDeploymentConfirmation() {
  showConfirmModal.value = false
  passwordInput.value = ''
  confirmationError.value = ''
}

async function confirmDeployment() {
  const needsPassword = Boolean(selectedEnvironmentInfo.value?.need_password)
  if (needsPassword && !passwordInput.value.trim()) {
    confirmationError.value = '请输入部署密码'
    await nextTick()
    passwordInputRef.value?.focus()
    return
  }
  const deployPassword = needsPassword ? passwordInput.value.trim() : undefined
  showConfirmModal.value = false
  confirmationError.value = ''
  await executeDeploy(deployPassword)
  passwordInput.value = ''
}

async function executeDeploy(deployPassword?: string) {
  if (!selectedProject.value) return
  deploying.value = true
  errorMessage.value = ''
  deployResult.value = null
  try {
    deployResult.value = await triggerDeploy({
      project_owner: selectedProject.value.owner,
      project_name: selectedProject.value.name,
      branch: selectedRef.value,
      ref_type: refType.value,
      commit_id: selectedRefInfo.value?.commit_id,
      commit_message: selectedRefInfo.value?.commit_message,
      commit_time: selectedRefInfo.value?.commit_time,
      environment: selectedEnvironment.value,
      component: selectedComponent.value || undefined,
      target: selectedTarget.value,
      deploy_password: deployPassword,
    })
    showDeployStatus.value = true
    emit('deployed')
  } catch (error) {
    errorMessage.value = `部署触发失败：${error instanceof Error ? error.message : '未知错误'}`
  } finally {
    deploying.value = false
  }
}

function onDeployComplete() {
  emit('deployed')
}

onMounted(async () => {
  document.addEventListener('click', handleDocumentClick)
  await loadProjects()
})

onUnmounted(() => document.removeEventListener('click', handleDocumentClick))
</script>

<style scoped>
.deploy-page { padding: 24px 16px; color: #172033; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
.page-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 24px; }
.page-heading h1 { margin: 0; font-size: 1.55rem; }
.page-heading p { margin: 6px 0 0; color: #64748b; }
.alert { margin-bottom: 16px; padding: 11px 14px; border: 1px solid; border-radius: 8px; }
.error-alert { color: #b91c1c; border-color: #fecaca; background: #fef2f2; }
.success-alert { color: #166534; border-color: #bbf7d0; background: #f0fdf4; }
.deploy-form { display: grid; gap: 18px; max-width: 860px; }
.form-step { padding: 18px; border: 1px solid #e2e8f0; border-radius: 10px; background: #fff; }
.form-step.muted { opacity: .65; }
.step-title { display: flex; align-items: center; gap: 9px; margin-bottom: 14px; }
.step-title span { display: grid; width: 24px; height: 24px; place-items: center; border-radius: 50%; color: #fff; background: #2563eb; font-size: .8rem; }
.project-cascader { position: relative; max-width: 720px; }
.cascader-trigger { display: flex; width: 100%; min-height: 50px; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 13px; border: 1px solid #cbd5e1; border-radius: 8px; background: #fff; color: #334155; font-size: 1rem; cursor: pointer; }
.cascader-trigger:disabled { cursor: wait; opacity: .6; }
.selected-project-value { display: flex; min-width: 0; align-items: center; flex-wrap: wrap; gap: 8px; text-align: left; }
.selected-project-value strong { overflow: hidden; font-size: 1.05rem; font-weight: 650; line-height: 1.45; text-overflow: ellipsis; white-space: nowrap; }
.selected-unit-chip { flex: 0 0 auto; padding: 2px 8px; border: 1px solid #bfdbfe; border-radius: 4px; background: #eff6ff; color: #1d4ed8; font-size: .9rem; font-weight: 600; line-height: 1.4; }
.selected-unit-chip.component { border-color: #bbf7d0; background: #f0fdf4; color: #047857; }
.cascader-placeholder { font-size: 1rem; }
.chevron { flex: 0 0 auto; transform: rotate(90deg); font-size: 1.25rem; }
.cascader-panel { position: absolute; z-index: 8; top: calc(100% + 6px); left: 0; display: grid; width: min(760px, calc(100vw - 64px)); grid-template-columns: minmax(240px, 1fr) minmax(280px, 1.2fr); overflow: hidden; border: 1px solid #cbd5e1; border-radius: 10px; background: #fff; box-shadow: 0 14px 30px rgb(15 23 42 / 16%); }
.cascader-column { display: flex; max-height: 340px; flex-direction: column; overflow: auto; padding: 8px; }
.component-column { border-left: 1px solid #e2e8f0; background: #f8fafc; }
.cascader-option { display: flex; min-height: 58px; align-items: center; justify-content: space-between; gap: 12px; padding: 11px 12px; border: 0; border-radius: 7px; background: transparent; text-align: left; cursor: pointer; }
.cascader-option:hover, .cascader-option.active { background: #eff6ff; color: #1d4ed8; }
.cascader-option span:first-child { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.cascader-option strong { font-size: .96rem; line-height: 1.4; }
.cascader-option small { overflow: hidden; color: #64748b; font-size: .84rem; line-height: 1.45; text-overflow: ellipsis; white-space: nowrap; }
.leaf-mark { color: #2563eb; font-size: .84rem; }
.empty-option { padding: 20px; color: #94a3b8; text-align: center; }
.ref-type-toggle { display: flex; gap: 18px; margin-bottom: 12px; }
.field-label { display: block; margin-bottom: 6px; color: #475569; font-size: .88rem; font-weight: 600; }
select, .deployment-confirm-modal input[type="password"], .deployment-confirm-modal input[type="text"] { box-sizing: border-box; width: 100%; min-height: 42px; padding: 9px 11px; border: 1px solid #cbd5e1; border-radius: 7px; background: #fff; }
#ref-select { min-height: 50px; font-size: 1rem; font-weight: 500; }
#ref-select option { font-size: 1rem; }
.commit-card { display: flex; margin-top: 10px; flex-direction: column; gap: 3px; padding: 10px 12px; border-radius: 7px; background: #f8fafc; color: #334155; }
.commit-meta { color: #64748b; font-size: .82rem; }
.choice-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; }
.choice-card { display: grid; grid-template-columns: auto 1fr; column-gap: 9px; padding: 12px; border: 1px solid #cbd5e1; border-radius: 8px; cursor: pointer; }
.choice-card input { grid-row: 1 / span 2; align-self: center; }
.choice-card strong, .choice-card small { grid-column: 2; }
.choice-card small { margin-top: 3px; color: #64748b; }
.choice-card.selected { border-color: #2563eb; background: #eff6ff; box-shadow: 0 0 0 1px #2563eb; }
.choice-card.disabled { cursor: not-allowed; opacity: .55; background: #f8fafc; }
.target-grid { grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); }
.auto-target { display: flex; flex-direction: column; gap: 4px; padding: 14px; border: 1px solid #86efac; border-radius: 8px; background: #f0fdf4; }
.auto-target small { color: #64748b; }
.auto-badge { align-self: flex-start; padding: 2px 7px; border-radius: 10px; color: #166534; background: #dcfce7; font-size: .75rem; }
.deploy-actions { display: flex; align-items: center; gap: 12px; color: #64748b; font-size: .88rem; }
.deploy-btn, .secondary-btn { padding: 10px 18px; border-radius: 7px; font-weight: 600; cursor: pointer; }
.deploy-btn { border: 1px solid #2563eb; color: #fff; background: #2563eb; }
.deploy-btn:disabled { cursor: not-allowed; opacity: .55; }
.secondary-btn { border: 1px solid #cbd5e1; color: #334155; background: #fff; }
.modal-backdrop { position: fixed; z-index: 20; inset: 0; display: grid; place-items: center; padding: 16px; background: rgb(15 23 42 / 50%); }
.deployment-confirm-modal { display: flex; width: min(100%, 500px); flex-direction: column; gap: 14px; padding: 22px; border-radius: 10px; background: #fff; box-shadow: 0 24px 60px rgb(15 23 42 / 28%); }
.deployment-confirm-modal h2, .deployment-confirm-modal p { margin: 0; }
.deployment-confirm-modal p { color: #64748b; }
.deployment-confirm-modal label:not(.show-password) { display: flex; flex-direction: column; gap: 6px; font-weight: 600; }
.deployment-summary { overflow: hidden; border: 1px solid #e2e8f0; border-radius: 8px; background: #f8fafc; }
.deployment-summary div { display: grid; grid-template-columns: 88px minmax(0, 1fr); gap: 12px; padding: 9px 12px; border-bottom: 1px solid #e2e8f0; }
.deployment-summary div:last-child { border-bottom: 0; }
.deployment-summary span { color: #64748b; }
.deployment-summary strong { overflow-wrap: anywhere; }
.show-password { display: flex; align-items: center; gap: 7px; }
.confirmation-error { padding: 9px 11px; border: 1px solid #fecaca; border-radius: 7px; color: #b91c1c; background: #fef2f2; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; }
@media (max-width: 680px) {
  .cascader-panel { grid-template-columns: 1fr; }
  .component-column { border-top: 1px solid #e2e8f0; border-left: 0; }
  .deploy-actions { align-items: flex-start; flex-direction: column; }
}
</style>
