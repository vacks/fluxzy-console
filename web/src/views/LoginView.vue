<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Activity, Eye, EyeOff, LockKeyhole, ShieldCheck, UserRound, Zap } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import Button from '@/components/ui/Button.vue'
import Input from '@/components/ui/Input.vue'
import BrandMark from '@/components/ui/BrandMark.vue'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const username = ref('')
const password = ref('')
const showPassword = ref(false)
const loading = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  if (!username.value || !password.value) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    const redirect = (route.query.redirect as string) || '/'
    router.replace(redirect)
  } catch (e: any) {
    error.value = e?.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="relative z-10 flex min-h-screen">
    <!-- brand / story panel -->
    <section class="relative hidden w-[54%] flex-col justify-between overflow-hidden border-r border-line/40 p-12 lg:flex">
      <div class="absolute inset-0 -z-10">
        <div class="absolute left-[-10%] top-[-20%] h-[520px] w-[520px] rounded-full bg-accent/20 blur-[120px]" />
        <div class="absolute bottom-[-15%] right-[-8%] h-[420px] w-[420px] rounded-full bg-accent-2/15 blur-[110px]" />
        <div class="absolute inset-0 bg-[linear-gradient(rgba(148,163,184,.055)_1px,transparent_1px),linear-gradient(90deg,rgba(148,163,184,.055)_1px,transparent_1px)] bg-[size:52px_52px] [mask-image:radial-gradient(ellipse_75%_65%_at_45%_35%,black_30%,transparent_75%)]" />
      </div>

      <div class="flex items-center gap-3">
        <BrandMark size="lg" />
        <div>
          <div class="text-lg font-semibold tracking-tight text-ink">FLUXZY <span class="text-accent">·</span> CONSOLE</div>
          <div class="text-[10.5px] uppercase tracking-[0.2em] text-faint">Traffic Observatory</div>
        </div>
      </div>

      <div class="max-w-xl animate-fade-up">
        <div class="mb-5 inline-flex items-center gap-2 rounded-full border border-accent/25 bg-accent/8 px-3 py-1 text-[11px] font-medium text-accent">
          <Zap class="h-3.5 w-3.5" /> 为 fluxzy 打造的生产级控制台
        </div>
        <h1 class="text-[42px] font-semibold leading-[1.08] tracking-[-0.03em] text-ink">
          看清每一个请求，<br />
          <span class="bg-gradient-to-r from-accent via-accent-2 to-accent bg-[length:200%_auto] bg-clip-text text-transparent animate-gradient-pan">
            掌控每一次改写。
          </span>
        </h1>
        <p class="mt-5 max-w-lg text-[13.5px] leading-relaxed text-muted">
          实时观测 fluxzy 捕获的 HTTP、HTTPS、WebSocket 与 gRPC 流量；用可视化规则拦截、改写、Mock 请求与响应；所有审计数据都可检索、可追溯。
        </p>

        <div class="mt-8 grid max-w-lg grid-cols-1 gap-2.5 sm:grid-cols-3">
          <div class="rounded-xl border border-line/50 bg-panel/40 p-3 backdrop-blur">
            <Activity class="h-4 w-4 text-accent-2" />
            <div class="mt-2 text-[12px] font-medium text-ink">实时观测</div>
            <div class="mt-0.5 text-[10.5px] leading-relaxed text-faint">WebSocket 推送，峰值自动限流</div>
          </div>
          <div class="rounded-xl border border-line/50 bg-panel/40 p-3 backdrop-blur">
            <ShieldCheck class="h-4 w-4 text-ok" />
            <div class="mt-2 text-[12px] font-medium text-ink">规则即代码</div>
            <div class="mt-0.5 text-[10.5px] leading-relaxed text-faint">可视化编辑 + 原生 YAML</div>
          </div>
          <div class="rounded-xl border border-line/50 bg-panel/40 p-3 backdrop-blur">
            <LockKeyhole class="h-4 w-4 text-warn" />
            <div class="mt-2 text-[12px] font-medium text-ink">可审计</div>
            <div class="mt-0.5 text-[10.5px] leading-relaxed text-faint">会话、版本、操作全留痕</div>
          </div>
        </div>
      </div>

      <div class="relative max-w-xl overflow-hidden rounded-2xl border border-line/50 bg-panel/55 p-4 shadow-panel backdrop-blur-xl animate-fade-up">
        <div class="flex items-center justify-between border-b border-line/40 pb-2.5">
          <div class="flex items-center gap-2 text-[11px] text-muted">
            <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-ok" /> Live capture
          </div>
          <div class="font-mono text-[10.5px] text-faint">2,847 rps · live</div>
        </div>
        <div class="mt-2 space-y-1.5 font-mono text-[11px]">
          <div class="flex items-center gap-2 text-emerald-500 dark:text-emerald-300"><span>200</span><span>GET</span><span class="truncate text-muted">https://api.example.com/v1/users</span><span class="ml-auto text-faint">42ms</span></div>
          <div class="flex items-center gap-2 text-sky-500 dark:text-sky-300"><span>201</span><span>POST</span><span class="truncate text-muted">https://api.example.com/v1/orders</span><span class="ml-auto text-faint">88ms</span></div>
          <div class="flex items-center gap-2 text-amber-500 dark:text-amber-300"><span>403</span><span>GET</span><span class="truncate text-muted">https://cdn.example.com/admin</span><span class="ml-auto text-faint">blocked</span></div>
          <div class="flex items-center gap-2 text-rose-500 dark:text-rose-300"><span>502</span><span>GET</span><span class="truncate text-muted">https://upstream.internal/health</span><span class="ml-auto text-faint">timeout</span></div>
        </div>
      </div>
    </section>

    <!-- login form -->
    <section class="flex flex-1 items-center justify-center px-5 py-10">
      <div class="w-full max-w-[390px] animate-fade-up">
        <div class="mb-8 flex items-center gap-3 lg:hidden">
          <BrandMark />
          <div>
            <div class="text-[15px] font-semibold tracking-tight text-ink">FLUXZY <span class="text-accent">·</span> CONSOLE</div>
            <div class="text-[10px] uppercase tracking-[0.18em] text-faint">Traffic Observatory</div>
          </div>
        </div>

        <h2 class="text-[24px] font-semibold tracking-tight text-ink">欢迎回来</h2>
        <p class="mt-1.5 text-[12.5px] text-muted">登录后查看实时流量、管理拦截规则与审计数据。</p>

        <form class="mt-7 space-y-4" @submit.prevent="submit">
          <div class="relative">
            <UserRound class="pointer-events-none absolute left-3.5 top-[38px] h-4 w-4 text-faint" />
            <Input v-model="username" label="用户名" placeholder="admin" autocomplete="username" class="[&_input]:pl-10" />
          </div>
          <div class="relative">
            <LockKeyhole class="pointer-events-none absolute left-3.5 top-[38px] h-4 w-4 text-faint" />
            <Input
              v-model="password"
              label="密码"
              :type="showPassword ? 'text' : 'password'"
              placeholder="输入密码"
              autocomplete="current-password"
              class="[&_input]:pl-10 [&_input]:pr-10"
            />
            <button
              type="button"
              class="absolute right-3 top-[34px] rounded-md p-1 text-faint transition hover:text-ink"
              @click="showPassword = !showPassword"
            >
              <component :is="showPassword ? EyeOff : Eye" class="h-4 w-4" />
            </button>
          </div>

          <div v-if="error" class="rounded-xl border border-bad/30 bg-bad/10 px-3.5 py-2.5 text-[12px] text-bad">
            {{ error }}
          </div>

          <Button type="submit" variant="primary" size="lg" block :loading="loading">登录控制台</Button>
        </form>

        <div class="mt-6 flex items-center justify-center gap-2 text-[11px] text-faint">
          <LockKeyhole class="h-3.5 w-3.5" />
          登录会话使用 HttpOnly Cookie 与 CSRF 保护
        </div>
      </div>
    </section>
  </div>
</template>
