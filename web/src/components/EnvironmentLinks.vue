<template>
  <section v-if="environments.length" class="env-links-section">
    <header class="env-links-header">
      <h3>环境访问</h3>
      <input v-model="searchQuery" type="search" class="env-links-search" placeholder="搜索入口名称" aria-label="搜索项目或入口名称" />
    </header>
    <div ref="environmentTabs" class="env-tabs" role="tablist" aria-label="访问环境">
      <button
        v-for="(environment, index) in environments"
        :id="tabId(environment.key)"
        :key="environment.key"
        type="button"
        role="tab"
        class="env-tab"
        :class="{ 'is-production': environment.key.toLowerCase() === 'prod' }"
        :aria-selected="activeEnvironment?.key === environment.key"
        :aria-controls="`${id}-panel`"
        :tabindex="activeEnvironment?.key === environment.key ? 0 : -1"
        @click="selectedEnvironmentKey = environment.key"
        @keydown="handleTabKeydown($event, index)"
      >
        {{ environment.label }} <span class="env-tab-code">{{ environment.key.toUpperCase() }}</span>
      </button>
    </div>
    <div v-if="activeEnvironment" :id="`${id}-panel`" class="env-links-panel" role="tabpanel" :aria-labelledby="tabId(activeEnvironment.key)">
      <div class="env-links-context">
        <span class="env-current-environment" :class="{ 'is-production': activeEnvironment.key.toLowerCase() === 'prod' }" role="status" aria-live="polite">
          <span class="env-current-dot" aria-hidden="true"></span>当前：{{ activeEnvironment.label }}
        </span>
        <span class="env-link-count">{{ visibleLinkCount }} 个入口</span>
      </div>
      <div class="env-link-groups">
        <fieldset v-for="group in visibleGroups" :key="group.label" class="env-link-group">
          <legend class="env-group-label">{{ group.label }}</legend>
          <div class="env-link-group-body">
            <div class="env-link-buttons">
              <template v-for="link in group.links" :key="link.label">
                <a v-if="link.url" :href="link.url" target="_blank" rel="noopener noreferrer" class="env-link-btn" :aria-label="`${activeEnvironment.label}：${link.label}`">
                  {{ link.label }}
                </a>
                <button v-else type="button" class="env-link-btn unavailable" :aria-label="`${activeEnvironment.label}：${link.label}，暂未部署`" @click="showUnavailable(activeEnvironment.label, link.label, $event)">
                  {{ link.label }}
                </button>
              </template>
            </div>
            <span class="env-link-count">{{ group.links.length }} 个入口</span>
          </div>
        </fieldset>
      </div>
      <p v-if="!visibleGroups.length" class="env-links-empty" role="status">没有找到匹配的入口</p>
    </div>
    <div v-if="unavailableMessage" class="unavailable-overlay" @click.self="closeUnavailable" @keydown.esc.stop="closeUnavailable">
      <section class="unavailable-dialog" role="alertdialog" aria-modal="true" aria-labelledby="unavailable-title" aria-describedby="unavailable-message">
        <h3 id="unavailable-title">暂未部署</h3>
        <p id="unavailable-message">{{ unavailableMessage }}</p>
        <button ref="closeButton" type="button" class="dialog-close" @click="closeUnavailable" @keydown.tab.prevent>知道了</button>
      </section>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useId } from 'vue'
import { fetchEnvironments, type Environment } from '../api/index'
import { groupEnvironmentLinks } from '../utils/environmentLinks'

const environments = ref<Environment[]>([])
const id = useId()
const selectedEnvironmentKey = ref('')
const searchQuery = ref('')
const environmentTabs = ref<HTMLDivElement | null>(null)
const activeEnvironment = computed(() => environments.value.find(environment => environment.key === selectedEnvironmentKey.value) ?? environments.value[0])
const visibleGroups = computed(() => {
  if (!activeEnvironment.value) return []
  const groups = groupEnvironmentLinks(activeEnvironment.value)
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return groups
  return groups.map(group => ({
    ...group,
    links: group.links.filter(link => `${group.label} ${link.label}`.toLowerCase().includes(query)),
  })).filter(group => group.links.length)
})
const visibleLinkCount = computed(() => visibleGroups.value.reduce((count, group) => count + group.links.length, 0))
const unavailableMessage = ref('')
const closeButton = ref<HTMLButtonElement | null>(null)
let unavailableTrigger: HTMLElement | null = null

function tabId(key: string) {
  return `${id}-tab-${encodeURIComponent(key)}`
}

function handleTabKeydown(event: KeyboardEvent, index: number) {
  let nextIndex: number
  switch (event.key) {
    case 'ArrowRight': nextIndex = (index + 1) % environments.value.length; break
    case 'ArrowLeft': nextIndex = (index - 1 + environments.value.length) % environments.value.length; break
    case 'Home': nextIndex = 0; break
    case 'End': nextIndex = environments.value.length - 1; break
    default: return
  }
  event.preventDefault()
  selectedEnvironmentKey.value = environments.value[nextIndex].key
  environmentTabs.value?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[nextIndex]?.focus()
}

async function showUnavailable(environment: string, label: string, event: MouseEvent) {
  unavailableTrigger = event.currentTarget as HTMLElement
  unavailableMessage.value = `${environment}的「${label}」暂未部署。`
  await nextTick()
  closeButton.value?.focus()
}

function closeUnavailable() {
  unavailableMessage.value = ''
  unavailableTrigger?.focus()
  unavailableTrigger = null
}

onMounted(async () => {
  try {
    environments.value = await fetchEnvironments()
    selectedEnvironmentKey.value = environments.value.find(environment => environment.key === 'dev')?.key ?? environments.value[0]?.key ?? ''
  } catch {
    environments.value = []
  }
})
</script>

<style scoped>
.env-links-section { padding: 20px 16px; border-bottom: 1px solid #e5e7eb; }
.env-links-section h3 { margin: 0; font-size: 1rem; font-weight: 600; }
.env-links-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; height: 22px; margin-bottom: 12px; }
.env-links-header > h3 { flex-shrink: 0; }
.env-links-search { box-sizing: border-box; flex: 0 1 205px; width: 205px; max-width: 100%; min-width: 0; height: 22px; padding: 2px 8px; border: 1px solid #e5e7eb; border-radius: 5px; color: #374151; background: #fff; font: inherit; font-size: .75rem; }
.env-tabs { display: flex; flex-wrap: wrap; gap: 0 30px; border-bottom: 1px solid #e5e7eb; }
.env-tab { display: inline-flex; align-items: center; gap: 8px; padding: 8px 3px 9px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: #6b7280; font: inherit; font-size: .875rem; cursor: pointer; }
.env-tab-code { font-size: .6875rem; font-weight: 400; }
.env-tab[aria-selected="true"] { border-bottom-color: #3159d8; color: #3159d8; font-weight: 600; }
.env-tab.is-production[aria-selected="true"] { border-bottom-color: #c34d52; color: #c34d52; }
.env-links-panel { padding-top: 12px; }
.env-links-context { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 12px; }
.env-current-environment { display: inline-flex; align-items: center; gap: 7px; color: #374151; font-size: .75rem; font-weight: 400; }
.env-current-dot { width: 5px; height: 5px; border-radius: 50%; background: #3159d8; }
.env-current-environment.is-production { color: #c34d52; }
.env-current-environment.is-production .env-current-dot { background: currentColor; }
.env-link-count { flex-shrink: 0; color: #6b7280; font-size: .75rem; white-space: nowrap; }
.env-link-groups { display: flex; min-width: 0; flex-direction: column; gap: 14px; }
.env-link-group { min-inline-size: 0; margin: 0; padding: 12px 16px 14px; border: 1px solid #e5e7eb; border-radius: 6px; background: #fafafa; }
.env-group-label { max-width: 100%; padding: 0 8px; color: #6b7280; font-size: .875rem; font-weight: 400; }
.env-link-group-body { display: flex; align-items: center; justify-content: space-between; gap: 15px; min-width: 0; }
.env-link-buttons { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.env-link-btn { padding: 4px 12px; border: 0; border-radius: 4px; background: #e0e7ff; color: #3730a3; font: inherit; font-size: .8rem; font-weight: 500; text-decoration: none; cursor: pointer; }
.env-link-btn:hover { background: #c7d2fe; }
.env-link-btn.unavailable { color: #64748b; background: #e5e7eb; }
.env-link-btn.unavailable:hover { background: #d1d5db; }
.env-links-empty { padding: 24px 0; margin: 0; color: #6b7280; text-align: center; font-size: .875rem; }
.unavailable-overlay { position: fixed; inset: 0; z-index: 1000; display: flex; align-items: center; justify-content: center; background: #0f172a66; padding: 20px; }
.unavailable-dialog { width: 360px; max-width: 100%; border-radius: 10px; background: #fff; padding: 24px; box-shadow: 0 16px 48px #0f172a33; }
.unavailable-dialog p { margin: 0 0 20px; color: #64748b; overflow-wrap: anywhere; }
.dialog-close { display: block; margin-left: auto; border: 0; border-radius: 6px; padding: 8px 18px; color: #fff; background: #2563eb; font: inherit; cursor: pointer; }
@media (max-width: 720px) {
  .env-link-group-body { align-items: flex-start; }
  .env-link-group-body > .env-link-count { padding-top: 4px; }
}
@media (max-width: 560px) {
  .env-tabs { gap: 0 21px; }
  .env-tab { font-size: .8125rem; }
  .env-tab-code { display: none; }
  .env-link-group { padding: 10px 13px 12px; }
  .env-link-group-body { flex-direction: column; gap: 11px; }
  .env-link-group-body > .env-link-count { align-self: flex-end; padding-top: 0; }
}
</style>
