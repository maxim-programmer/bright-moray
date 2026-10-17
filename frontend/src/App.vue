<script setup>
import AppHeader from './components/AppHeader.vue'
</script>

<template>
  <AppHeader />
  <main class="container" :class="{ wide: $route.path === '/lots' || $route.path === '/' }">
    <!-- ключ по path, а не fullPath: смена фильтров (query) не пересоздаёт страницу -->
    <router-view :key="$route.path" />
  </main>
</template>
