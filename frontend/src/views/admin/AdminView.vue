<script setup>
// Каркас панели администратора: вкладки и место для вложенных страниц.
const tabs = [
  { to: '/admin', name: 'Статистика', exact: true },
  { to: '/admin/users', name: 'Пользователи' },
  { to: '/admin/lots', name: 'Модерация лотов' },
  { to: '/admin/auctions', name: 'Аукционы' },
  { to: '/admin/events', name: 'Журнал событий' },
  { to: '/admin/settings', name: 'Настройки' }
]
</script>

<template>
  <div class="admin">
    <h1>Панель администратора</h1>
    <nav class="admin-tabs" aria-label="Разделы панели администратора">
      <router-link
        v-for="tab in tabs"
        :key="tab.to"
        :to="tab.to"
        :active-class="tab.exact ? '' : 'active'"
        :exact-active-class="tab.exact ? 'active' : ''"
      >
        {{ tab.name }}
      </router-link>
    </nav>
    <router-view :key="$route.path" />
  </div>
</template>
