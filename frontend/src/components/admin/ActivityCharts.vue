<script setup>
import { ref, computed, watch } from 'vue'
import BarChart from './BarChart.vue'
import { vStack } from './stackTable'
import { buckets } from '../../views/admin/period'
import { useLoader, mapLimit } from '../../views/admin/useAdmin'

// Графики активности за период. Отдельного ряда по дням в API нет, поэтому период делится
// на интервалы и статистика запрашивается за каждый.
const props = defineProps({
  from: { type: String, default: '' }, // ГГГГ-ММ-ДД
  to: { type: String, default: '' },
  fetch: { type: Function, required: true }, // ({ from, to }) → объект статистики
  metrics: { type: Array, required: true } // [{ key, name }]
})

const rows = ref([]) // [{ label, title, stats }]
const loader = useLoader((list) => {
  let failed = false // после первой ошибки остальные запросы не отправляем
  return mapLimit(list, 4, async (b) => {
    if (failed) return null
    try {
      return { ...b, stats: await props.fetch({ from: b.from, to: b.to }) }
    } catch (e) {
      failed = true
      throw e
    }
  })
})

watch(
  () => [props.from, props.to],
  async () => {
    const result = await loader.run(buckets(props.from, props.to))
    if (result) rows.value = result
  },
  { immediate: true }
)

const charts = computed(() =>
  props.metrics.map((m) => ({
    ...m,
    points: rows.value.map((r) => ({ label: r.label, title: r.title, value: r.stats[m.key] }))
  }))
)
</script>

<template>
  <div v-if="loader.error.value" class="error">{{ loader.error.value }}</div>
  <p v-if="!from && rows.length" class="muted small">Начало периода не задано — на графиках последние 14 дней.</p>
  <p v-if="loader.loading.value && !rows.length" class="muted">Строим графики…</p>
  <p v-else-if="!rows.length && !loader.error.value" class="muted">За этот период данных ещё нет.</p>
  <template v-if="rows.length">
    <div class="chart-grid" :class="{ stale: loader.loading.value }">
      <div v-for="c in charts" :key="c.key" class="chart-card">
        <BarChart :title="c.name" :points="c.points" />
      </div>
    </div>
    <details class="chart-data">
      <summary>Показать таблицей</summary>
      <div class="admin-table-wrap">
        <table v-stack class="admin-table">
          <thead>
            <tr><th>Период</th><th v-for="m in metrics" :key="m.key" class="num">{{ m.name }}</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.from">
              <td>{{ r.title }}</td>
              <td v-for="m in metrics" :key="m.key" class="num">{{ r.stats[m.key] }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </details>
  </template>
</template>
