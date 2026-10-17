<script setup>
import {
  getPriceText,
  formatTimeShort,
  getStatus,
  getStatusText,
  getBidsCount,
  getCategory,
  getSubcategory,
  isEndingSoon
} from '../store'

defineProps({
  lot: { type: Object, required: true }
})
</script>

<template>
  <router-link :to="'/lots/' + lot.id" class="card">
    <div class="card-image">
      <img :src="lot.image" :alt="lot.title" loading="lazy" />
      <span v-if="getStatus(lot) !== 'active'" class="badge" :class="getStatus(lot)">
        {{ getStatusText(lot) }}
      </span>
      <span v-else-if="isEndingSoon(lot)" class="badge hot">Скоро конец</span>
    </div>
    <div class="card-body">
      <div class="card-category">
        {{ getCategory(lot.category)?.name }}<template v-if="getSubcategory(lot.category, lot.subcategory)">
          · {{ getSubcategory(lot.category, lot.subcategory).name }}</template>
      </div>
      <div class="card-title">{{ lot.title }}</div>
      <div class="card-price">{{ getPriceText(lot) }}</div>
      <div class="card-meta">
        <span>{{ getBidsCount(lot) }} ст.</span>
        <span v-if="getStatus(lot) === 'active'">ещё {{ formatTimeShort(lot.endsAt) }}</span>
      </div>
    </div>
  </router-link>
</template>
