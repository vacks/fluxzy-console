<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Check, LockKeyhole, ShieldCheck, UserRound } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import Button from '@/components/ui/Button.vue'
import Input from '@/components/ui/Input.vue'
import BrandMark from '@/components/ui/BrandMark.vue'

const auth = useAuthStore()
const router = useRouter()

const username = ref('')
const password = ref('')
const confirm = ref('')
const loading = ref(false)
const error = ref('')

const strength = computed(() => {
  let score = 0
  if (password.value.length >= 10) score++
  if (password.value.length >= 14) score++
  if (/[A-Z]/.test(password.value) && /[a-z]/.test(password.value)) score++
  if (/\d/.test(password.value)) score++
  if (/[^A-Za-z0-9]/.test(password.value)) score++
  return Math.min(score, 4)
})
const strengthLabel = computed(() => ['太短', '较弱', '一般', '较强', '很强'][strength.value])
const strengthColor = computed(
  () => ['bg-bad', 'bg-bad', 'bg-warn', 'bg-ok', 'bg-ok'][strength.value],
)

async function submit() {
  error.value = ''
  if (username.value.trim().length < 3) {
    error.value = '用户名至少 3 个字符'
    return
  }
  if (password.value.length < 10) {
    error.value = '密码至少 5 个字符'
    return
  }
  if (password.value !== confirm.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  loading.value = true
  try {
    await auth.setup(username.value.trim(), password.value)
    router.replace('/')
  } catch (e: any) {
    error.value = e?.message || '初始化失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="relative z-10 flex min-h-screen items-center justify-center px-5 py-12">
    <div class="absolute inset-0 -z-10 overflow-hidden">
      <div class="absolute left-1/2 top-[-20%] h-[560px] w-[760px] -translate-x-1/2 rounded-full bg-accent/15 blur-[130px]" />
      <div class="absolute bottom-[-25%] left-[8%] h-[420px] w-[420px] rounded-full bg-accent-2/10 blur-[120px]" />
    </div>

    <div class="w-full max-w-[470px] animate-fade-up">
      <div class="mb-8 flex flex-col items-center text-center">
        <BrandMark size="lg" />
        <h1 class="mt-4 text-[24px] font-semibold tracking-tight text-ink">初始化 FLUXZY <span class="text-accent">·</span> CONSOLE</h1>
        <p class="mt-1.5 max-w-sm text-[12.5px] leading-relaxed text-muted">
          首次使用需要创建管理员账号。这个账号拥有配置规则、管理用户和查看全部流量的权限。
        </p>
      </div>

      <div class="panel p-6">
        <form class="space-y-4" @submit.prevent="submit">
          <div class="relative">
            <UserRound class="pointer-events-none absolute left-3.5 top-[38px] h-4 w-4 text-faint" />
            <Input v-model="username" label="管理员用户名" placeholder="admin" autocomplete="username" class="[&_input]:pl-10" />
          </div>
          <div class="relative">
            <LockKeyhole class="pointer-events-none absolute left-3.5 top-[38px] h-4 w-4 text-faint" />
            <Input
              v-model="password"
              label="密码"
              type="password"
              placeholder="至少 5 个字符"
              autocomplete="new-password"
              class="[&_input]:pl-10"
            />
            <div class="mt-2 flex items-center gap-2">
              <div class="h-1 flex-1 overflow-hidden rounded-full bg-panel-strong">
                <div class="h-full rounded-full transition-all" :class="strengthColor" :style="{ width: `${(strength / 4) * 100}%` }" />
              </div>
              <span class="w-8 text-right text-[10.5px] text-faint">{{ strengthLabel }}</span>
            </div>
          </div>
          <div class="relative">
            <ShieldCheck class="pointer-events-none absolute left-3.5 top-[38px] h-4 w-4 text-faint" />
            <Input
              v-model="confirm"
              label="确认密码"
              type="password"
              placeholder="再次输入密码"
              autocomplete="new-password"
              class="[&_input]:pl-10"
            />
          </div>

          <div v-if="error" class="rounded-xl border border-bad/30 bg-bad/10 px-3.5 py-2.5 text-[12px] text-bad">
            {{ error }}
          </div>

          <Button type="submit" variant="primary" size="lg" block :loading="loading">
            <template #icon><Check class="h-4 w-4" /></template>
            创建管理员并登录
          </Button>
        </form>

        <div class="mt-5 grid grid-cols-3 gap-2 border-t border-line/50 pt-4 text-[10.5px] text-faint">
          <div class="flex items-center gap-1.5"><Check class="h-3 w-3 text-ok" /> 本地账号</div>
          <div class="flex items-center gap-1.5"><Check class="h-3 w-3 text-ok" /> Argon2id</div>
          <div class="flex items-center gap-1.5"><Check class="h-3 w-3 text-ok" /> 操作审计</div>
        </div>
      </div>
    </div>
  </div>
</template>
