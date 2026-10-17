<script setup>
import { computed } from 'vue'

const props = defineProps({
  page: { type: Number, required: true },
  totalPages: { type: Number, required: true }
})
const emit = defineEmits(['change'])

// 1 … 4 5 [6] 7 8 … 20
const items = computed(() => {
  const { page, totalPages } = props
  const result = []
  const from = Math.max(2, page - 2)
  const to = Math.min(totalPages - 1, page + 2)
  result.push(1)
  if (from > 2) result.push('…')
  for (let p = from; p <= to; p++) result.push(p)
  if (to < totalPages - 1) result.push('…')
  if (totalPages > 1) result.push(totalPages)
  return result
})

function go(p) {
  if (p >= 1 && p <= props.totalPages && p !== props.page) emit('change', p)
}
</script>

<template>
  <nav v-if="totalPages > 1" class="pagination" aria-label="Страницы">
    <button class="page-btn" :disabled="page === 1" @click="go(page - 1)" aria-label="Назад">‹</button>
    <template v-for="(item, i) in items" :key="i">
      <span v-if="item === '…'" class="page-gap">…</span>
      <button
        v-else
        class="page-btn"
        :class="{ current: item === page }"
        :aria-current="item === page ? 'page' : undefined"
        @click="go(item)"
      >
        {{ item }}
      </button>
    </template>
    <button class="page-btn" :disabled="page === totalPages" @click="go(page + 1)" aria-label="Вперёд">›</button>
  </nav>
</template>
