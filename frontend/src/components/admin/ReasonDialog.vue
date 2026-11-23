<script setup>
import { ref, watch } from 'vue'

// Окно подтверждения действия с причиной (отказ, снятие, отмена, блокировка…).
const props = defineProps({
  title: { type: String, required: true },
  hint: { type: String, default: '' },
  label: { type: String, default: 'Причина' },
  confirmText: { type: String, default: 'Подтвердить' },
  required: { type: Boolean, default: true },
  danger: { type: Boolean, default: false },
  busy: { type: Boolean, default: false },
  error: { type: String, default: '' }
})
const emit = defineEmits(['confirm', 'cancel'])

const reason = ref('')
const localError = ref('')
watch(
  () => props.title,
  () => {
    reason.value = ''
    localError.value = ''
  }
)

function submit() {
  const text = reason.value.trim()
  if (props.required && !text) {
    localError.value = 'Укажите причину'
    return
  }
  localError.value = ''
  emit('confirm', text)
}
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('cancel')">
    <form class="modal" role="dialog" aria-modal="true" @submit.prevent="submit" @keydown.esc="emit('cancel')">
      <h2>{{ title }}</h2>
      <p v-if="hint" class="muted small">{{ hint }}</p>
      <label class="field">
        {{ label }}<span v-if="!required" class="muted"> (необязательно)</span>
        <textarea v-model="reason" rows="3" maxlength="1000" autofocus></textarea>
      </label>
      <div v-if="localError || error" class="error">{{ localError || error }}</div>
      <div class="modal-actions">
        <button type="button" class="button light" :disabled="busy" @click="emit('cancel')">Отмена</button>
        <button type="submit" class="button" :class="{ danger }" :disabled="busy">
          {{ busy ? 'Подождите…' : confirmText }}
        </button>
      </div>
    </form>
  </div>
</template>
