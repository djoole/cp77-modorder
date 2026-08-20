<template>
  <div class="dnd-wrap">
    <div v-if="loading" class="text-dim" style="padding: 8px">Loading…</div>
    <draggable
      v-else
      v-model="localOrder"
      item-key="name"
      handle=".drag-handle"
      :animation="150"
      @start="onDragStart"
      @end="onReorder"
    >
      <template #item="{ element }">
        <div class="dnd-row" :class="rowClass(element)">
          <GripVertical class="drag-handle" :size="16" />
          <span class="dnd-name" :title="element">{{ truncate(element, 36) }}</span>
        </div>
      </template>
    </draggable>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import draggable from 'vuedraggable'
import { GripVertical } from 'lucide-vue-next'
import { useWails } from '../composables/useWails'
import { truncate } from '../utils'

const props = defineProps<{ anchorName: string }>()

const wails = useWails()
const localOrder = ref<string[]>([])
const loading = ref(true)
let draggedName: string | null = null

async function load() {
  loading.value = true
  try {
    const group = await wails.getConflictGroup(props.anchorName)
    localOrder.value = group.mods
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => props.anchorName, load)

function onDragStart(event: { oldIndex?: number }) {
  draggedName = event.oldIndex == null ? null : (localOrder.value[event.oldIndex] ?? null)
}

async function onReorder() {
  const movedName = draggedName
  draggedName = null
  if (!movedName) return
  await wails.reorderGroup(localOrder.value, movedName)
}

function rowClass(name: string): string {
  if (name === props.anchorName) return 'dnd-row--anchor'
  const anchorIdx = localOrder.value.indexOf(props.anchorName)
  const idx = localOrder.value.indexOf(name)
  return idx > anchorIdx ? 'dnd-row--losing' : 'dnd-row--conflict'
}
</script>

<style scoped>
.dnd-wrap {
  user-select: none;
}
.dnd-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--cp-border);
  border-left: 2px solid transparent;
  cursor: default;
  transition: background 0.1s;
}
.dnd-row--anchor {
  background: var(--cp-dnd-anchor);
  border-left-color: var(--cp-focus);
}
.dnd-row--conflict {
  border-left-color: var(--cp-error);
}
.dnd-row--conflict:hover {
  background: rgba(255, 51, 102, 0.06);
}
.dnd-row--losing {
  border-left-color: var(--cp-success);
}
.dnd-row--losing:hover {
  background: rgba(57, 255, 20, 0.05);
}
.drag-handle {
  color: var(--cp-dim);
  cursor: grab;
  flex-shrink: 0;
  transition:
    color 0.15s,
    text-shadow 0.15s;
}
.drag-handle:hover {
  color: var(--cp-focus);
  text-shadow: var(--glow-focus);
}
.drag-handle:active {
  cursor: grabbing;
  color: var(--cp-primary);
  text-shadow: var(--glow-primary);
}
.dnd-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dnd-row--conflict .dnd-name {
  color: var(--cp-error);
}
.dnd-row--losing .dnd-name {
  color: var(--cp-success);
}
.dnd-row--anchor .dnd-name {
  color: var(--cp-fg);
}
.sortable-drag .dnd-row {
  background: var(--cp-dnd-drag) !important;
  border-left-color: var(--cp-primary) !important;
  box-shadow: 0 0 16px rgba(240, 192, 0, 0.3) !important;
}
</style>
