<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import ReasonDialog from '../../components/admin/ReasonDialog.vue'
import { vStack } from '../../components/admin/stackTable'
import { adminApi } from '../../api'
import { state, formatDate, formatMoney, getCategory } from '../../store'
import { adminStatusText, statusClass } from '../../status'
import { AUCTION_DIALOGS } from './auctionDialogs'
import { useLoader, useAction } from './useAdmin'

const route = useRoute()
const id = computed(() => Number(route.params.id))

const detail = ref(null)
const notFound = ref(false)
const loader = useLoader(() => adminApi.auction(id.value))

async function load() {
  const data = await loader.run()
  if (data) detail.value = data
  else if (loader.status.value === 404) notFound.value = true
}

const lot = computed(() => detail.value?.lot)
// идёт ли ещё торг (с учётом часов сервера)
const running = computed(() => lot.value && lot.value.status === 'active' && lot.value.endsAt > state.now && !detail.value.closed)
// завершён, но не отменён: по такому аукциону можно разбирать споры
const finished = computed(() => lot.value && detail.value.closed && lot.value.status !== 'cancelled')

// у отменённого аукциона победителя нет
const cancelled = computed(() => lot.value?.status === 'cancelled')

/* --- действия --- */

const dialog = ref(null) // { kind: finish | cancel | dismiss | annul | remove, bid? }
const action = useAction()

function open(kind, bid) {
  action.error.value = ''
  dialog.value = { kind, bid }
}

async function confirm(reason) {
  const { kind, bid } = dialog.value
  const calls = {
    finish: () => adminApi.finishAuction(id.value, reason),
    cancel: () => adminApi.cancelAuction(id.value, reason),
    dismiss: () => adminApi.resolveDispute(id.value, { action: 'dismiss', reason }),
    annul: () => adminApi.resolveDispute(id.value, { action: 'annul', reason }),
    remove: () => adminApi.resolveDispute(id.value, { action: 'remove_bid', bidId: bid?.id, reason })
  }
  const result = await action.exec(calls[kind])
  if (result) {
    dialog.value = null
    if (result !== true) detail.value = result // сервер вернул обновлённый аукцион
    else await load()
  }
}

const dlg = computed(() => (dialog.value ? AUCTION_DIALOGS[dialog.value.kind] : null))

// Срок вышел, пока страница открыта: перечитываем итог (аукцион могли и продлить)
let timer
watch(running, (now, before) => {
  if (before && !now && !detail.value.closed) timer = setTimeout(load, 1500)
})
onUnmounted(() => clearTimeout(timer))

onMounted(load)
</script>

<template>
  <router-link to="/admin/auctions" class="admin-link">← К аукционам</router-link>

  <div v-if="notFound" class="admin-card"><h2>Аукцион не найден</h2></div>
  <div v-else-if="loader.error.value && !detail" class="error">{{ loader.error.value }}</div>
  <div v-else-if="!detail" class="muted">Загрузка…</div>

  <template v-else>
    <div class="admin-card">
      <h2>
        {{ lot.title }}
        <span class="pill" :class="statusClass(lot, state.now)">{{ adminStatusText(lot, state.now) }}</span>
      </h2>
      <dl class="kv">
        <dt>Лот</dt><dd><router-link :to="'/admin/lots/' + lot.id" class="admin-link">№{{ lot.id }} — карточка и история изменений</router-link></dd>
        <dt>Категория</dt><dd>{{ getCategory(lot.category)?.name || lot.category }}</dd>
        <dt>Стартовая цена / шаг</dt><dd>{{ formatMoney(lot.startPrice) }} / {{ formatMoney(lot.step) }}</dd>
        <dt>Начало</dt><dd>{{ lot.startsAt ? formatDate(lot.startsAt) : '—' }}</dd>
        <dt>{{ detail.closed ? 'Окончание' : 'Окончание (расчётное)' }}</dt><dd>{{ formatDate(lot.endsAt) }}</dd>
        <dt>Продавец</dt>
        <dd v-if="detail.author">
          <router-link :to="'/admin/users/' + detail.author.id" class="admin-link">{{ detail.author.username }}</router-link>
          · {{ detail.author.name }}<template v-if="detail.author.telegram"> · @{{ detail.author.telegram }}</template>
        </dd>
        <dd v-else>—</dd>
        <dt>{{ cancelled ? 'Лидировал до отмены' : detail.closed ? 'Победитель' : 'Лидирует' }}</dt>
        <dd v-if="detail.winner">
          <router-link :to="'/admin/users/' + detail.winner.id" class="admin-link">{{ detail.winner.username }}</router-link>
          · {{ detail.winner.name }}<template v-if="detail.winner.telegram"> · @{{ detail.winner.telegram }}</template>
        </dd>
        <dd v-else>ставок нет</dd>
        <dt>{{ cancelled ? 'Цена на момент отмены' : detail.closed ? 'Итоговая стоимость' : 'Текущая цена' }}</dt>
        <dd>{{ detail.finalPrice != null ? formatMoney(detail.finalPrice) : '—' }}</dd>
        <template v-if="lot.moderationReason">
          <dt>Комментарий администратора</dt><dd>{{ lot.moderationReason }}</dd>
        </template>
      </dl>

      <div v-if="action.error.value && !dialog" class="error">{{ action.error.value }}</div>
      <div class="admin-actions">
        <template v-if="running">
          <button class="button" type="button" @click="open('finish')">Завершить досрочно</button>
          <button class="button danger" type="button" @click="open('cancel')">Отменить аукцион</button>
        </template>
        <template v-if="finished">
          <button class="button light" type="button" @click="open('dismiss')">Спор: оставить результат</button>
          <button class="button danger" type="button" @click="open('annul')">Аннулировать результат</button>
        </template>
        <router-link :to="{ path: '/admin/events', query: { lotId: lot.id } }" class="button ghost">Журнал по лоту</router-link>
      </div>
      <p v-if="finished && detail.bids.length" class="muted small">
        Спорную ставку можно удалить в таблице ниже — победитель и цена пересчитаются.
      </p>
    </div>

    <section class="admin-section">
      <h2>История ставок <span class="muted">({{ detail.bids.length }})</span></h2>
      <div class="admin-table-wrap">
        <div v-if="!detail.bids.length" class="admin-empty">Ставок нет.</div>
        <table v-else v-stack class="admin-table">
          <thead><tr><th>Время</th><th>Пользователь</th><th class="num">Сумма</th><th></th></tr></thead>
          <tbody>
            <tr v-for="(b, i) in detail.bids" :key="b.id">
              <td class="nowrap">{{ formatDate(b.createdAt) }}</td>
              <td>
                <router-link :to="'/admin/users/' + b.userId" class="admin-link">{{ b.username }}</router-link>
                <span v-if="i === 0 && !cancelled" class="pill good">{{ detail.closed ? 'победитель' : 'лидер' }}</span>
              </td>
              <td class="num"><b>{{ formatMoney(b.amount) }}</b></td>
              <td>
                <button v-if="finished" class="button small danger" type="button" @click="open('remove', b)">Удалить ставку</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </template>

  <ReasonDialog
    v-if="dialog"
    :title="dlg.title + (dialog.bid ? ' (' + formatMoney(dialog.bid.amount) + ', ' + dialog.bid.username + ')' : '')"
    :hint="dlg.hint"
    :confirm-text="dlg.confirm"
    :required="dlg.required"
    :danger="dlg.danger"
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="confirm"
    @cancel="dialog = null"
  />
</template>
