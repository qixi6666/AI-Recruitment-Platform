<script setup lang="ts">
import { computed, ref } from 'vue'
import { LogIn, UserPlus } from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'
import type { Role } from '../types/api'

const emit = defineEmits<{ signedIn: [] }>()

const auth = useAuthStore()
const mode = ref<'login' | 'register'>('login')
const role = ref<Role>('candidate')
const username = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

const title = computed(() => (mode.value === 'login' ? '登录账号' : '创建账号'))

async function submit() {
  error.value = ''
  loading.value = true
  try {
    const payload = { username: username.value.trim(), password: password.value, role: role.value }
    if (mode.value === 'login') {
      await auth.login(payload)
    } else {
      await auth.register(payload)
    }
    emit('signedIn')
  } catch (err) {
    error.value = err instanceof Error ? err.message : '操作失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <section class="auth-panel">
    <div>
      <p class="eyebrow">Recruitment Console</p>
      <h2>{{ title }}</h2>
      <p class="muted">候选人可以完善档案并投递岗位，HR 可以维护岗位、查看投递并使用简历推荐。</p>
    </div>

    <div class="segmented" aria-label="账号操作">
      <button :class="{ active: mode === 'login' }" type="button" @click="mode = 'login'">
        <LogIn :size="16" />
        登录
      </button>
      <button :class="{ active: mode === 'register' }" type="button" @click="mode = 'register'">
        <UserPlus :size="16" />
        注册
      </button>
    </div>

    <form class="form-grid" @submit.prevent="submit">
      <label>
        <span>角色</span>
        <select v-model="role">
          <option value="candidate">候选人</option>
          <option value="hr">HR</option>
        </select>
      </label>
      <label>
        <span>用户名</span>
        <input v-model="username" autocomplete="username" required minlength="3" placeholder="例如 hr001" />
      </label>
      <label>
        <span>密码</span>
        <input
          v-model="password"
          autocomplete="current-password"
          required
          minlength="6"
          type="password"
          placeholder="至少 6 位"
        />
      </label>
      <p v-if="error" class="inline-error">{{ error }}</p>
      <button class="primary wide" type="submit" :disabled="loading">
        <LogIn :size="17" />
        {{ loading ? '处理中...' : title }}
      </button>
    </form>
  </section>
</template>
