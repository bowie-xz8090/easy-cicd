<template>
  <section v-if="environments.length" class="env-links-section">
    <h3>环境访问</h3>
    <div class="env-links-grid">
      <div v-for="environment in environments" :key="environment.key" class="env-link-card">
        <span class="env-link-label">{{ environment.label }}</span>
        <div class="env-link-buttons">
          <a
            v-if="environment.user_url"
            :href="environment.user_url"
            target="_blank"
            rel="noopener noreferrer"
            class="env-link-btn user-btn"
          >
            用户端
          </a>
          <a
            v-if="environment.admin_url"
            :href="environment.admin_url"
            target="_blank"
            rel="noopener noreferrer"
            class="env-link-btn admin-btn"
          >
            管理端
          </a>
          <template v-if="environment.extra?.length">
            <span class="env-link-divider">|</span>
            <a
              v-for="link in environment.extra"
              :key="link.url"
              :href="link.url"
              target="_blank"
              rel="noopener noreferrer"
              class="env-link-btn extra-btn"
            >
              {{ link.label }}
            </a>
          </template>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchEnvironments, type Environment } from '../api/index'

const environments = ref<Environment[]>([])

onMounted(async () => {
  try {
    environments.value = await fetchEnvironments()
  } catch {
    environments.value = []
  }
})
</script>

<style scoped>
.env-links-section {
  padding: 20px 16px;
  border-bottom: 1px solid #e5e7eb;
}

.env-links-section h3 {
  margin: 0 0 12px;
  font-size: 1rem;
  font-weight: 600;
}

.env-links-grid {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.env-link-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  background: #fafafa;
}

.env-link-label {
  min-width: 72px;
  color: #374151;
  font-size: 0.9rem;
  font-weight: 500;
}

.env-link-buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.env-link-btn {
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 0.8rem;
  font-weight: 500;
  text-decoration: none;
  transition: background 0.15s;
}

.user-btn {
  background: #dbeafe;
  color: #1d4ed8;
}

.user-btn:hover {
  background: #bfdbfe;
}

.admin-btn {
  background: #fef3c7;
  color: #92400e;
}

.admin-btn:hover {
  background: #fde68a;
}

.env-link-divider {
  margin: 0 2px;
  color: #d1d5db;
  font-size: 0.9rem;
}

.extra-btn {
  background: #e0e7ff;
  color: #3730a3;
}

.extra-btn:hover {
  background: #c7d2fe;
}

@media (max-width: 720px) {
  .env-link-card {
    align-items: flex-start;
    flex-direction: column;
  }

  .env-link-label {
    min-width: 0;
  }
}
</style>
