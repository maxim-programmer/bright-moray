<script setup>
import { getPriceText, formatTimeLeft } from '../store'

defineProps({
  lot: Object
})
</script>

<template>
  <router-link :to="'/lots/' + lot.id" class="card">
    <img :src="lot.image" :alt="lot.title" />
    <div class="card-body">
      <div class="card-title">{{ lot.title }}</div>
      <div class="card-price">{{ getPriceText(lot) }}</div>
      <div class="muted">Осталось: {{ formatTimeLeft(lot.endsAt) }}</div>
    </div>
  </router-link>
</template>
