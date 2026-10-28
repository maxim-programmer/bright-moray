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
    { path: '/login', component: LoginView },
    { path: '/register', component: RegisterView },
    { path: '/create', component: CreateLotView, meta: { needAuth: true } },
    { path: '/profile', component: ProfileView, meta: { needAuth: true } },
    { path: '/settings', component: SettingsView, meta: { needAuth: true } },
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
  if (to.meta.needAuth && !state.currentUserId) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
})

onUnauthorized(() => {
  const current = router.currentRoute.value
  if (current.path === '/login') return
  router.push({ path: '/login', query: { redirect: current.fullPath } })
})

export default router
