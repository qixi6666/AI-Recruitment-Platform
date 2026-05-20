<script setup lang="ts">
import { computed, ref } from 'vue'
import { BriefcaseBusiness, LogOut, ShieldCheck, UserRound } from 'lucide-vue-next'
import AuthPanel from './components/AuthPanel.vue'
import CandidateWorkspace from './components/CandidateWorkspace.vue'
import HrWorkspace from './components/HrWorkspace.vue'
import PublicJobs from './components/PublicJobs.vue'
import { useAuthStore } from './stores/auth'

type ViewKey = 'jobs' | 'candidate' | 'hr'
interface ViewItem {
  key: ViewKey
  label: string
}

const auth = useAuthStore()
const activeView = ref<ViewKey>('jobs')

const availableViews = computed(() => {
  const views: ViewItem[] = [{ key: 'jobs', label: '公开岗位' }]
  if (auth.role === 'candidate') {
    views.push({ key: 'candidate' as const, label: '候选人工作台' })
  }
  if (auth.role === 'hr') {
    views.push({ key: 'hr' as const, label: 'HR 工作台' })
  }
  return views
})

function afterSignedIn() {
  activeView.value = auth.role === 'hr' ? 'hr' : 'candidate'
}

function logout() {
  auth.logout()
  activeView.value = 'jobs'
}
</script>

<template>
  <main class="app-shell">
    <header class="topbar">
      <div class="brand">
        <BriefcaseBusiness :size="25" />
        <div>
          <strong>Recruitment Console</strong>
          <span>招聘运营与候选人投递</span>
        </div>
      </div>
      <nav class="tabs" aria-label="主导航">
        <button
          v-for="view in availableViews"
          :key="view.key"
          :class="{ active: activeView === view.key }"
          type="button"
          @click="activeView = view.key"
        >
          {{ view.label }}
        </button>
      </nav>
      <div class="account">
        <template v-if="auth.isAuthed && auth.user">
          <span class="account-chip">
            <ShieldCheck v-if="auth.role === 'hr'" :size="16" />
            <UserRound v-else :size="16" />
            {{ auth.user.username }}
          </span>
          <button class="ghost icon-only" type="button" aria-label="退出登录" @click="logout">
            <LogOut :size="17" />
          </button>
        </template>
      </div>
    </header>

    <section class="hero-band">
      <div>
        <p class="eyebrow">Hiring operations</p>
        <h1>把岗位、候选人和招聘数据放在一个清爽的工作台里。</h1>
      </div>
      <AuthPanel v-if="!auth.isAuthed" @signed-in="afterSignedIn" />
    </section>

    <PublicJobs v-if="activeView === 'jobs'" />
    <CandidateWorkspace v-else-if="activeView === 'candidate' && auth.role === 'candidate'" />
    <HrWorkspace v-else-if="activeView === 'hr' && auth.role === 'hr'" />
  </main>
</template>
