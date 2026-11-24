<script setup>
import { ref, onMounted } from 'vue'

// Подтверждение критического действия, для которого причина не нужна.
defineProps({
  title: { type: String, required: true },
  hint: { type: String, default: '' },
  confirmText: { type: String, default: 'Подтвердить' },
  danger: { type: Boolean, default: false },
  busy: { type: Boolean, default: false },
  error: { type: String, default: '' }
})
const emit = defineEmits(['confirm', 'cancel'])

// Фокус на «Отмена»: случайный Enter не выполнит опасное действие
const cancelButton = ref(null)
onMounted(() => cancelButton.value.focus())
</script>

<template>
  <div class="modal-backdrop" @click.self="busy || emit('cancel')">
    <form class="modal" role="dialog" aria-modal="true" :aria-label="title" @submit.prevent="emit('confirm')" @keydown.esc="busy || emit('cancel')">
      <h2>{{ title }}</h2>
      <p v-if="hint" class="muted small">{{ hint }}</p>
      <div v-if="error" class="error">{{ error }}</div>
      <div class="modal-actions">
        <button ref="cancelButton" type="button" class="button light" :disabled="busy" @click="emit('cancel')">Отмена</button>
        <button type="submit" class="button" :class="{ danger }" :disabled="busy">{{ busy ? 'Подождите…' : confirmText }}</button>
      </div>
    </form>
  </div>
</template>
