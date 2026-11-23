<script setup>
import { ref } from 'vue'
import { localDateTime } from '../../views/admin/format'

// Одобрение лота: начать аукцион сразу или в назначенное время.
defineProps({
  busy: { type: Boolean, default: false },
  error: { type: String, default: '' }
})
const emit = defineEmits(['confirm', 'cancel'])

const mode = ref('now') // now | later
const when = ref('')
const localError = ref('')

function submit() {
  if (mode.value === 'now') {
    emit('confirm', undefined)
    return
  }
  const ts = localDateTime(when.value)
  if (!ts || ts <= Date.now()) {
    localError.value = 'Укажите время начала в будущем'
    return
  }
  localError.value = ''
  emit('confirm', ts)
}
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('cancel')">
    <form class="modal" role="dialog" aria-modal="true" @submit.prevent="submit" @keydown.esc="emit('cancel')">
      <h2>Одобрить лот</h2>
      <label class="radio-row"><input v-model="mode" type="radio" value="now" /> Начать аукцион сразу</label>
      <label class="radio-row"><input v-model="mode" type="radio" value="later" /> Начать в назначенное время</label>
      <label v-if="mode === 'later'" class="field">
        Начало аукциона
        <input v-model="when" type="datetime-local" />
      </label>
      <p class="muted small">Длительность отсчитывается от фактического начала аукциона.</p>
      <div v-if="localError || error" class="error">{{ localError || error }}</div>
      <div class="modal-actions">
        <button type="button" class="button light" :disabled="busy" @click="emit('cancel')">Отмена</button>
        <button type="submit" class="button" :disabled="busy">{{ busy ? 'Подождите…' : 'Одобрить' }}</button>
      </div>
    </form>
  </div>
</template>
