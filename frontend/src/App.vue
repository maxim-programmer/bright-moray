<script setup>
import AppHeader from './components/AppHeader.vue'
</script>

<template>
  <AppHeader />
  <main class="container">
    <router-view :key="$route.fullPath" />
  </main>
</template>
