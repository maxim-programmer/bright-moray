<script setup>
import { state, getCurrentUser } from '../store'
</script>

<template>
  <header class="header">
    <div class="header-inner">
      <router-link to="/" class="logo">Аукционы общежития</router-link>
      <nav class="nav">
        <router-link to="/">Главная</router-link>
        <template v-if="state.currentUserId">
          <router-link to="/profile">{{ getCurrentUser().username }}</router-link>
          <router-link to="/create" class="button small">Создать лот</router-link>
        </template>
        <template v-else>
          <router-link to="/login">Вход</router-link>
          <router-link to="/register">Регистрация</router-link>
        </template>
      </nav>
    </div>
  </header>
</template>
