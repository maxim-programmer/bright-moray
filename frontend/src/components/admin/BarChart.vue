<script setup>
import { ref, computed } from 'vue'
import { niceScale } from '../../views/admin/period'

// Столбчатый график одного показателя по времени (SVG, без библиотек).
const props = defineProps({
  title: { type: String, required: true },
  points: { type: Array, required: true } // [{ label, title, value }]
})

const W = 480
const H = 200
const PAD = { left: 40, right: 8, top: 14, bottom: 26 }
const plotW = W - PAD.left - PAD.right
const plotH = H - PAD.top - PAD.bottom

const scale = computed(() => niceScale(Math.max(0, ...props.points.map((p) => p.value))))
const ticks = computed(() => {
  const out = []
  for (let v = 0; v <= scale.value.top; v += scale.value.step) out.push({ v, y: y(v) })
  return out
})
const y = (v) => PAD.top + plotH - (v / scale.value.top) * plotH

const band = computed(() => plotW / Math.max(1, props.points.length))
const bars = computed(() => {
  const w = Math.min(24, band.value - 4)
  const every = Math.ceil(props.points.length / 7) // подписи не должны слипаться
  return props.points.map((p, i) => {
    const cx = PAD.left + band.value * (i + 0.5)
    const top = y(p.value)
    const h = PAD.top + plotH - top
    const r = Math.min(4, h, w / 2)
    const l = cx - w / 2
    const path = `M${l},${top + h} V${top + r} Q${l},${top} ${l + r},${top} H${l + w - r} Q${l + w},${top} ${l + w},${top + r} V${top + h} Z`
    return { ...p, cx, top, path, showLabel: i % every === 0 }
  })
})

const hover = ref(-1)
const tip = computed(() => bars.value[hover.value])
const num = (n) => n.toLocaleString('ru-RU')
</script>

<template>
  <figure class="bar-chart">
    <figcaption>{{ title }}</figcaption>
    <div class="bar-chart-plot" @mouseleave="hover = -1">
      <svg :viewBox="`0 0 ${W} ${H}`" role="group" :aria-label="title">
        <g class="grid">
          <template v-for="t in ticks" :key="t.v">
            <line :x1="PAD.left" :x2="W - PAD.right" :y1="t.y" :y2="t.y" />
            <text :x="PAD.left - 8" :y="t.y + 4" text-anchor="end">{{ num(t.v) }}</text>
          </template>
        </g>
        <g v-for="(b, i) in bars" :key="i">
          <path v-if="b.value" class="bar" :class="{ hovered: hover === i }" :d="b.path" />
          <text v-if="b.showLabel" :x="b.cx" :y="H - 8" text-anchor="middle">{{ b.label }}</text>
          <rect
            class="hit"
            :x="b.cx - band / 2"
            :y="PAD.top"
            :width="band"
            :height="plotH"
            tabindex="0"
            role="img"
            :aria-label="b.title + ': ' + num(b.value)"
            @mouseenter="hover = i"
            @click="hover = i"
            @focus="hover = i"
            @blur="hover = -1"
          />
        </g>
      </svg>
      <div
        v-if="tip"
        class="bar-chart-tip"
        :style="{ left: Math.min(80, Math.max(20, (tip.cx / W) * 100)) + '%', top: (tip.top / H) * 100 + '%' }"
      >
        <b>{{ num(tip.value) }}</b><span>{{ tip.title }}</span>
      </div>
    </div>
  </figure>
</template>
