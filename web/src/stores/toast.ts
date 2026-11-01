import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastKind = 'success' | 'error' | 'info' | 'warning'

export interface ToastItem {
  id: number
  kind: ToastKind
  title: string
  message?: string
}

let seq = 1

export const useToastStore = defineStore('toast', () => {
  const items = ref<ToastItem[]>([])

  function push(kind: ToastKind, title: string, message?: string) {
    const id = seq++
    items.value.push({ id, kind, title, message })
    window.setTimeout(() => remove(id), kind === 'error' ? 7000 : 4200)
  }

  function remove(id: number) {
    items.value = items.value.filter((t) => t.id !== id)
  }

  const success = (title: string, message?: string) => push('success', title, message)
  const error = (title: string, message?: string) => push('error', title, message)
  const info = (title: string, message?: string) => push('info', title, message)
  const warning = (title: string, message?: string) => push('warning', title, message)

  return { items, push, remove, success, error, info, warning }
})
