<script setup>
import { getActiveLots } from '../store'
import LotCard from '../components/LotCard.vue'
</script>

<template>
  <h1>Активные аукционы</h1>
  <div v-if="getActiveLots().length === 0" class="muted">
    Сейчас нет активных аукционов. Создайте первый лот.
  </div>
  <div class="grid">
    <LotCard v-for="lot in getActiveLots()" :key="lot.id" :lot="lot" />
  </div>
</template>
