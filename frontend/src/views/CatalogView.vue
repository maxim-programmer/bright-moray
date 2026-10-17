<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LotCard from '../components/LotCard.vue'
import Pagination from '../components/Pagination.vue'
import {
  categories,
  dorms,
  sortOptions,
  statusOptions,
  getCategory,
  getSubcategory,
  filterLots,
  sortLots,
  paginate,
  getRandomLotId,
  PAGE_SIZE
} from '../store'

const route = useRoute()
const router = useRouter()

// Все фильтры живут в адресной строке: /lots?cat=computers&sub=laptops&sort=price_asc&page=2
// Поэтому ссылку на подборку можно скопировать, а кнопка «Назад» работает.
const filters = computed(() => ({
  category: route.query.cat || '',
  sub: route.query.sub || '',
  q: route.query.q || '',
  status: route.query.status || 'active',
  priceMin: route.query.min || '',
  priceMax: route.query.max || '',
  dormId: route.query.dorm || ''
}))
const sort = computed(() => route.query.sort || 'ending')
const pageNumber = computed(() => Number(route.query.page) || 1)

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

/* --- Данные --- */
const filtered = computed(() => sortLots(filterLots(filters.value), sort.value))
const pageData = computed(() => paginate(filtered.value, pageNumber.value))

// количество лотов в категориях — с учётом всех фильтров, кроме самой категории
const countsBase = computed(() => filterLots({ ...filters.value, category: '', sub: '' }))
const counts = computed(() => {
  const result = {}
  for (const lot of countsBase.value) {
    result[lot.category] = (result[lot.category] || 0) + 1
    const key = lot.category + '/' + lot.subcategory
    result[key] = (result[key] || 0) + 1
  }
  return result
})
function countFor(categoryId, subId) {
  return counts.value[subId ? categoryId + '/' + subId : categoryId] || 0
}

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
const allExpanded = computed(() => expanded.size === categories.length)
function toggleAll() {
  if (allExpanded.value) expanded.clear()
  else categories.forEach((c) => expanded.add(c.id))
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
function openRandom() {
  const id = getRandomLotId(filtered.value)
  if (id) router.push('/lots/' + id)
  else noActiveLots.value = true
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
              <span class="cat-count">{{ countsBase.length }}</span>
            </button>
          </div>
        </li>
        <li v-for="cat in categories" :key="cat.id">
          <div class="cat-row">
            <button
              class="cat-item"
              :class="{
                selected: filters.category === cat.id && !filters.sub,
                'in-path': filters.category === cat.id && filters.sub,
                empty: countFor(cat.id) === 0
              }"
              @click="selectCategory(cat.id)"
            >
              <span class="cat-name">{{ cat.name }}</span>
              <span class="cat-count">{{ countFor(cat.id) }}</span>
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
                :class="{ selected: filters.category === cat.id && filters.sub === sub.id, empty: countFor(cat.id, sub.id) === 0 }"
                @click="selectCategory(cat.id, sub.id)"
              >
                <span>{{ sub.name }}</span>
                <span class="cat-count">{{ countFor(cat.id, sub.id) }}</span>
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
            Найдено: {{ filtered.length }}
            <template v-if="pageData.totalPages > 1">
              · страница {{ pageData.page }} из {{ pageData.totalPages }} · по {{ PAGE_SIZE }} на странице
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
            Все в разделе <span>{{ countFor(activeCategory.id) }}</span>
          </button>
          <button
            v-for="sub in activeCategory.subs"
            :key="sub.id"
            class="chip"
            :class="{ selected: filters.sub === sub.id }"
            :disabled="countFor(activeCategory.id, sub.id) === 0 && filters.sub !== sub.id"
            @click="selectCategory(activeCategory.id, sub.id)"
          >
            {{ sub.name }} <span>{{ countFor(activeCategory.id, sub.id) }}</span>
          </button>
        </template>
        <template v-else>
          <button
            v-for="cat in categories"
            :key="cat.id"
            class="chip"
            :disabled="countFor(cat.id) === 0"
            @click="selectCategory(cat.id)"
          >
            {{ cat.name }} <span>{{ countFor(cat.id) }}</span>
          </button>
        </template>
      </div>

      <div v-if="filtered.length === 0" class="empty">
        <p>По вашему запросу ничего не нашлось.</p>
        <button class="button light" @click="resetFilters">Сбросить фильтры</button>
      </div>

      <div v-else class="grid">
        <LotCard v-for="lot in pageData.items" :key="lot.id" :lot="lot" />
      </div>

      <Pagination :page="pageData.page" :total-pages="pageData.totalPages" @change="changePage" />
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
            v-for="dorm in dorms"
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
        Показать {{ filtered.length }}
      </button>
    </aside>

    <div
      v-if="showCategories || showFilters"
      class="overlay"
      @click="showCategories = showFilters = false"
    ></div>
  </div>
</template>
