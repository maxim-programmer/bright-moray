import { createRouter, createWebHistory } from 'vue-router'
import { state, initSession, onUnauthorized } from './store'
import HomeView from './views/HomeView.vue'
import CatalogView from './views/CatalogView.vue'
import LoginView from './views/LoginView.vue'
import RegisterView from './views/RegisterView.vue'
import CreateLotView from './views/CreateLotView.vue'
import LotView from './views/LotView.vue'
import ProfileView from './views/ProfileView.vue'
import SettingsView from './views/SettingsView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: HomeView },
    { path: '/lots', component: CatalogView },
    { path: '/lots/:id', component: LotView },
    { path: '/login', component: LoginView, meta: { guestOnly: true } },
    { path: '/register', component: RegisterView, meta: { guestOnly: true } },
    { path: '/create', component: CreateLotView, meta: { needAuth: true } },
    { path: '/profile', component: ProfileView, meta: { needAuth: true } },
    { path: '/settings', component: SettingsView, meta: { needAuth: true } },
    {
      // Панель администратора. Роль проверяет и сервер (каждый /api/admin/* — 403 для не-админа),
      // здесь только не показываем обычному пользователю пустые страницы.
      path: '/admin',
      component: () => import('./views/admin/AdminView.vue'),
      meta: { needAuth: true, needAdmin: true },
      children: [
        { path: '', component: () => import('./views/admin/AdminStats.vue') },
        { path: 'users', component: () => import('./views/admin/AdminUsers.vue') },
        { path: 'users/:id', component: () => import('./views/admin/AdminUserView.vue') },
        { path: 'lots', component: () => import('./views/admin/AdminLots.vue') },
        { path: 'lots/:id', component: () => import('./views/admin/AdminLotView.vue') },
        { path: 'auctions', component: () => import('./views/admin/AdminAuctions.vue') },
        { path: 'auctions/:id', component: () => import('./views/admin/AdminAuctionView.vue') },
        { path: 'events', component: () => import('./views/admin/AdminEvents.vue') },
        { path: 'settings', component: () => import('./views/admin/AdminSettings.vue') }
      ]
    },
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ],
  scrollBehavior(to, from, saved) {
    if (saved) return saved
    // при смене фильтров в каталоге не прыгаем наверх, при смене страницы — прыгаем
    if (to.path === from.path && to.query.page === from.query.page) return false
    return { top: 0 }
  }
})

// Ждём /auth/me: иначе после обновления страницы вошедшего пользователя выкинет на вход
router.beforeEach(async (to) => {
  await initSession()
  if (to.meta.needAuth && !state.user) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.meta.needAdmin && state.user.role !== 'admin') {
    return '/'
  }
  // уже вошедшему нечего делать на страницах входа и регистрации
  if (to.meta.guestOnly && state.user) {
    const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : '/'
    return redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/'
  }
})

onUnauthorized(() => {
  const current = router.currentRoute.value
  if (current.path === '/login') return
  router.push({ path: '/login', query: { redirect: current.fullPath } })
})

export default router
