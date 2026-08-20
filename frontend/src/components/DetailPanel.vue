<template>
  <div class="detail">
    <div class="detail-name">{{ mod.name }}</div>

    <div class="detail-stats">
      <div class="stat-row">
        <span class="stat-label">Files</span><span>{{ mod.fileCount }}</span>
      </div>
      <div class="stat-row">
        <span class="stat-label">Conflicting files</span><span>{{ mod.conflictCount }}</span>
      </div>
      <div class="stat-row">
        <span class="stat-label">Wins</span><span class="text-success">{{ mod.wins }}</span>
      </div>
      <div class="stat-row">
        <span class="stat-label">Losses</span><span class="text-error">{{ mod.losses }}</span>
      </div>
      <!-- <div class="stat-row"><span class="stat-label">Priority</span><PriorityBadge :priority="mod.priority" /></div> -->
    </div>

    <!-- priority actions hidden until reworked
    <div class="detail-actions">
      <button @click="openPrioForm">Set Priority</button>
      <button @click="clearPriority" :disabled="mod.priority === 0">Clear Priority</button>
    </div>
    -->

    <div v-if="mod.conflictsWith?.length" class="detail-section section-conflicts">
      <div class="section-title section-title--conflict">Conflicts</div>
      <div class="conflict-list">
        <div v-for="entry in groupedConflicts" :key="entry.opponent" class="conflict-group">
          <button
            type="button"
            class="conflict-entry"
            :aria-expanded="isExpanded(entry.opponent)"
            @click="toggleConflict(entry.opponent)"
          >
            <span class="conflict-opponent text-error">
              <ChevronDown v-if="isExpanded(entry.opponent)" :size="13" />
              <ChevronRight v-else :size="13" />
              <span>{{ entry.opponent }}</span>
            </span>
            <span class="conflict-count">{{ entry.resources.length }}</span>
          </button>
          <div v-if="isExpanded(entry.opponent)" class="conflict-resources">
            <div v-for="(resource, index) in entry.resources" :key="resource" class="conflict-resource">
              <span class="resource-index">{{ index + 1 }}</span>
              <span class="resource-path" :title="resource">{{ resource }}</span>
            </div>
          </div>
        </div>
        <div v-if="mod.hasMore" class="text-dim" style="padding: 4px 0">…and {{ mod.moreCount }} more</div>
      </div>
    </div>

    <div v-if="mod.conflictCount > 0" class="detail-section section-loadorder">
      <div class="section-title section-title--loadorder">Load Order — drag to reorder</div>
      <LoadOrderDnd :anchor-name="mod.name" />
    </div>

    <!-- priority form hidden until reworked
    <dialog ref="prioDialog" class="prio-dialog">
      <div class="dialog-header">Set Priority — {{ mod.name }}</div>
      <div class="dialog-body">
        <input ref="prioInput" type="text" v-model="prioText"
          placeholder="1–99, 0 = unset" @keydown.enter="applyPriority" />
      </div>
      <div class="dialog-footer">
        <button @click="prioDialog!.close()">Cancel</button>
        <button class="primary" @click="applyPriority">Apply</button>
      </div>
    </dialog>
    -->
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import type { main } from '../../wailsjs/go/models'
import { useWails } from '../composables/useWails'
import PriorityBadge from './PriorityBadge.vue'
import LoadOrderDnd from './LoadOrderDnd.vue'

const props = defineProps<{
  mod: main.ModDTO
  selectedIndex: number
  rows: main.DisplayRowDTO[]
}>()

const wails = useWails()

const prioDialog = ref<HTMLDialogElement>()
const prioInput = ref<HTMLInputElement>()
const prioText = ref('')
const expandedOpponents = ref(new Set<string>())

const groupedConflicts = computed(() => {
  const map = new Map<string, Set<string>>()
  for (const pair of props.mod.conflictsWith ?? []) {
    let resources = map.get(pair.opponent)
    if (!resources) {
      resources = new Set<string>()
      map.set(pair.opponent, resources)
    }
    resources.add(pair.resource)
  }
  return Array.from(map.entries()).map(([opponent, resources]) => ({
    opponent,
    resources: Array.from(resources),
  }))
})

watch(
  () => props.mod.name,
  () => {
    expandedOpponents.value = new Set()
  }
)

function isExpanded(opponent: string): boolean {
  return expandedOpponents.value.has(opponent)
}

function toggleConflict(opponent: string) {
  const next = new Set(expandedOpponents.value)
  if (next.has(opponent)) next.delete(opponent)
  else next.add(opponent)
  expandedOpponents.value = next
}

function openPrioForm() {
  prioText.value = props.mod.priority > 0 ? String(props.mod.priority) : ''
  prioDialog.value?.showModal()
  setTimeout(() => prioInput.value?.focus(), 50)
}

async function applyPriority() {
  const p = parseInt(prioText.value)
  if (isNaN(p) || p < 0 || p > 99) return
  prioDialog.value?.close()
  await wails.setPriority(props.mod.name, p)
}

async function clearPriority() {
  await wails.setPriority(props.mod.name, 0)
}
</script>

<style scoped>
.detail {
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.detail-name {
  font-size: 13px;
  font-weight: 700;
  color: var(--cp-primary);
  text-shadow: var(--glow-primary);
  text-transform: uppercase;
  letter-spacing: 1px;
  word-break: break-all;
  padding-bottom: 8px;
  border-bottom: 2px solid var(--cp-primary);
}
.detail-stats {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.stat-row {
  display: flex;
  gap: 8px;
  font-size: 12px;
  padding: 4px 0;
  border-bottom: 1px solid var(--cp-border);
}
.stat-label {
  color: var(--cp-dim);
  width: 130px;
  flex-shrink: 0;
  text-transform: uppercase;
  font-size: 11px;
  letter-spacing: 0.4px;
}
.detail-actions {
  display: flex;
  gap: 8px;
}
.detail-section {
  display: flex;
  flex-direction: column;
  gap: 4px;
  border-radius: 0;
  padding: 8px;
}
.section-conflicts {
  background: rgba(255, 51, 102, 0.05);
  border: 1px solid rgba(255, 51, 102, 0.3);
  box-shadow: 0 0 12px rgba(255, 51, 102, 0.08);
}
.section-loadorder {
  background: rgba(0, 229, 255, 0.04);
  border: 1px solid rgba(0, 229, 255, 0.25);
  box-shadow: 0 0 12px rgba(0, 229, 255, 0.06);
}
.section-title {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 1px;
  padding-bottom: 5px;
  margin-bottom: 2px;
}
.section-title--conflict {
  color: var(--cp-error);
  text-shadow: var(--glow-error);
  border-bottom: 1px solid rgba(255, 51, 102, 0.4);
}
.section-title--loadorder {
  color: var(--cp-focus);
  text-shadow: var(--glow-focus);
  border-bottom: 1px solid rgba(0, 229, 255, 0.3);
}
.conflict-list {
  max-height: 260px;
  overflow-y: auto;
}
.conflict-group {
  border-bottom: 1px solid var(--cp-border);
}
.conflict-entry {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 5px 2px;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.conflict-entry:hover {
  background: rgba(255, 51, 102, 0.08);
}
.conflict-opponent {
  display: flex;
  align-items: center;
  gap: 3px;
  min-width: 0;
}
.conflict-opponent span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.conflict-opponent svg {
  flex-shrink: 0;
}
.conflict-count {
  font-size: 10px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: 0;
  background: rgba(255, 51, 102, 0.15);
  color: var(--cp-error);
  border: 1px solid var(--cp-error);
  flex-shrink: 0;
}
.conflict-resources {
  padding: 2px 4px 6px 18px;
}
.conflict-resource {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 3px 0;
  color: var(--cp-dim);
  font-family: monospace;
  font-size: 10px;
  line-height: 1.35;
}
.resource-index {
  min-width: 16px;
  color: var(--cp-error);
  text-align: right;
  flex-shrink: 0;
}
.resource-path {
  min-width: 0;
  overflow-wrap: anywhere;
  user-select: text;
}
.prio-dialog {
  min-width: 300px;
}
.prio-dialog input {
  width: 100%;
}
</style>
