<script setup lang="ts">
import { ref } from 'vue'
import { Check, Copy } from 'lucide-vue-next'
import { useToastStore } from '@/stores/toast'

const props = withDefaults(defineProps<{ text: string; label?: string; small?: boolean }>(), {
  label: '',
  small: false,
})
const toast = useToastStore()
const copied = ref(false)

async function copy() {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(props.text)
    } else {
      const ta = document.createElement('textarea')
      ta.value = props.text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      ta.remove()
    }
    copied.value = true
    toast.success('已复制')
    window.setTimeout(() => (copied.value = false), 1500)
  } catch {
    toast.error('复制失败', '请手动选择内容复制')
  }
}
</script>

<template>
  <button
    type="button"
    :class="['btn-ghost', small ? 'h-7 px-2 text-[11.5px]' : '']"
    :title="label || '复制'"
    @click="copy"
  >
    <component :is="copied ? Check : Copy" :class="small ? 'h-3.5 w-3.5' : 'h-4 w-4'" />
    <span v-if="label">{{ copied ? '已复制' : label }}</span>
  </button>
</template>
