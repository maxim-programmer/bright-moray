import { createRouter, createWebHistory } from 'vue-router'
import { state } from './store'
import HomeView from './views/HomeView.vue'
import LoginView from './views/LoginView.vue'
import RegisterView from './views/RegisterView.vue'
import CreateLotView from './views/CreateLotView.vue'
import LotView from './views/LotView.vue'
import ProfileView from './views/ProfileView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: HomeView },
    { path: '/login', component: LoginView },
    { path: '/register', component: RegisterView },
    { path: '/create', component: CreateLotView, meta: { needAuth: true } },
    { path: '/lots/:id', component: LotView },
    { path: '/profile', component: ProfileView, meta: { needAuth: true } }
  ]
})

router.beforeEach((to) => {
  if (to.meta.needAuth && !state.currentUserId) {
    return '/login'
  }
})

export default router
