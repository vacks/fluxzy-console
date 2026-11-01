<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Pencil, Plus, RefreshCw, Trash2, UserRound, Users } from 'lucide-vue-next'
import { api } from '@/api/client'
import type { User } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'
import Button from '@/components/ui/Button.vue'
import Input from '@/components/ui/Input.vue'
import Switch from '@/components/ui/Switch.vue'
import Badge from '@/components/ui/Badge.vue'
import Modal from '@/components/ui/Modal.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import { formatDateTime, formatRelative } from '@/utils/format'

const auth = useAuthStore()
const toast = useToastStore()

const users = ref<User[]>([])
const loading = ref(false)
const addOpen = ref(false)
const editOpen = ref(false)
const deleteOpen = ref(false)
const editTarget = ref<User | null>(null)
const deleteTarget = ref<User | null>(null)
const busy = ref(false)

const addForm = ref({ username: '', password: '' })
const editForm = ref({ disabled: false })

async function load() {
  loading.value = true
  try {
    const r = await api.get<{ users: User[] }>('/users')
    users.value = r.users || []
  } catch (e: any) {
    toast.error('读取用户失败', e?.message)
  } finally {
    loading.value = false
  }
}

onMounted(load)

function openAdd() {
  addForm.value = { username: '', password: '' }
  addOpen.value = true
}

async function createUser() {
  if (addForm.value.username.trim().length < 3) {
    toast.error('用户名至少 3 个字符')
    return
  }
  if (addForm.value.password.length < 10) {
    toast.error('密码至少 5 个字符')
    return
  }
  busy.value = true
  try {
    await api.post('/users', addForm.value)
    toast.success('用户已创建')
    addOpen.value = false
    await load()
  } catch (e: any) {
    toast.error('创建失败', e?.message)
  } finally {
    busy.value = false
  }
}

function openEdit(u: User) {
  editTarget.value = u
  editForm.value = { disabled: u.disabled }
  editOpen.value = true
}

async function saveEdit() {
  if (!editTarget.value) return
  busy.value = true
  try {
    await api.patch(`/users/${editTarget.value.id}`, editForm.value)
    toast.success('用户已更新')
    editOpen.value = false
    await load()
  } catch (e: any) {
    toast.error('更新失败', e?.message)
  } finally {
    busy.value = false
  }
}

function openDelete(u: User) {
  deleteTarget.value = u
  deleteOpen.value = true
}

async function deleteUser() {
  if (!deleteTarget.value) return
  busy.value = true
  try {
    await api.del(`/users/${deleteTarget.value.id}`)
    toast.success('用户已删除')
    deleteOpen.value = false
    await load()
  } catch (e: any) {
    toast.error('删除失败', e?.message)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <section class="panel flex flex-col gap-4 p-4 lg:flex-row lg:items-center lg:justify-between">
      <div class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10">
          <Users class="h-5 w-5 text-accent" />
        </div>
        <div>
          <h2 class="text-[15px] font-semibold tracking-tight text-ink">用户管理</h2>
          <p class="mt-0.5 text-[11.5px] text-muted">所有用户均拥有管理员权限，可查看流量、管理规则与用户。</p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <Button size="sm" :loading="loading" @click="load">
          <template #icon><RefreshCw class="h-3.5 w-3.5" /></template>
          刷新
        </Button>
        <Button size="sm" variant="primary" @click="openAdd">
          <template #icon><Plus class="h-3.5 w-3.5" /></template>
          新增用户
        </Button>
      </div>
    </section>

    <section class="panel overflow-hidden">
      <div class="overflow-x-auto">
        <table class="table">
          <thead>
            <tr>
              <th>用户</th>
              <th>状态</th>
              <th class="hidden md:table-cell">创建时间</th>
              <th class="hidden md:table-cell">最近登录</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody v-if="users.length">
            <tr v-for="u in users" :key="u.id">
              <td>
                <div class="flex items-center gap-2">
                  <span class="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-accent-strong to-accent-2-strong text-[11px] font-semibold text-white">
                    {{ u.username.slice(0, 1).toUpperCase() }}
                  </span>
                  <span class="font-mono text-[12px] text-ink">{{ u.username }}</span>
                  <Badge v-if="u.id === auth.user?.id" variant="muted">当前账号</Badge>
                </div>
              </td>
              <td>
                <Badge :variant="u.disabled ? 'bad' : 'ok'" dot>{{ u.disabled ? '已禁用' : '正常' }}</Badge>
              </td>
              <td class="hidden text-[11px] text-muted md:table-cell">{{ formatDateTime(u.createdAt) }}</td>
              <td class="hidden text-[11px] text-muted md:table-cell">{{ u.lastLoginAt ? formatRelative(u.lastLoginAt) : '从未登录' }}</td>
              <td>
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="ghost" class="whitespace-nowrap" @click="openEdit(u)">
                    <template #icon><Pencil class="h-3.5 w-3.5" /></template>
                    编辑
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    class="text-bad hover:text-bad"
                    :disabled="u.id === auth.user?.id"
                    @click="openDelete(u)"
                  >
                    <template #icon><Trash2 class="h-3.5 w-3.5" /></template>
                  </Button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <EmptyState v-if="!users.length && !loading" title="没有用户" description="点击右上角新增用户。">
          <template #icon><UserRound class="h-5 w-5" /></template>
        </EmptyState>
      </div>
    </section>

    <Modal v-model:open="addOpen" title="新增用户" width="480px">
      <div class="space-y-4">
        <Input v-model="addForm.username" label="用户名" placeholder="至少 3 个字符" autocomplete="off" />
        <Input v-model="addForm.password" label="初始密码" type="password" placeholder="至少 5 个字符" autocomplete="new-password" />
        <div class="rounded-xl border border-line/60 bg-panel-strong/40 p-3 text-[11px] leading-relaxed text-faint">
          新用户将拥有管理员权限，可查看流量、管理规则与用户。
        </div>
      </div>
      <template #footer>
        <Button variant="ghost" @click="addOpen = false">取消</Button>
        <Button variant="primary" :loading="busy" @click="createUser">创建用户</Button>
      </template>
    </Modal>

    <Modal v-model:open="editOpen" title="编辑用户" width="480px">
      <div v-if="editTarget" class="space-y-4">
        <div class="rounded-xl border border-line/60 bg-panel-strong/40 p-3">
          <div class="font-mono text-[12px] text-ink">{{ editTarget.username }}</div>
          <div class="mt-0.5 text-[10.5px] text-faint">创建于 {{ formatDateTime(editTarget.createdAt) }}</div>
        </div>
        <div class="rounded-xl border border-line/60 bg-panel-strong/40 p-3 text-[11px] leading-relaxed text-faint">
          角色：管理员（固定）。
        </div>
        <Switch v-model="editForm.disabled" label="禁用此账号" hint="禁用后该账号的登录会话会立即失效" />
      </div>
      <template #footer>
        <Button variant="ghost" @click="editOpen = false">取消</Button>
        <Button variant="primary" :loading="busy" @click="saveEdit">保存</Button>
      </template>
    </Modal>

    <Modal v-model:open="deleteOpen" title="删除用户" width="440px">
      <div v-if="deleteTarget" class="space-y-3 text-[12px] text-muted">
        <div class="rounded-xl border border-bad/25 bg-bad/8 p-3 text-bad">
          确定删除用户 <b class="font-mono">{{ deleteTarget.username }}</b>？该操作不可恢复。
        </div>
        <p>该用户的登录会话会被立即清除；如果这是最后一个管理员，服务端会拒绝删除。</p>
      </div>
      <template #footer>
        <Button variant="ghost" @click="deleteOpen = false">取消</Button>
        <Button variant="danger" :loading="busy" @click="deleteUser">确认删除</Button>
      </template>
    </Modal>
  </div>
</template>
