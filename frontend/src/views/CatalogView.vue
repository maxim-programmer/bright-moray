<script setup>
import { ref, reactive, computed, watch, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LotCard from '../components/LotCard.vue'
import Pagination from '../components/Pagination.vue'
import { api } from '../api'
import {
  state,
  sortOptions,
  statusOptions,
  getCategory,
  getSubcategory,
  handleApiError,
  PAGE_SIZE
} from '../store'

const route = useRoute()
const router = useRouter()

const statusIds = statusOptions.map((o) => o.id)
const sortIds = sortOptions.map((o) => o.id)

// Все фильтры живут в адресной строке: /lots?cat=computers&sub=laptops&sort=price_asc&page=2
// Поэтому ссылку на подборку можно скопировать, а кнопка «Назад» работает.
// Имена в адресе (cat, min, max, dorm) короче, чем в API, — маппим их в apiParams.
const filters = computed(() => ({
  category: route.query.cat || '',
  sub: route.query.sub || '',
  q: route.query.q || '',
  // неизвестный статус сервер отклонит с 400, поэтому старые и битые ссылки сводим к «Активные»
  status: statusIds.includes(route.query.status) ? route.query.status : 'active',
  priceMin: route.query.min || '',
  priceMax: route.query.max || '',
  dormId: route.query.dorm || ''
}))
const sort = computed(() => (sortIds.includes(route.query.sort) ? route.query.sort : 'ending'))
const pageNumber = computed(() => Number(route.query.page) || 1)

// сервер ждёт целые числа; пустые значения api.js сам выкинет из query
function toInt(value) {
  const n = Math.floor(Number(value))
  return Number.isFinite(n) && n > 0 ? n : ''
}

const apiParams = computed(() => ({
  category: filters.value.category,
  sub: filters.value.sub,
  q: filters.value.q.trim(),
  status: filters.value.status,
  sort: sort.value,
  priceMin: toInt(filters.value.priceMin),
  priceMax: toInt(filters.value.priceMax),
  dormId: toInt(filters.value.dormId),
  page: pageNumber.value > 1 ? pageNumber.value : ''
}))

function updateQuery(patch, { push = false, keepPage = false } = {}) {
  const next = { ...route.query, ...patch }
  if (!keepPage) delete next.page
  for (const key of Object.keys(next)) {
    if (next[key] === '' || next[key] === null || next[key] === undefined) delete next[key]
  }
  if (next.status === 'active') delete next.status
  if (next.sort === 'ending') delete next.sort
  if (next.page === 1 || next.page === '1') delete next.page
  router[push ? 'push' : 'replace']({ path: '/lots', query: next })
}

/* --- Текстовые поля применяем с небольшой задержкой, чтобы не дёргать список на каждую букву --- */
const search = ref(filters.value.q)
const priceMin = ref(filters.value.priceMin)
const priceMax = ref(filters.value.priceMax)
let timer = null
let sent = { q: filters.value.q, min: filters.value.priceMin, max: filters.value.priceMax }
watch([search, priceMin, priceMax], () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    const next = { q: search.value.trim(), min: String(priceMin.value), max: String(priceMax.value) }
    if (next.q === sent.q && next.min === sent.min && next.max === sent.max) return
    sent = next
    updateQuery(next)
  }, 350)
})
// если фильтры поменяли извне (кнопкой «Сбросить» или «Назад») — синхронизируем поля
watch(
  () => [route.query.q || '', route.query.min || '', route.query.max || ''],
  ([q, min, max]) => {
    if (q === sent.q && String(min) === sent.min && String(max) === sent.max) return
    clearTimeout(timer)
    sent = { q, min: String(min), max: String(max) }
    search.value = q
    priceMin.value = min
    priceMax.value = max
  }
)
onUnmounted(() => clearTimeout(timer))

/* --- Данные: фильтры, сортировку и страницы считает сервер --- */
const result = ref({ lots: [], page: 1, totalPages: 1, total: 0 })
const loading = ref(false)
const error = ref('')
let requestId = 0

async function load() {
  // при быстрой смене фильтров ответы могут прийти не по порядку — берём только последний
  const id = ++requestId
  loading.value = true
  error.value = ''
  try {
    const data = await api.lots(apiParams.value)
    if (id === requestId) result.value = data
  } catch (e) {
    if (id === requestId) error.value = handleApiError(e)
  } finally {
    if (id === requestId) loading.value = false
  }
}

watch(
  () => JSON.stringify(apiParams.value),
  () => {
    // при уходе со страницы query тоже меняется — лишний запрос не нужен
    if (route.path === '/lots') load()
  },
  { immediate: true }
)
onUnmounted(() => {
  requestId++ // ответ, пришедший после ухода со страницы, игнорируем
})

const activeCategory = computed(() => getCategory(filters.value.category))
const activeSub = computed(() => getSubcategory(filters.value.category, filters.value.sub))
const title = computed(() => {
  if (activeSub.value) return activeSub.value.name
  if (activeCategory.value) return activeCategory.value.name
  return 'Все лоты'
})

const hasExtraFilters = computed(
  () =>
    filters.value.q ||
    filters.value.priceMin ||
    filters.value.priceMax ||
    filters.value.dormId ||
    filters.value.status !== 'active' ||
    sort.value !== 'ending'
)

/* --- Раскрытие категорий в боковом меню --- */
const expanded = reactive(new Set(filters.value.category ? [filters.value.category] : []))
watch(
  () => filters.value.category,
  (cat) => {
    if (cat) expanded.add(cat)
  }
)
function toggle(catId) {
  if (expanded.has(catId)) expanded.delete(catId)
  else expanded.add(catId)
}
const allExpanded = computed(() => expanded.size === state.categories.length)
function toggleAll() {
  if (allExpanded.value) expanded.clear()
  else state.categories.forEach((c) => expanded.add(c.id))
}

/* --- Действия --- */
function selectCategory(catId, subId = '') {
  updateQuery({ cat: catId, sub: subId }, { push: true })
  showCategories.value = false
}

function changePage(p) {
  updateQuery({ page: p }, { push: true, keepPage: true })
}

function resetFilters() {
  router.push({ path: '/lots', query: { cat: route.query.cat, sub: route.query.sub } })
}

const noActiveLots = ref(false)
const randomError = ref('')
async function openRandom() {
  noActiveLots.value = false
  randomError.value = ''
  try {
    const { id } = await api.randomLot({ category: filters.value.category, sub: filters.value.sub })
    if (id) router.push('/lots/' + id)
    else noActiveLots.value = true
  } catch (e) {
    randomError.value = handleApiError(e)
  }
}

// На узких экранах боковые колонки сворачиваются в выезжающие панели
const showCategories = ref(false)
const showFilters = ref(false)
</script>

<template>
  <div class="catalog">
    <!-- ===== Левая колонка: дерево категорий ===== -->
    <aside class="side side-left" :class="{ open: showCategories }">
      <div class="side-head">
        <h3>Каталог</h3>
        <button class="link small" @click="toggleAll">
          {{ allExpanded ? 'Свернуть все' : 'Развернуть все' }}
        </button>
        <button class="side-close" @click="showCategories = false" aria-label="Закрыть">×</button>
      </div>
      <ul class="cat-list">
        <li>
          <div class="cat-row">
            <button class="cat-item" :class="{ selected: !filters.category }" @click="selectCategory('')">
              <span class="cat-name">Все категории</span>
            </button>
          </div>
        </li>
        <li v-for="cat in state.categories" :key="cat.id">
          <div class="cat-row">
            <button
              class="cat-item"
              :class="{
                selected: filters.category === cat.id && !filters.sub,
                'in-path': filters.category === cat.id && filters.sub
              }"
              @click="selectCategory(cat.id)"
            >
              <span class="cat-name">{{ cat.name }}</span>
            </button>
            <button
              class="cat-toggle"
              :class="{ open: expanded.has(cat.id) }"
              :aria-expanded="expanded.has(cat.id)"
              :aria-label="(expanded.has(cat.id) ? 'Свернуть ' : 'Развернуть ') + cat.name"
              @click="toggle(cat.id)"
            >
              <svg viewBox="0 0 12 12" width="12" height="12" aria-hidden="true">
                <path d="M3 4.5 6 7.5 9 4.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </button>
          </div>
          <ul v-if="expanded.has(cat.id)" class="sub-list">
            <li v-for="sub in cat.subs" :key="sub.id">
              <button
                class="sub-item"
                :class="{ selected: filters.category === cat.id && filters.sub === sub.id }"
                @click="selectCategory(cat.id, sub.id)"
              >
                <span>{{ sub.name }}</span>
              </button>
            </li>
          </ul>
        </li>
      </ul>
    </aside>

    <!-- ===== Центр: заголовок, быстрые фильтры по подкатегориям, карточки, страницы ===== -->
    <section class="catalog-main">
      <div class="catalog-head">
        <div>
          <div v-if="activeCategory" class="crumbs">
            <button class="link" @click="selectCategory('')">Все лоты</button>
            <span>›</span>
            <button v-if="activeSub" class="link" @click="selectCategory(activeCategory.id)">
              {{ activeCategory.name }}
            </button>
            <span v-else>{{ activeCategory.name }}</span>
            <template v-if="activeSub">
              <span>›</span><span>{{ activeSub.name }}</span>
            </template>
          </div>
          <h1>{{ title }}</h1>
          <p v-if="activeCategory && !activeSub" class="cat-description">{{ activeCategory.description }}</p>
          <div class="muted">
            Найдено: {{ result.total }}
            <template v-if="result.totalPages > 1">
              · страница {{ result.page }} из {{ result.totalPages }} · по {{ PAGE_SIZE }} на странице
            </template>
          </div>
        </div>
        <div class="mobile-toggles">
          <button class="button small light" @click="showCategories = true">Категории</button>
          <button class="button small light" @click="showFilters = true">Фильтры</button>
        </div>
      </div>

      <!-- Чипы: без категории — список категорий, внутри категории — её подкатегории -->
      <div class="chips">
        <template v-if="activeCategory">
          <button class="chip" :class="{ selected: !filters.sub }" @click="selectCategory(activeCategory.id)">
            Все в разделе
          </button>
          <button
            v-for="sub in activeCategory.subs"
            :key="sub.id"
            class="chip"
            :class="{ selected: filters.sub === sub.id }"
            @click="selectCategory(activeCategory.id, sub.id)"
          >
            {{ sub.name }}
          </button>
        </template>
        <template v-else>
          <button
            v-for="cat in state.categories"
            :key="cat.id"
            class="chip"
            @click="selectCategory(cat.id)"
          >
            {{ cat.name }}
          </button>
        </template>
      </div>

      <div v-if="loading" class="muted">Загрузка…</div>
      <div v-if="error" class="error">{{ error }}</div>

      <div v-if="!loading && !error && result.lots.length === 0" class="empty">
        <p>По вашему запросу ничего не нашлось.</p>
        <button class="button light" @click="resetFilters">Сбросить фильтры</button>
      </div>

      <div v-else-if="result.lots.length" class="grid">
        <LotCard v-for="lot in result.lots" :key="lot.id" :lot="lot" />
      </div>

      <Pagination :page="result.page" :total-pages="result.totalPages" @change="changePage" />
    </section>

    <!-- ===== Правая колонка: фильтры, сортировка, случайный лот ===== -->
    <aside class="side side-right" :class="{ open: showFilters }">
      <div class="side-head">
        <h3>Фильтры</h3>
        <button class="side-close" @click="showFilters = false" aria-label="Закрыть">×</button>
      </div>

      <button class="button random-btn" @click="openRandom">
        {{ activeCategory ? 'Случайный лот в разделе' : 'Случайный лот' }}
      </button>
      <div v-if="noActiveLots" class="muted small">Активных лотов пока нет</div>
      <div v-if="randomError" class="error">{{ randomError }}</div>

      <label class="field">
        Поиск
        <input v-model="search" type="search" placeholder="Название или описание" />
      </label>

      <label class="field">
        Сортировка
        <select :value="sort" @change="updateQuery({ sort: $event.target.value })">
          <option v-for="opt in sortOptions" :key="opt.id" :value="opt.id">{{ opt.name }}</option>
        </select>
      </label>

      <div class="field">
        Статус
        <div class="segmented">
          <button
            v-for="opt in statusOptions"
            :key="opt.id"
            :class="{ selected: filters.status === opt.id }"
            @click="updateQuery({ status: opt.id })"
          >
            {{ opt.name }}
          </button>
        </div>
      </div>

      <div class="field">
        Цена, ₽
        <div class="price-range">
          <input v-model="priceMin" type="number" min="0" placeholder="от" />
          <span>—</span>
          <input v-model="priceMax" type="number" min="0" placeholder="до" />
        </div>
      </div>

      <div class="field">
        Общежитие
        <div class="segmented">
          <button :class="{ selected: !filters.dormId }" @click="updateQuery({ dorm: '' })">Все</button>
          <button
            v-for="dorm in state.dorms"
            :key="dorm.id"
            :class="{ selected: String(filters.dormId) === String(dorm.id) }"
            @click="updateQuery({ dorm: dorm.id })"
          >
            {{ dorm.name }}
          </button>
        </div>
      </div>

      <button v-if="hasExtraFilters" class="button light small" @click="resetFilters">
        Сбросить фильтры
      </button>
      <button class="button small mobile-only" @click="showFilters = false">
        Показать {{ result.total }}
      </button>
    </aside>

    <div
      v-if="showCategories || showFilters"
      class="overlay"
      @click="showCategories = showFilters = false"
    ></div>
  </div>
</template>
