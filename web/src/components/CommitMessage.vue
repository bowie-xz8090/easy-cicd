<template>
  <div class="commit-message">
    <span>{{ displayed }}</span><template v-if="long && !expanded">…</template>
    <button v-if="long" type="button" class="commit-toggle" :aria-expanded="expanded" @click.stop="expanded = !expanded">{{ expanded ? '收起' : '更多' }}</button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
const props = defineProps<{ message: string }>()
const expanded = ref(false)
const characters = computed(() => Array.from(props.message || ''))
const long = computed(() => characters.value.length > 100)
const displayed = computed(() => !props.message ? '-' : expanded.value ? props.message : characters.value.slice(0, 100).join(''))
watch(() => props.message, () => { expanded.value = false })
</script>

<style scoped>
.commit-message { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.6; }
.commit-toggle { display: inline; margin-left: 5px; padding: 0; border: 0; background: none; color: #2563eb; cursor: pointer; font: inherit; white-space: nowrap; }
.commit-toggle:hover { text-decoration: underline; }
</style>
