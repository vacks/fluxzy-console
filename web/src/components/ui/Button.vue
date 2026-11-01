<script setup lang="ts">
import { computed } from 'vue'
import Spinner from './Spinner.vue'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
    size?: 'sm' | 'md' | 'lg'
    type?: 'button' | 'submit' | 'reset'
    loading?: boolean
    disabled?: boolean
    block?: boolean
    iconOnly?: boolean
  }>(),
  {
    variant: 'secondary',
    size: 'md',
    type: 'button',
    loading: false,
    disabled: false,
    block: false,
    iconOnly: false,
  },
)

defineEmits<{ click: [MouseEvent] }>()

const cls = computed(() => {
  const variant =
    props.variant === 'primary'
      ? 'btn-primary'
      : props.variant === 'danger'
        ? 'btn-danger'
        : props.variant === 'ghost'
          ? 'btn-ghost'
          : 'btn-secondary'
  const size =
    props.size === 'sm' ? 'px-2.5 py-1.5 text-[12px]' : props.size === 'lg' ? 'px-5 py-3 text-sm' : ''
  const icon = props.iconOnly ? 'h-9 w-9 px-0' : ''
  return [variant, size, icon, props.block ? 'w-full' : ''].join(' ')
})
</script>

<template>
  <button
    :type="type"
    :class="cls"
    :disabled="disabled || loading"
    @click="$emit('click', $event)"
  >
    <Spinner v-if="loading" class="h-3.5 w-3.5" />
    <slot v-else name="icon" />
    <slot />
  </button>
</template>
