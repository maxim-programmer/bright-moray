<script setup>
import { ref, onMounted } from 'vue'
import { adminApi } from '../../api'
import { dayStart, dayEnd, formatAvg } from './format'
import { useLoader } from './useAdmin'

const from = ref('')
const to = ref('')
const stats = ref(null)

const loader = useLoader(() => adminApi.stats({ from: dayStart(from.value), to: dayEnd(to.value) }))

async function load() {
  const data = await loader.run()
  if (data) stats.value = data
}

function reset() {
  from.value = ''
  to.value = ''
  load()
}

onMounted(load)
</script>

<template>
  <div class="admin-bar">
    <label class="field">
      С даты
      <input v-model="from" type="date" @change="load" />
    </label>
    <label class="field">
      По дату (включительно)
      <input v-model="to" type="date" @change="load" />
    </label>
    <button class="button light small" type="button" @click="reset">Сбросить период</button>
  </div>

  <div v-if="loader.error.value" class="error">{{ loader.error.value }}</div>
  <div v-if="!stats && loader.loading.value" class="muted">Загрузка…</div>

  <template v-if="stats">
    <p v-if="stats.activeWindowDefault" class="muted small">
      Период не задан: показатели за всё время, а активные пользователи — за последние 30 суток.
    </p>

    <section class="admin-section">
      <h2>Пользователи</h2>
      <div class="stat-grid">
        <div class="stat-card"><div class="stat-value">{{ stats.usersTotal }}</div><div class="stat-label">Зарегистрировано</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.usersRegistered }}</div><div class="stat-label">Новых за период</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.usersActive }}</div><div class="stat-label">Активных</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.usersBlocked }}</div><div class="stat-label">Заблокировано сейчас</div></div>
      </div>
    </section>

    <section class="admin-section">
      <h2>Лоты и аукционы</h2>
      <div class="stat-grid">
        <div class="stat-card"><div class="stat-value">{{ stats.lotsCreated }}</div><div class="stat-label">Создано лотов</div></div>
        <div class="stat-card">
          <div class="stat-value">
            <router-link to="/admin/lots?status=pending" class="admin-link">{{ stats.lotsPending }}</router-link>
          </div>
          <div class="stat-label">Ждут модерации сейчас</div>
        </div>
        <div class="stat-card"><div class="stat-value">{{ stats.auctionsActive }}</div><div class="stat-label">Идут сейчас</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.auctionsFinished }}</div><div class="stat-label">Завершено</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.auctionsCancelled }}</div><div class="stat-label">Отменено</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.bidsTotal }}</div><div class="stat-label">Сделано ставок</div></div>
        <div class="stat-card"><div class="stat-value">{{ formatAvg(stats.avgBidsPerAuction) }}</div><div class="stat-label">Ставок на аукцион (в среднем)</div></div>
      </div>
    </section>

    <section class="admin-section">
      <h2>Самые востребованные категории</h2>
      <div class="admin-table-wrap">
        <div v-if="!stats.topCategories.length" class="admin-empty">За период нет данных.</div>
        <table v-else class="admin-table">
          <thead><tr><th>Категория</th><th class="num">Ставок</th><th class="num">Лотов</th></tr></thead>
          <tbody>
            <tr v-for="c in stats.topCategories" :key="c.id">
              <td>{{ c.name }}</td>
              <td class="num">{{ c.bids }}</td>
              <td class="num">{{ c.lots }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="admin-section">
      <h2>Самые активные пользователи</h2>
      <div class="admin-table-wrap">
        <div v-if="!stats.topUsers.length" class="admin-empty">За период нет данных.</div>
        <table v-else class="admin-table">
          <thead><tr><th>Пользователь</th><th class="num">Лотов</th><th class="num">Ставок</th><th class="num">Всего</th></tr></thead>
          <tbody>
            <tr v-for="u in stats.topUsers" :key="u.id">
              <td>
                <router-link :to="'/admin/users/' + u.id" class="admin-link">{{ u.username }}</router-link>
                <span class="muted small"> {{ u.name }}</span>
              </td>
              <td class="num">{{ u.lots }}</td>
              <td class="num">{{ u.bids }}</td>
              <td class="num"><b>{{ u.score }}</b></td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </template>
</template>
