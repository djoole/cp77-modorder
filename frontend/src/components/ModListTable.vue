<template>
  <div class="table-outer">
    <div class="table-controls">
      <input v-model="searchQuery" class="search-input" type="search" placeholder="Search archive…" />
      <span class="selection-hint">Shift+click: select range</span>
      <label class="filter-toggle">
        <input type="checkbox" v-model="showLosingOnly" />
        Display only conflicts
      </label>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="width: 24px"></th>
            <th style="width: 32px">#</th>
            <th style="width: 390px">Mod</th>
            <th style="width: 55px">Files</th>
            <th style="width: 80px">Conflicts</th>
            <th style="width: 80px">W / L</th>
            <th style="width: 65px">Status</th>
          </tr>
        </thead>
        <draggable
          v-if="!isFiltered"
          v-model="draggableRows"
          tag="tbody"
          :item-key="(el: RowEntry) => el.row.name"
          handle=".drag-handle"
          :animation="150"
          @start="onDragStart"
          @end="onReorder"
        >
          <template #item="{ element: { row, idx } }">
            <tr :class="rowClass(row, idx)" @click="onRowClick(row, idx, $event)">
              <td><GripVertical class="drag-handle" :class="{ 'drag-handle--hidden': row.missing }" :size="14" /></td>
              <td>{{ row.missing ? '—' : idx + 1 }}</td>
              <td :title="row.name">{{ truncate(row.name, 43) }}</td>
              <td>{{ row.mod ? row.mod.fileCount : '—' }}</td>
              <td :class="row.mod && row.mod.conflictCount > 0 ? 'text-error' : ''">
                {{ row.mod ? row.mod.conflictCount : '—' }}
              </td>
              <td v-if="row.mod">
                <span class="text-success">{{ row.mod.wins }}</span>
                {{ ' / ' }}
                <span class="text-error">{{ row.mod.losses }}</span>
              </td>
              <td v-else>—</td>
              <td>
                <span v-if="row.missing" class="badge badge-missing">MISSING</span>
                <span v-else-if="row.unlisted" class="badge badge-new">NEW</span>
              </td>
            </tr>
          </template>
        </draggable>
        <tbody v-else>
          <tr
            v-for="{ row, idx } in filteredRows"
            :key="row.name"
            :class="rowClass(row, idx)"
            @click="onRowClick(row, idx, $event)"
          >
            <td><GripVertical class="drag-handle drag-handle--hidden" :size="14" /></td>
            <td>{{ row.missing ? '—' : idx + 1 }}</td>
            <td :title="row.name">{{ truncate(row.name, 43) }}</td>
            <td>{{ row.mod ? row.mod.fileCount : '—' }}</td>
            <td :class="row.mod && row.mod.conflictCount > 0 ? 'text-error' : ''">
              {{ row.mod ? row.mod.conflictCount : '—' }}
            </td>
            <td v-if="row.mod">
              <span class="text-success">{{ row.mod.wins }}</span>
              {{ ' / ' }}
              <span class="text-error">{{ row.mod.losses }}</span>
            </td>
            <td v-else>—</td>
            <td>
              <span v-if="row.missing" class="badge badge-missing">MISSING</span>
              <span v-else-if="row.unlisted" class="badge badge-new">NEW</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import draggable from 'vuedraggable'
import { GripVertical } from 'lucide-vue-next'
import type { main } from '../../wailsjs/go/models'
import { truncate } from '../utils'
import { useWails } from '../composables/useWails'

type RowEntry = { row: main.DisplayRowDTO; idx: number }

const searchQuery = ref('')
const showLosingOnly = ref(false)

const props = defineProps<{
  rows: main.DisplayRowDTO[]
  selectedIndex: number
}>()
const emit = defineEmits<{ select: [idx: number] }>()

const wails = useWails()

const draggableRows = ref<RowEntry[]>([])
const selectedNames = ref<string[]>([])
const selectionAnchorName = ref<string | null>(null)
let dragState: {
  originalRows: RowEntry[]
  selectedNames: string[]
  draggedName: string
} | null = null

watch(
  () => props.rows,
  (rows) => {
    draggableRows.value = rows.map((row, idx) => ({ row, idx }))

    const validNames = new Set(rows.map((row) => row.name))
    selectedNames.value = selectedNames.value.filter((name) => validNames.has(name))
    if (selectionAnchorName.value && !validNames.has(selectionAnchorName.value)) {
      selectionAnchorName.value = null
    }

    // External operations can reorder rows. Never retain a scattered selection:
    // collapse it to the primary row if the selected names are no longer contiguous.
    const selectedIndexes = selectedNames.value
      .map((name) => rows.findIndex((row) => row.name === name))
      .filter((idx) => idx >= 0)
      .sort((a, b) => a - b)
    const isContiguous =
      selectedIndexes.length < 2 ||
      selectedIndexes[selectedIndexes.length - 1] - selectedIndexes[0] + 1 === selectedIndexes.length
    if (!isContiguous) {
      const primaryName = props.selectedIndex >= 0 ? rows[props.selectedIndex]?.name : null
      selectedNames.value = primaryName ? [primaryName] : []
      selectionAnchorName.value = primaryName ?? null
    }
  },
  { immediate: true }
)

watch(
  () => props.selectedIndex,
  (idx) => {
    if (idx < 0) {
      selectedNames.value = []
      selectionAnchorName.value = null
    }
  }
)

const isFiltered = computed(() => searchQuery.value !== '' || showLosingOnly.value)

const filteredRows = computed(() => {
  const q = searchQuery.value.toLowerCase()
  return draggableRows.value.filter(({ row }) => {
    if (q && !row.name.toLowerCase().includes(q)) return false
    if (showLosingOnly.value && (row.mod?.losses ?? 0) === 0) return false
    return true
  })
})

function onRowClick(row: main.DisplayRowDTO, idx: number, event: MouseEvent) {
  if (row.missing) return

  const clickedHandle = (event.target as HTMLElement).closest('.drag-handle')
  if (!event.shiftKey && clickedHandle && selectedNames.value.includes(row.name)) {
    // Sortable may emit a click after dropping. Keep the block selected.
    emit('select', idx)
    return
  }

  if (event.shiftKey && selectionAnchorName.value) {
    const anchorIdx = draggableRows.value.findIndex((entry) => entry.row.name === selectionAnchorName.value)
    const clickedIdx = draggableRows.value.findIndex((entry) => entry.row.name === row.name)
    if (anchorIdx >= 0 && clickedIdx >= 0) {
      const start = Math.min(anchorIdx, clickedIdx)
      const end = Math.max(anchorIdx, clickedIdx)
      selectedNames.value = draggableRows.value.slice(start, end + 1).map((entry) => entry.row.name)
    }
  } else {
    selectedNames.value = [row.name]
    selectionAnchorName.value = row.name
  }

  emit('select', idx)
}

function onDragStart(event: { oldIndex?: number }) {
  const oldIndex = event.oldIndex
  if (oldIndex == null) return

  const dragged = draggableRows.value[oldIndex]
  if (!dragged) return

  if (!selectedNames.value.includes(dragged.row.name)) {
    selectedNames.value = [dragged.row.name]
    selectionAnchorName.value = dragged.row.name
    emit('select', dragged.idx)
  }

  dragState = {
    originalRows: [...draggableRows.value],
    selectedNames: [...selectedNames.value],
    draggedName: dragged.row.name,
  }
}

async function onReorder() {
  if (isFiltered.value) return

  if (dragState && dragState.selectedNames.length > 1) {
    const selectedSet = new Set(dragState.selectedNames)
    const selectedBlock = dragState.originalRows.filter((entry) => selectedSet.has(entry.row.name))
    const draggedIdx = draggableRows.value.findIndex((entry) => entry.row.name === dragState?.draggedName)

    // vuedraggable has moved only the grabbed row. Count the unselected rows
    // before it to turn that drop location into an insertion point for the block.
    const insertionIdx = draggableRows.value
      .slice(0, Math.max(0, draggedIdx))
      .filter((entry) => !selectedSet.has(entry.row.name)).length
    const remaining = draggableRows.value.filter((entry) => !selectedSet.has(entry.row.name))
    remaining.splice(insertionIdx, 0, ...selectedBlock)
    draggableRows.value = remaining.map((entry, idx) => ({ ...entry, idx }))
  }

  dragState = null
  await wails.setModlistOrder(draggableRows.value.map((e) => e.row.name))
}

function rowClass(row: main.DisplayRowDTO, idx: number) {
  const losses = row.mod?.losses ?? 0
  const fileCount = row.mod?.fileCount ?? 0
  const fullyOverridden = fileCount > 0 && losses >= fileCount
  return {
    'row-selected': selectedNames.value.includes(row.name),
    'row-missing': row.missing,
    'row-new': row.unlisted && !row.missing,
    'row-fully-overridden': !row.missing && !row.unlisted && fullyOverridden,
    'row-conflict': !row.missing && !row.unlisted && losses > 0 && !fullyOverridden,
    'row-clickable': !row.missing,
  }
}
</script>

<style scoped>
.table-outer {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.table-wrap {
  flex: 1;
  overflow-y: auto;
}
.table-controls {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 8px;
  background: #0d0d0d;
  border-bottom: 1px solid var(--cp-border);
  flex-shrink: 0;
}
.search-input {
  flex: 1;
  background: var(--cp-input);
  color: var(--cp-fg);
  border: 1px solid var(--cp-border);
  border-radius: 0;
  padding: 3px 7px;
  font-size: 12px;
  outline: none;
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.search-input:focus {
  border-color: var(--cp-focus);
  box-shadow: var(--glow-focus);
}
.filter-toggle {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--cp-dim);
  cursor: pointer;
  white-space: nowrap;
  text-transform: uppercase;
  letter-spacing: 0.4px;
}
.selection-hint {
  color: var(--cp-dim);
  font-size: 10px;
  letter-spacing: 0.3px;
  text-transform: uppercase;
  white-space: nowrap;
}
table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
}
thead tr {
  background: #0d0d0d;
  border-bottom: 2px solid var(--cp-primary);
  position: sticky;
  top: 0;
}
th,
td {
  padding: 4px 6px;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  border-bottom: 1px solid var(--cp-border);
}
th {
  font-weight: 700;
  font-size: 11px;
  color: var(--cp-primary);
  text-transform: uppercase;
  letter-spacing: 0.6px;
  text-shadow: var(--glow-primary);
}

.drag-handle {
  color: var(--cp-dim);
  cursor: grab;
  display: block;
  transition: color 0.15s;
}
.drag-handle:hover {
  color: var(--cp-focus);
}
.drag-handle:active {
  cursor: grabbing;
  color: var(--cp-primary);
}
.drag-handle--hidden {
  visibility: hidden;
  cursor: default;
}

.row-clickable {
  cursor: pointer;
}
.row-clickable:hover td {
  background: rgba(255, 255, 255, 0.03);
}
.row-selected td {
  background: var(--cp-row-selected) !important;
}
.row-selected td:first-child {
  border-left: 2px solid var(--cp-focus) !important;
}
.row-fully-overridden td {
  color: var(--cp-dim);
}
.row-fully-overridden td:first-child {
  border-left: 2px solid var(--cp-dim);
}
.row-conflict td {
  background: var(--cp-row-conflict);
}
.row-conflict td:first-child {
  border-left: 2px solid var(--cp-error);
}
.row-missing td {
  background: var(--cp-row-missing);
  color: var(--cp-dim);
  cursor: default;
}
.row-new td {
  background: var(--cp-row-new);
}
.row-new td:first-child {
  border-left: 2px solid var(--cp-primary);
}

.badge {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 0;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.badge-missing {
  background: rgba(255, 51, 102, 0.2);
  color: var(--cp-error);
  border: 1px solid var(--cp-error);
  box-shadow: var(--glow-error);
}
.badge-new {
  background: rgba(240, 192, 0, 0.15);
  color: var(--cp-primary);
  border: 1px solid var(--cp-primary);
  box-shadow: var(--glow-primary);
}
</style>
