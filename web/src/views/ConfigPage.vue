<template>
  <div class="config-page">
    <div v-if="errorMessage" class="alert error-alert" role="alert">{{ errorMessage }}</div>
    <div v-if="successMessage" class="alert success-alert" role="status">{{ successMessage }}</div>
    <div v-if="loading" class="empty-state">加载配置中...</div>

    <template v-else>
      <section class="config-section">
        <div>
          <h2>环境服务器</h2>
          <p>通常无需修改；这里只决定每个环境可以选择哪些服务器。</p>
        </div>
        <div class="environment-actions">
          <button
            v-for="environment in standardEnvironments"
            :key="environment.key"
            class="environment-btn"
            :disabled="!environments[environment.key]"
            @click="openEnvironmentDialog(environment.key)"
          >
            {{ environment.label }}
            <span>{{ environments[environment.key]?.servers.length ?? 0 }} 台服务器</span>
          </button>
        </div>
      </section>

      <section class="config-section">
        <div class="section-heading">
          <div>
            <h2>项目配置</h2>
            <p>新增前端子项目时只需填写 apps 目录名称，其余配置自动生成。</p>
          </div>
        </div>

        <div class="project-primary-nav">
          <div class="project-list" role="tablist" aria-label="服务">
            <button
              v-for="project in projectEntries"
              :key="project.key"
              :class="['project-item', { active: selectedProjectKey === project.key }]"
              role="tab"
              :aria-selected="selectedProjectKey === project.key"
              @click="selectProject(project.key)"
            >
              <strong>{{ project.key }}</strong>
              <span>
                {{ project.value.label || project.key }} ·
                {{ project.value.mode === 'monorepo' ? '包含多个子项目' : '单体项目' }}
              </span>
            </button>
          </div>
          <div class="tab-actions">
            <button class="add-project-tab" @click="openCreateDialog('project')">
              <span aria-hidden="true">＋</span>
              新增项目
            </button>
            <button v-if="selectedProject" class="remove-btn tab-delete-btn" @click="removeProject">
              删除当前项目
            </button>
          </div>
        </div>

        <div class="project-content">
          <div v-if="selectedProject" class="project-editor">
            <div v-if="selectedProject.mode === 'monorepo'" class="component-panel">
              <div class="component-tab-row">
                <div class="component-list" role="tablist" aria-label="子项目">
                  <button
                    v-for="component in componentEntries"
                    :key="component.key"
                    :class="['component-item', { active: selectedComponentKey === component.key }]"
                    role="tab"
                    :aria-selected="selectedComponentKey === component.key"
                    @click="selectComponent(component.key)"
                  >
                    {{ component.value.label || component.key }}
                  </button>
                </div>
                <div class="tab-actions">
                  <button class="add-component-tab" @click="openCreateDialog('component')">
                    <span aria-hidden="true">＋</span>
                    新增子项目
                  </button>
                  <button v-if="selectedComponent" class="remove-btn tab-delete-btn" @click="removeComponent">
                    删除当前子项目
                  </button>
                </div>
              </div>
              <div v-if="selectedComponent" class="component-basic">
                <label>
                  标签名称
                  <input v-model.trim="selectedComponent.label" />
                </label>
                <div class="auto-value">
                  <span>代码目录</span>
                  <strong>{{ selectedComponent.path }}</strong>
                  <small>已自动关联</small>
                </div>
              </div>
            </div>

            <div v-if="activeUnitEnvironments" class="card environment-card">
              <div class="environment-tab-row">
                <div
                  class="environment-tabs"
                  :class="{ tertiary: selectedProject.mode === 'monorepo' }"
                  role="tablist"
                  aria-label="环境"
                >
                  <button
                    v-for="environment in standardEnvironments"
                    :key="environment.key"
                    :class="{ active: selectedEnvironmentKey === environment.key }"
                    role="tab"
                    :aria-selected="selectedEnvironmentKey === environment.key"
                    @click="selectEnvironmentRule(environment.key)"
                  >
                    {{ environment.shortLabel }}
                  </button>
                </div>
                <button
                  v-if="currentEnvironmentRule"
                  class="remove-btn clear-config-btn"
                  @click="removeCurrentEnvironment"
                >
                  清空配置
                </button>
              </div>

              <div v-if="!currentEnvironmentRule" class="rule-empty">
                <strong>{{ currentEnvironmentLabel }}尚未配置</strong>
                <p>点击后将自动填写构建命令、上传文件和部署服务器。</p>
                <button class="save-btn" @click="enableCurrentEnvironment">使用默认配置</button>
              </div>

              <template v-else>
                <div class="basic-fields">
                  <label>
                    构建命令
                    <textarea v-model="currentEnvironmentRule.build.command" rows="2" />
                  </label>

                  <div class="upload-list">
                    <div
                      v-for="(_, index) in currentEnvironmentRule.build.artifacts"
                      :key="index"
                      class="upload-row"
                    >
                      <label>
                        打包后待上传服务器的文件名
                        <input v-model.trim="currentEnvironmentRule.build.artifacts[index].upload_as" placeholder="app.tar.gz" />
                      </label>
                    </div>
                  </div>
                </div>

                <div class="section-heading target-heading">
                  <div>
                    <strong>部署服务器</strong>
                    <p>系统已选择本环境的默认服务器，可修改或继续添加。</p>
                  </div>
                  <button
                    type="button"
                    class="add-server-btn"
                    @click="openAddServerDialog"
                  >
                    <span aria-hidden="true">＋</span>
                    添加部署服务器
                  </button>
                </div>

                <div v-if="deploymentEntries.length === 0" class="rule-empty compact-empty">
                  <p>当前没有部署服务器，请从上方选择一台服务器。</p>
                </div>
                <div class="target-list">
                  <div v-for="(deployment, index) in deploymentEntries" :key="deployment.key" class="deployment-card">
                    <div class="target-card-main">
                      <strong class="server-order">{{ serverOrdinal(index) }}、服务器</strong>
                      <select v-model="deployment.value.server" :aria-label="`${serverOrdinal(index)}、服务器`">
                        <option v-for="server in currentEnvironmentServers" :key="server.key" :value="server.key">
                          {{ serverDisplayName(server) }}
                        </option>
                      </select>
                      <button class="text-danger" @click="removeDeploymentTarget(deployment.key)">移除</button>
                    </div>
                    <details class="inline-advanced">
                      <summary>修改构建命令和部署脚本</summary>
                      <div class="advanced-fields">
                        <label>服务器标识<input v-model.trim="deployment.value.label" /></label>
                        <label>
                          服务器专用构建命令
                          <small>留空使用上方公共构建命令；仅在该服务器需要额外环境变量或参数时填写。</small>
                          <textarea
                            :value="deployment.value.build_command ?? deployment.value.build_override?.command ?? ''"
                            :placeholder="currentEnvironmentRule.build.command"
                            rows="3"
                            @input="updateDeploymentBuildCommand(deployment.value, $event)"
                          />
                        </label>
                        <label>部署脚本<textarea v-model="deployment.value.script" rows="7" /></label>
                      </div>
                    </details>
                  </div>
                </div>

                <details class="advanced-panel">
                  <summary>高级配置</summary>
                  <p>仅在默认路径不适用时修改。</p>

                  <div v-if="selectedProject.mode === 'monorepo' && selectedComponent" class="form-grid">
                    <label>代码路径<input v-model.trim="selectedComponent.path" /></label>
                    <label>
                      构建类型
                      <select v-model="selectedComponent.build_type">
                        <option value="">自动识别</option>
                        <option value="frontend">前端</option>
                        <option value="backend">后端</option>
                      </select>
                    </label>
                  </div>

                  <div class="advanced-artifacts">
                    <div class="section-heading">
                      <strong>构建产物</strong>
                      <button class="secondary-btn" @click="addArtifact">新增构建产物</button>
                    </div>
                    <p>仅当一次构建需要上传多个目录或文件时使用。</p>
                    <div
                      v-for="(_, index) in currentEnvironmentRule.build.artifacts"
                      :key="index"
                      class="artifact-row"
                    >
                      <label>产物标识<input v-model.trim="currentEnvironmentRule.build.artifacts[index].key" /></label>
                      <label>输出路径<input v-model.trim="currentEnvironmentRule.build.artifacts[index].output" /></label>
                      <button
                        v-if="currentEnvironmentRule.build.artifacts.length > 1"
                        class="text-danger"
                        @click="removeArtifact(index)"
                      >
                        移除
                      </button>
                    </div>
                  </div>
                </details>
              </template>
            </div>

            <details class="advanced-panel project-advanced">
              <summary>项目高级设置</summary>
              <p>项目标识和类型通常不需要修改。</p>
              <div class="form-grid">
                <label>项目标识<input v-model.trim="projectKeyInput" @change="renameProject" /></label>
                <label>项目名称<input v-model.trim="selectedProject.label" /></label>
                <label>
                  项目类型
                  <select v-model="selectedProject.mode" @change="handleModeChange">
                    <option value="single">单体项目</option>
                    <option value="monorepo">多子项目</option>
                  </select>
                </label>
                <label v-if="selectedProject.mode === 'single'">
                  构建类型
                  <select v-model="selectedProject.build_type">
                    <option value="">自动识别</option>
                    <option value="frontend">前端</option>
                    <option value="backend">后端</option>
                  </select>
                </label>
              </div>
            </details>

            <div v-if="!activeUnitEnvironments" class="empty-state">请先新增或选择一个子项目。</div>
          </div>
          <div v-else class="empty-state">请选择一个项目。</div>
        </div>
        <div class="config-save-actions">
          <button class="save-btn" :disabled="savingProjects" @click="saveProjects">
            {{ savingProjects ? '保存中...' : '保存配置' }}
          </button>
        </div>
      </section>
    </template>

    <div v-if="addServerDialog" class="dialog-backdrop" @click.self="closeAddServerDialog">
      <form class="create-dialog" @submit.prevent="addDeploymentTarget">
        <div class="section-heading">
          <h2>添加部署服务器</h2>
          <button type="button" class="close-btn" @click="closeAddServerDialog">&times;</button>
        </div>
        <p>选择一台当前环境中尚未添加的服务器。</p>
        <label v-if="availableTargetServers.length > 0">
          服务器
          <select v-model="newTargetServer">
            <option value="" disabled>请选择服务器</option>
            <option v-for="server in availableTargetServers" :key="server.key" :value="server.key">
              {{ serverDisplayName(server) }}
            </option>
          </select>
        </label>
        <div v-else class="rule-empty compact-empty">
          当前环境没有更多可添加的服务器。
        </div>
        <div class="dialog-actions">
          <button type="button" class="secondary-btn" @click="closeAddServerDialog">取消</button>
          <button class="save-btn" :disabled="!newTargetServer">确认添加</button>
        </div>
      </form>
    </div>

    <div v-if="selectedEnvironment" class="dialog-backdrop" @click.self="closeEnvironmentDialog">
      <form class="environment-dialog" @submit.prevent="saveEnvironment">
        <div class="section-heading">
          <h2>{{ environmentTitle }}</h2>
          <button type="button" class="close-btn" @click="closeEnvironmentDialog">&times;</button>
        </div>
        <p>勾选这个环境可以使用的服务器。项目的默认部署目标会从这里生成。</p>
        <fieldset>
          <legend>可用服务器</legend>
          <label v-for="server in serverOptions" :key="server.key" class="server-check">
            <input v-model="environmentDraft.servers" type="checkbox" :value="server.key" />
            <span>
              <strong>{{ server.label || server.key }}</strong>
              <small>{{ server.host }}:{{ server.port }} · {{ server.deploy_path }}</small>
            </span>
          </label>
        </fieldset>
        <details class="inline-advanced">
          <summary>环境访问地址</summary>
          <div class="advanced-fields">
            <label>用户端地址<input v-model.trim="environmentDraft.links.user_url" type="url" placeholder="http://" /></label>
            <label>管理端地址<input v-model.trim="environmentDraft.links.admin_url" type="url" placeholder="http://" /></label>
            <div>
              <div class="section-heading">
                <strong>其他地址</strong>
                <button type="button" class="secondary-btn" @click="addExtraLink">新增地址</button>
              </div>
              <div v-for="(_, index) in environmentDraft.links.extra" :key="index" class="extra-row">
                <input v-model.trim="environmentDraft.links.extra[index].label" placeholder="标签" />
                <input v-model.trim="environmentDraft.links.extra[index].url" placeholder="URL" />
                <button type="button" class="text-danger" @click="removeExtraLink(index)">删除</button>
              </div>
            </div>
          </div>
        </details>
        <div class="dialog-actions">
          <button type="button" class="secondary-btn" @click="closeEnvironmentDialog">取消</button>
          <button class="save-btn" :disabled="savingEnvironment">{{ savingEnvironment ? '保存中...' : '确认保存' }}</button>
        </div>
      </form>
    </div>

    <div v-if="createDialog" class="dialog-backdrop" @click.self="closeCreateDialog">
      <form class="create-dialog" @submit.prevent="createEntity">
        <div class="section-heading">
          <h2>{{ createDialog === 'project' ? '新增一级项目' : '新增子项目' }}</h2>
          <button type="button" class="close-btn" @click="closeCreateDialog">&times;</button>
        </div>
        <label>
          {{ createDialog === 'project' ? '项目名称（仓库名称）' : '子项目名称（apps 目录名）' }}
          <input
            v-model.trim="newEntityName"
            :placeholder="createDialog === 'project' ? '例如 cde-service' : '例如 cde-example-ui'"
            autofocus
          />
        </label>
        <label v-if="createDialog === 'component'">
          标签名称
          <input
            v-model.trim="newEntityLabel"
            placeholder="例如 数据要素X官网"
          />
        </label>
        <div v-if="newEntityKeyPreview" class="create-preview">
          <span>将自动创建</span>
          <strong>{{ createDialog === 'project' ? newEntityKeyPreview : `apps/${newEntityKeyPreview}` }}</strong>
          <small>
            {{ createDialog === 'project'
              ? '创建后可按环境一键生成默认配置'
              : '开发、测试、生产配置及默认服务器会一并生成' }}
          </small>
        </div>
        <div class="dialog-actions">
          <button type="button" class="secondary-btn" @click="closeCreateDialog">取消</button>
          <button
            class="save-btn"
            :disabled="!newEntityKeyPreview || (createDialog === 'component' && !newEntityLabel)"
          >
            确认新增
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  fetchEditableConfig,
  updateEnvironmentAccess,
  updateProjectBuildConfig,
  type ArtifactConfig,
  type ComponentBuildConfig,
  type DeploymentConfig,
  type EditableConfig,
  type EditableEnvironment,
  type EnvironmentAccessConfig,
  type ProjectBuildConfig,
  type ProjectBuildConfigItem,
  type ServerSummary,
  type UnitEnvironmentConfig,
} from '../api/index'

const props = defineProps<{ adminToken: string }>()
const standardEnvironments = [
  { key: 'dev', label: '开发环境配置', shortLabel: '开发' },
  { key: 'sit', label: '测试环境配置', shortLabel: '测试' },
  { key: 'prod', label: '生产环境配置', shortLabel: '生产' },
]

const environments = ref<EditableConfig['environments']>({})
const projects = ref<ProjectBuildConfig>({})
const serverOptions = ref<ServerSummary[]>([])
const selectedProjectKey = ref('')
const selectedComponentKey = ref('')
const selectedEnvironmentKey = ref('dev')
const projectKeyInput = ref('')
const newTargetServer = ref('')
const addServerDialog = ref(false)
const createDialog = ref<'project' | 'component' | null>(null)
const newEntityName = ref('')
const newEntityLabel = ref('')
const selectedEnvironment = ref<string | null>(null)
const loading = ref(true)
const savingEnvironment = ref(false)
const savingProjects = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const environmentDraft = reactive<EnvironmentAccessConfig>({
  disabled: false,
  servers: [],
  links: { user_url: '', admin_url: '', extra: [] },
})

const projectEntries = computed(() => Object.entries(projects.value).map(([key, value]) => ({ key, value })))
const selectedProject = computed<ProjectBuildConfigItem | null>(() => projects.value[selectedProjectKey.value] ?? null)
const componentEntries = computed(() => Object.entries(selectedProject.value?.components ?? {}).map(([key, value]) => ({ key, value })))
const selectedComponent = computed<ComponentBuildConfig | null>(() => selectedProject.value?.components[selectedComponentKey.value] ?? null)
const newEntityKeyPreview = computed(() => normalizeEntityKey(newEntityName.value))
const currentUnitKey = computed(() => selectedProject.value?.mode === 'monorepo' ? selectedComponentKey.value : selectedProjectKey.value)
const currentUnitLabel = computed(() => {
  if (selectedProject.value?.mode === 'monorepo') return selectedComponent.value?.label || selectedComponentKey.value
  return selectedProject.value?.label || selectedProjectKey.value
})
const activeUnitEnvironments = computed<Record<string, UnitEnvironmentConfig> | null>(() => {
  const project = selectedProject.value
  if (!project) return null
  return project.mode === 'single' ? project.environments : selectedComponent.value?.environments ?? null
})
const currentEnvironmentRule = computed<UnitEnvironmentConfig | null>(() => activeUnitEnvironments.value?.[selectedEnvironmentKey.value] ?? null)
const currentEnvironmentLabel = computed(() => standardEnvironments.find(item => item.key === selectedEnvironmentKey.value)?.shortLabel ?? selectedEnvironmentKey.value)
const environmentTitle = computed(() => standardEnvironments.find(item => item.key === selectedEnvironment.value)?.label ?? '环境配置')
const currentEnvironmentServers = computed<ServerSummary[]>(() => {
  const keys = environments.value[selectedEnvironmentKey.value]?.servers ?? []
  return keys.map(key => serverOptions.value.find(server => server.key === key) ?? {
    key,
    label: key,
    host: '',
    port: 0,
    deploy_path: '',
  })
})
const deploymentEntries = computed(() => Object.entries(currentEnvironmentRule.value?.deployments ?? {}).map(([key, value]) => ({ key, value })))
const availableTargetServers = computed(() => {
  const selectedKeys = new Set(deploymentEntries.value.map(({ value }) => value.server))
  return currentEnvironmentServers.value.filter(server => !selectedKeys.has(server.key))
})

function serverDisplayName(server: ServerSummary): string {
  const name = server.label || server.key
  return server.host && server.port ? `${name}（${server.host}:${server.port}）` : name
}

function serverOrdinal(index: number): string {
  const ordinals = ['一', '二', '三', '四', '五', '六', '七', '八', '九', '十']
  return ordinals[index] ?? String(index + 1)
}

function cloneConfig<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function normalizeEntityKey(value: string): string {
  const lastPathSegment = value.trim().replace(/\\/g, '/').split('/').filter(Boolean).pop() ?? ''
  return lastPathSegment
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9_.-]/g, '')
    .replace(/-{2,}/g, '-')
    .replace(/^[.-]+|[.-]+$/g, '')
}

function artifactStem(key: string): string {
  return key.replace(/^cde-/, '') || key || 'app'
}

function adaptTemplateText(value: string, sourceKey: string, targetKey: string): string {
  if (!sourceKey || sourceKey === targetKey) return value
  let adapted = value.split(sourceKey).join(targetKey)
  const sourceStem = artifactStem(sourceKey)
  const targetStem = artifactStem(targetKey)
  if (sourceStem !== sourceKey) adapted = adapted.split(sourceStem).join(targetStem)
  return adapted
}

function componentEnvironmentTemplates(environmentKey: string) {
  const templates: Array<{ unitKey: string; rule: UnitEnvironmentConfig }> = []
  const preferredProject = selectedProject.value
  for (const [unitKey, component] of Object.entries(preferredProject?.components ?? {})) {
    const rule = component.environments[environmentKey]
    if (rule) templates.push({ unitKey, rule })
  }
  for (const project of Object.values(projects.value)) {
    if (project === preferredProject) continue
    for (const [unitKey, component] of Object.entries(project.components ?? {})) {
      const rule = component.environments[environmentKey]
      if (rule) templates.push({ unitKey, rule })
    }
  }
  return templates
}

function singleEnvironmentTemplates(environmentKey: string) {
  const templates: Array<{ unitKey: string; rule: UnitEnvironmentConfig }> = []
  for (const [unitKey, project] of Object.entries(projects.value)) {
    if (project.mode !== 'single') continue
    const rule = project.environments[environmentKey]
    if (rule) templates.push({ unitKey, rule })
  }
  return templates
}

function environmentTemplates(environmentKey: string, frontend: boolean) {
  return frontend
    ? componentEnvironmentTemplates(environmentKey)
    : singleEnvironmentTemplates(environmentKey)
}

function defaultBuild(environmentKey: string, unitKey: string, frontend: boolean): UnitEnvironmentConfig['build'] {
  const template = environmentTemplates(environmentKey, frontend)[0]
  if (template) {
    const build = cloneConfig(template.rule.build)
    build.command = adaptTemplateText(build.command, template.unitKey, unitKey)
    build.artifacts = build.artifacts.map(artifact => ({
      ...artifact,
      output: adaptTemplateText(artifact.output, template.unitKey, unitKey),
      upload_as: adaptTemplateText(artifact.upload_as, template.unitKey, unitKey),
    }))
    return build
  }

  if (frontend) {
    return {
      command: `cd apps/${unitKey} && pnpm install && pnpm build:${environmentKey}`,
      artifacts: [{
        key: 'app',
        output: `./apps/${unitKey}/dist`,
        upload_as: `${artifactStem(unitKey)}.tar.gz`,
      }],
    }
  }

  return {
    command: `mvn clean package -DskipTests -P${environmentKey} -Dmaven.test.skip=true`,
    artifacts: [{
      key: 'app',
      output: `./target/${unitKey}.jar`,
      upload_as: `${unitKey}.jar`,
    }],
  }
}

function deploymentTemplate(environmentKey: string, serverKey: string, frontend: boolean) {
  for (const template of environmentTemplates(environmentKey, frontend)) {
    const deployment = Object.values(template.rule.deployments).find(item => item.server === serverKey)
    if (deployment) return { unitKey: template.unitKey, deployment }
  }
  return null
}

function defaultDeployment(
  environmentKey: string,
  serverKey: string,
  unitKey: string,
  unitLabel: string,
  frontend: boolean,
): DeploymentConfig {
  const template = deploymentTemplate(environmentKey, serverKey, frontend)
  const buildCommand = template?.deployment.build_command
    ? adaptTemplateText(template.deployment.build_command, template.unitKey, unitKey)
    : ''
  return {
    label: unitLabel || unitKey,
    server: serverKey,
    script: template
      ? adaptTemplateText(template.deployment.script, template.unitKey, unitKey)
      : '',
    disabled: null,
    ...(buildCommand ? { build_command: buildCommand } : {}),
  }
}

function updateDeploymentBuildCommand(deployment: DeploymentConfig, event: Event) {
  const command = (event.target as HTMLTextAreaElement).value
  // Editing this simple field intentionally converts any legacy full build
  // override into a command-only override with shared artifacts.
  delete deployment.build_override
  if (!command.trim()) {
    delete deployment.build_command
    return
  }
  deployment.build_command = command
}

function defaultEnvironmentRule(
  environmentKey: string,
  unitKey: string,
  unitLabel: string,
  frontend: boolean,
): UnitEnvironmentConfig {
  const deployments: Record<string, DeploymentConfig> = {}
  const defaultServer = environments.value[environmentKey]?.servers?.[0]
  for (const serverKey of defaultServer ? [defaultServer] : []) {
    deployments[serverKey] = defaultDeployment(environmentKey, serverKey, unitKey, unitLabel, frontend)
  }
  return {
    disabled: null,
    build: defaultBuild(environmentKey, unitKey, frontend),
    deployments,
  }
}

function defaultArtifact(): ArtifactConfig {
  const frontend = selectedProject.value?.mode === 'monorepo'
  const key = currentUnitKey.value || 'app'
  const index = (currentEnvironmentRule.value?.build.artifacts.length ?? 0) + 1
  return {
    key: index === 1 ? 'app' : `app-${index}`,
    output: frontend
      ? `./${selectedComponent.value?.path || `apps/${key}`}/dist`
      : `./target/${key}.jar`,
    upload_as: `${artifactStem(key)}${index === 1 ? '' : `-${index}`}${frontend ? '.tar.gz' : '.jar'}`,
  }
}

function selectProject(key: string) {
  selectedProjectKey.value = key
  projectKeyInput.value = key
  selectedComponentKey.value = Object.keys(projects.value[key].components ?? {})[0] ?? ''
  newTargetServer.value = ''
}

function selectComponent(key: string) {
  selectedComponentKey.value = key
  newTargetServer.value = ''
}

function selectEnvironmentRule(key: string) {
  selectedEnvironmentKey.value = key
  newTargetServer.value = ''
}

function handleModeChange() {
  const project = selectedProject.value
  if (!project) return
  project.environments ??= {}
  project.components ??= {}
  selectedComponentKey.value = project.mode === 'monorepo' ? Object.keys(project.components)[0] ?? '' : ''
}

function openCreateDialog(type: 'project' | 'component') {
  createDialog.value = type
  newEntityName.value = ''
  newEntityLabel.value = ''
  errorMessage.value = ''
}

function closeCreateDialog() {
  createDialog.value = null
  newEntityLabel.value = ''
}

function createEntity() {
  if (createDialog.value === 'project') addProject()
  else addComponent()
}

function addProject() {
  const key = newEntityKeyPreview.value
  if (!key) return
  if (projects.value[key]) {
    errorMessage.value = '项目标识已存在'
    return
  }
  projects.value[key] = {
    label: newEntityName.value || key,
    mode: 'single',
    build_type: 'backend',
    environments: {},
    components: {},
  }
  selectProject(key)
  closeCreateDialog()
}

function removeProject() {
  const project = selectedProject.value
  if (!project || !confirm(`是否删除“${project.label || selectedProjectKey.value}（${selectedProjectKey.value}）”？`)) return
  delete projects.value[selectedProjectKey.value]
  const first = Object.keys(projects.value)[0] ?? ''
  if (first) selectProject(first)
  else {
    selectedProjectKey.value = ''
    selectedComponentKey.value = ''
  }
}

function renameProject() {
  const oldKey = selectedProjectKey.value
  const newKey = projectKeyInput.value
  if (!oldKey || newKey === oldKey) return
  if (!newKey || projects.value[newKey]) {
    errorMessage.value = newKey ? '项目标识已存在' : '项目标识不能为空'
    projectKeyInput.value = oldKey
    return
  }
  projects.value[newKey] = projects.value[oldKey]
  delete projects.value[oldKey]
  selectedProjectKey.value = newKey
}

function addComponent() {
  const project = selectedProject.value
  const key = newEntityKeyPreview.value
  if (!project || !key) return
  if (project.components[key]) {
    errorMessage.value = '这个子项目已经存在'
    return
  }
  const label = newEntityLabel.value || newEntityName.value || key
  const componentEnvironments: Record<string, UnitEnvironmentConfig> = {}
  for (const environment of standardEnvironments) {
    if (!environments.value[environment.key]) continue
    componentEnvironments[environment.key] = defaultEnvironmentRule(environment.key, key, label, true)
  }
  project.components[key] = {
    label,
    path: `apps/${key}`,
    build_type: 'frontend',
    environments: componentEnvironments,
  }
  selectedComponentKey.value = key
  closeCreateDialog()
}

function removeComponent() {
  const project = selectedProject.value
  const component = selectedComponent.value
  if (
    !project
    || !component
    || !confirm(
      `是否删除“${component.label || selectedComponentKey.value}（${component.path || `apps/${selectedComponentKey.value}`}）”？`,
    )
  ) return
  delete project.components[selectedComponentKey.value]
  selectedComponentKey.value = Object.keys(project.components)[0] ?? ''
}

function enableCurrentEnvironment() {
  if (!activeUnitEnvironments.value) return
  activeUnitEnvironments.value[selectedEnvironmentKey.value] = defaultEnvironmentRule(
    selectedEnvironmentKey.value,
    currentUnitKey.value,
    currentUnitLabel.value,
    selectedProject.value?.mode === 'monorepo',
  )
}

function removeCurrentEnvironment() {
  if (!activeUnitEnvironments.value || !confirm(`是否清空${currentEnvironmentLabel.value}所有配置？`)) return
  delete activeUnitEnvironments.value[selectedEnvironmentKey.value]
}

function addArtifact() {
  currentEnvironmentRule.value?.build.artifacts.push(defaultArtifact())
}

function removeArtifact(index: number) {
  currentEnvironmentRule.value?.build.artifacts.splice(index, 1)
}

function addDeploymentTarget() {
  const rule = currentEnvironmentRule.value
  const serverKey = newTargetServer.value
  if (!rule || !serverKey) return
  let targetKey = serverKey
  let suffix = 2
  while (rule.deployments[targetKey]) targetKey = `${serverKey}-${suffix++}`
  const server = serverOptions.value.find(item => item.key === serverKey)
  rule.deployments[targetKey] = defaultDeployment(
    selectedEnvironmentKey.value,
    serverKey,
    currentUnitKey.value,
    currentUnitLabel.value || server?.label || serverKey,
    selectedProject.value?.mode === 'monorepo',
  )
  newTargetServer.value = ''
  addServerDialog.value = false
}

function openAddServerDialog() {
  newTargetServer.value = ''
  addServerDialog.value = true
}

function closeAddServerDialog() {
  newTargetServer.value = ''
  addServerDialog.value = false
}

function removeDeploymentTarget(key: string) {
  if (!currentEnvironmentRule.value || !confirm('是否删除这个部署目标？')) return
  delete currentEnvironmentRule.value.deployments[key]
}

function cloneAccess(environment: EditableEnvironment): EnvironmentAccessConfig {
  return {
    disabled: environment.disabled,
    servers: [...(environment.servers ?? [])],
    links: {
      user_url: environment.links?.user_url ?? '',
      admin_url: environment.links?.admin_url ?? '',
      extra: (environment.links?.extra ?? []).map(item => ({ ...item })),
    },
  }
}

function openEnvironmentDialog(key: string) {
  const environment = environments.value[key]
  if (!environment) return
  Object.assign(environmentDraft, cloneAccess(environment))
  selectedEnvironment.value = key
}

function closeEnvironmentDialog() {
  if (!savingEnvironment.value) selectedEnvironment.value = null
}

function addExtraLink() {
  environmentDraft.links.extra.push({ label: '', url: '' })
}

function removeExtraLink(index: number) {
  environmentDraft.links.extra.splice(index, 1)
}

async function loadConfig() {
  loading.value = true
  try {
    const loaded = await fetchEditableConfig(props.adminToken)
    environments.value = loaded.environments
    projects.value = loaded.projects
    serverOptions.value = loaded.servers
    const first = Object.keys(projects.value)[0]
    if (first) selectProject(first)
  } catch (error: unknown) {
    errorMessage.value = `获取配置失败：${error instanceof Error ? error.message : '未知错误'}`
  } finally {
    loading.value = false
  }
}

async function saveEnvironment() {
  if (!selectedEnvironment.value || !confirm(`是否保存${environmentTitle.value}？`)) return
  savingEnvironment.value = true
  try {
    const saved = await updateEnvironmentAccess(props.adminToken, selectedEnvironment.value, cloneAccess({
      label: '',
      ...environmentDraft,
    }))
    environments.value[selectedEnvironment.value] = {
      ...environments.value[selectedEnvironment.value],
      ...saved,
    }
    successMessage.value = `${environmentTitle.value}已保存`
    selectedEnvironment.value = null
  } catch (error: unknown) {
    errorMessage.value = `保存环境配置失败：${error instanceof Error ? error.message : '未知错误'}`
  } finally {
    savingEnvironment.value = false
  }
}

async function saveProjects() {
  if (!confirm('是否保存全部构建与部署目标配置？')) return
  savingProjects.value = true
  try {
    projects.value = await updateProjectBuildConfig(props.adminToken, projects.value)
    successMessage.value = '构建与部署目标配置已保存并生效'
  } catch (error: unknown) {
    errorMessage.value = `保存配置失败：${error instanceof Error ? error.message : '未知错误'}`
  } finally {
    savingProjects.value = false
  }
}

onMounted(loadConfig)
</script>

<style scoped>
.config-page { padding:24px; color:#172033; }
h1,h2,h3,p { margin:0; }
h1 { font-size:1.55rem; }
h2 { font-size:1.15rem; }
h3 { font-size:1rem; }
p { margin-top:5px; color:#687386; font-size:.9rem; line-height:1.5; }
button,input,select,textarea { font:inherit; }
button { cursor:pointer; }
button:disabled { opacity:.5; cursor:not-allowed; }
.page-heading { margin-bottom:20px; }
.config-section { padding:20px 0 26px; border-top:1px solid #e3e8f0; }
.section-heading { display:flex; align-items:center; justify-content:space-between; gap:16px; }
.compact-heading { margin-top:18px; }
.environment-actions { display:flex; flex-wrap:wrap; gap:10px; margin-top:16px; }
.environment-btn { display:flex; min-width:160px; flex-direction:column; gap:3px; padding:11px 15px; border:1px solid #bfdbfe; border-radius:9px; background:#eff6ff; color:#1d4ed8; font-weight:600; text-align:left; }
.environment-btn span,.project-item span { color:#64748b; font-size:.78rem; font-weight:400; }
.project-primary-nav { display:flex; align-items:flex-end; justify-content:space-between; gap:20px; margin-top:18px; border-bottom:1px solid #dbe2ec; }
.project-list { display:flex; min-width:0; align-items:flex-end; gap:26px; overflow-x:auto; }
.tab-actions { display:flex; flex:0 0 auto; align-items:center; gap:8px; }
.project-item { display:flex; min-width:max-content; flex-direction:column; gap:3px; padding:11px 4px 10px; border:0; border-bottom:3px solid transparent; background:transparent; color:#475569; text-align:left; }
.project-item strong { font-size:1rem; }
.project-item.active { border-bottom-color:#2563eb; color:#1d4ed8; }
.component-item.active { border-bottom-color:#2563eb; color:#1d4ed8; }
.add-project-tab { display:flex; align-items:center; gap:6px; flex:0 0 auto; margin-bottom:10px; padding:7px 10px; border:1px dashed #93b4ee; border-radius:7px; background:#f8fbff; color:#2563eb; font-size:.86rem; font-weight:600; }
.add-project-tab span { font-size:1.05rem; line-height:1; }
.project-content { margin-top:18px; }
.config-save-actions { display:flex; justify-content:flex-end; margin-top:20px; padding-top:16px; border-top:1px solid #e2e8f0; }
.project-editor { display:flex; min-width:0; flex-direction:column; gap:14px; }
.card { padding:17px; border:1px solid #e3e8f0; border-radius:10px; background:#fff; }
.component-panel { padding:15px 0 17px; border-top:1px solid #e6eaf0; border-bottom:1px solid #e6eaf0; }
.component-tab-row { display:flex; align-items:flex-end; justify-content:space-between; gap:20px; border-bottom:1px solid #dbe2ec; }
.form-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px; margin-top:14px; }
label { display:flex; flex-direction:column; gap:6px; color:#374151; font-size:.87rem; font-weight:600; }
input,select,textarea { box-sizing:border-box; width:100%; padding:9px 10px; border:1px solid #d1d8e2; border-radius:7px; background:#fff; color:#172033; }
textarea { font-family:Consolas,'Courier New',monospace; line-height:1.45; resize:vertical; }
input:disabled { background:#f3f4f6; color:#64748b; }
.environment-tabs { display:flex; flex-wrap:wrap; gap:7px; }
.environment-tab-row { display:flex; align-items:center; justify-content:space-between; gap:16px; }
.component-list { display:flex; min-width:0; flex:1; align-items:flex-end; gap:26px; overflow-x:auto; }
.component-item { min-width:max-content; padding:11px 4px 10px; border:0; border-bottom:3px solid transparent; background:transparent; color:#475569; font-size:.95rem; font-weight:600; }
.add-component-tab { display:flex; align-items:center; gap:5px; flex:0 0 auto; margin-bottom:10px; padding:7px 10px; border:1px dashed #93b4ee; border-radius:7px; background:#f8fbff; color:#2563eb; font-size:.86rem; font-weight:600; }
.add-component-tab span { font-size:1rem; line-height:1; }
.environment-tabs { width:max-content; padding:4px; border-radius:8px; background:#f1f5f9; }
.environment-tabs button { min-width:74px; padding:8px 14px; border:0; border-radius:6px; background:transparent; color:#64748b; font-weight:600; }
.environment-tabs button.active { background:#fff; color:#1d4ed8; box-shadow:0 1px 3px rgb(15 23 42 / 14%); }
.environment-tabs.tertiary { background:#eef2f7; }
.environment-tabs.tertiary button { min-width:70px; padding:7px 13px; font-size:.86rem; }
.component-basic { display:grid; grid-template-columns:minmax(220px,1fr) minmax(260px,1.4fr); gap:12px; align-items:end; margin-top:14px; padding:14px; border:1px solid #e2e8f0; border-radius:8px; background:#f8fafc; }
.auto-value { display:flex; min-height:40px; align-items:center; gap:9px; padding:0 11px; border:1px solid #e2e8f0; border-radius:7px; background:#f8fafc; }
.auto-value span,.auto-value small { color:#64748b; font-size:.78rem; }
.auto-value strong { overflow:hidden; color:#334155; font-family:Consolas,'Courier New',monospace; font-size:.84rem; text-overflow:ellipsis; white-space:nowrap; }
.auto-value small { margin-left:auto; color:#059669; white-space:nowrap; }
.rule-empty,.empty-state { padding:30px; color:#768197; text-align:center; }
.rule-empty .save-btn { margin-top:14px; }
.basic-fields { margin-top:17px; }
.upload-list { display:flex; flex-direction:column; gap:8px; margin-top:9px; }
.upload-row { display:grid; grid-template-columns:minmax(0,1fr); gap:10px; align-items:end; }
.target-heading { margin-top:22px; padding-top:18px; border-top:1px solid #edf0f4; }
.add-server-btn { display:flex; align-items:center; gap:5px; flex:0 0 auto; padding:8px 11px; border:1px dashed #93b4ee; border-radius:7px; background:#f8fbff; color:#2563eb; font-size:.86rem; font-weight:600; white-space:nowrap; }
.add-server-btn span { font-size:1rem; line-height:1; }
.add-server-btn:disabled { border-color:#dbe2ec; background:#f8fafc; color:#94a3b8; }
.target-list { display:flex; flex-direction:column; gap:10px; margin-top:11px; }
.deployment-card { padding:13px; border:1px solid #dbe2ec; border-radius:8px; background:#fbfcfe; }
.target-card-main { display:grid; grid-template-columns:auto minmax(200px,1fr) auto; gap:11px; align-items:center; }
.server-order { min-width:max-content; color:#334155; font-size:.87rem; }
.compact-empty { padding:18px; }
.inline-advanced,.advanced-panel { margin-top:12px; border-top:1px solid #e6eaf0; }
.inline-advanced summary,.advanced-panel summary { padding-top:11px; color:#526074; font-size:.86rem; font-weight:600; cursor:pointer; }
.advanced-panel { margin-top:20px; padding:14px; border:1px solid #e2e8f0; border-radius:8px; background:#f8fafc; }
.advanced-panel summary { padding:0; color:#334155; }
.advanced-fields { display:flex; flex-direction:column; gap:12px; margin-top:13px; }
.advanced-fields label small { color:#64748b; font-size:.78rem; font-weight:400; line-height:1.45; }
.advanced-artifacts { display:flex; flex-direction:column; gap:9px; margin-top:16px; }
.artifact-row { display:grid; grid-template-columns:.65fr 1.6fr auto; gap:10px; align-items:end; }
.project-advanced { margin-top:0; }
.add-btn,.save-btn,.secondary-btn,.remove-btn { padding:8px 12px; border-radius:7px; font-size:.87rem; }
.add-btn { border:1px solid #2563eb; background:#fff; color:#2563eb; font-weight:600; }
.save-btn { border:1px solid #2563eb; background:#2563eb; color:#fff; font-weight:600; }
.secondary-btn { border:1px solid #cbd5e1; background:#fff; color:#475569; }
.remove-btn { border:1px solid #fecaca; background:#fff; color:#dc2626; }
.tab-delete-btn { margin-bottom:10px; white-space:nowrap; }
.clear-config-btn { white-space:nowrap; }
.text-danger { padding:8px 2px; border:0; background:transparent; color:#dc2626; font-size:.84rem; }
.alert { margin-bottom:14px; padding:10px 12px; border:1px solid; border-radius:7px; }
.error-alert { border-color:#fca5a5; background:#fef2f2; color:#b91c1c; }
.success-alert { border-color:#86efac; background:#f0fdf4; color:#166534; }
.dialog-backdrop { position:fixed; z-index:20; inset:0; display:grid; place-items:center; padding:18px; background:rgb(15 23 42 / 48%); }
.environment-dialog,.create-dialog { display:flex; width:min(100%,650px); max-height:calc(100vh - 36px); flex-direction:column; gap:14px; overflow:auto; padding:20px; border-radius:10px; background:#fff; box-shadow:0 18px 42px rgb(15 23 42 / 25%); }
.create-dialog { width:min(100%,450px); }
.close-btn { border:0; background:transparent; font-size:1.5rem; }
fieldset { display:grid; gap:8px; padding:13px; border:1px solid #d8dee9; border-radius:8px; }
legend { padding:0 6px; color:#374151; font-size:.87rem; font-weight:600; }
.server-check { flex-direction:row; align-items:center; font-weight:400; }
.server-check input { width:auto; }
.server-check span { display:flex; flex-direction:column; }
.server-check small { color:#7b8798; }
.create-preview { display:flex; flex-direction:column; gap:4px; padding:12px; border:1px solid #bfdbfe; border-radius:8px; background:#eff6ff; }
.create-preview span,.create-preview small { color:#64748b; font-size:.8rem; }
.create-preview strong { color:#1d4ed8; font-family:Consolas,'Courier New',monospace; }
.extra-row { display:grid; grid-template-columns:.7fr 1.3fr auto; gap:8px; margin-top:8px; }
.dialog-actions { display:flex; justify-content:flex-end; gap:8px; }
@media (max-width:850px) {
  .form-grid,.component-basic { grid-template-columns:1fr; }
  .project-primary-nav { align-items:flex-start; flex-direction:column; gap:4px; }
  .project-list { width:100%; }
  .add-project-tab { margin-bottom:8px; }
  .component-tab-row { align-items:flex-start; flex-direction:column; }
  .component-list { width:100%; }
  .environment-tab-row { align-items:flex-start; flex-direction:column; }
  .artifact-row,.extra-row { grid-template-columns:1fr; }
  .target-card-main { grid-template-columns:1fr auto; }
  .server-order { grid-column:1 / -1; }
  .section-heading { align-items:flex-start; flex-wrap:wrap; }
}
</style>
