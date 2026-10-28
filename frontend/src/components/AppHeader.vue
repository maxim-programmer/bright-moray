<script setup>
import { state } from '../store'
</script>

<template>
  <header class="header">
    <div class="header-inner">
      <router-link to="/" class="logo">Аукционы общежития</router-link>
      <nav class="nav">
        <router-link to="/" class="nav-link" exact-active-class="active">Главная</router-link>
        <router-link to="/lots" class="nav-link" exact-active-class="active">Лоты</router-link>
        <router-link to="/create" class="nav-link" active-class="active">Создать лот</router-link>
        <router-link
          v-if="state.user"
          to="/profile"
          class="nav-link"
          :class="{ active: $route.path === '/profile' || $route.path === '/settings' }"
        >
          Профиль<span class="nav-user">· {{ state.user.username }}</span>
        </router-link>
      </nav>
      <div class="nav-auth">
        <template v-if="!state.user">
          <router-link to="/login" class="nav-link">Вход</router-link>
          <router-link to="/register" class="button small outline-white">Регистрация</router-link>
        </template>
      </div>
    </div>
  </header>
</template>
