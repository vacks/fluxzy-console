import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './style.css'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import { useAuthStore } from './stores/auth'
import { useThemeStore } from './stores/theme'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
// Instantiate before mount so the stored light/dark/system choice is applied
// before the first frame, avoiding a flash of the wrong theme.
useThemeStore(pinia)
app.use(router)
app.use(i18n)

window.addEventListener('fc:unauthorized', () => {
  const auth = useAuthStore()
  auth.reset()
  if (router.currentRoute.value.name !== 'login') {
    router.push({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } })
  }
})

// A console rebuild replaces hashed lazy chunks. If the browser still runs an
// older index.html, a route chunk request can 404; reload once to pick up the
// new bundle instead of leaving the user on a dead page.
window.addEventListener('vite:preloadError', (event) => {
  event.preventDefault()
  const key = 'fluxzy-console:preload-reload-at'
  const now = Date.now()
  const last = Number(sessionStorage.getItem(key) || 0)
  if (now - last < 15_000) return
  sessionStorage.setItem(key, String(now))
  window.location.reload()
})

app.config.errorHandler = (err) => {
  // Keep the console useful without leaking stack traces to the UI.
  console.error('[fluxzy-console]', err)
}

app.mount('#app')
