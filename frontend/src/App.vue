<script setup>
import AppHeader from './components/AppHeader.vue'
import { state } from './store'
</script>

<template>
  <AppHeader />
  <main class="container" :class="{ wide: $route.path === '/lots' || $route.path === '/' }">
    <div v-if="state.initError" class="error">{{ state.initError }}</div>
    <!-- ключ по path, а не fullPath: смена фильтров (query) не пересоздаёт страницу -->
    <router-view :key="$route.path" />
  </main>
</template>
