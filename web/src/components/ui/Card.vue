<script setup lang="ts">
withDefaults(defineProps<{ title?: string; subtitle?: string; padded?: boolean; solid?: boolean }>(), {
  padded: true,
  solid: false,
})
</script>

<template>
  <section :class="solid ? 'panel-solid' : 'panel'" class="overflow-hidden">
    <header v-if="title || $slots.header || $slots.actions" class="panel-header">
      <div class="min-w-0">
        <slot name="header">
          <h3 class="truncate text-[13.5px] font-semibold text-ink">{{ title }}</h3>
          <p v-if="subtitle" class="mt-0.5 truncate text-[11.5px] text-muted">{{ subtitle }}</p>
        </slot>
      </div>
      <div v-if="$slots.actions" class="flex shrink-0 flex-wrap items-center justify-end gap-2">
        <slot name="actions" />
      </div>
    </header>
    <div :class="padded ? 'p-5' : ''">
      <slot />
    </div>
    <footer v-if="$slots.footer" class="border-t border-line/50 px-5 py-3.5">
      <slot name="footer" />
    </footer>
  </section>
</template>
