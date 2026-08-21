import { EventsOn } from '../../wailsjs/runtime/runtime'
import * as App from '../../wailsjs/go/main/App'
import { useAppStore } from '../stores/app'

export function useWails() {
  const store = useAppStore()

  function registerEvents() {
    EventsOn('scan:progress', (msg: string) => {
      store.scanProgress = msg
    })
  }

  async function loadConfig() {
    const cfg = await App.GetConfig()
    store.modDir = cfg.modDir
    store.modStructure = (cfg.modStructure as 'default' | 'MO2') || 'default'
    store.mo2Dir = cfg.mo2Dir ?? ''
    store.mo2Profile = cfg.mo2Profile ?? ''
  }

  async function pickFolder(): Promise<string> {
    const dir = await App.PickFolder()
    if (dir) store.modDir = dir
    return dir
  }

  async function runScan(dir?: string) {
    const target = dir ?? store.modDir
    if (!target) return
    store.scanning = true
    store.scanError = ''
    try {
      const result = await App.Scan(target)
      store.setScanResult(result)
      store.dirty = false
      if (result.hasModlist) {
        const newConflicting = result.rows
          .filter((r) => r.unlisted && r.mod && r.mod.conflictCount > 0)
          .map((r) => r.name)
        if (newConflicting.length > 0) {
          store.newConflictingMods = newConflicting
          store.showConflictResolution = true
        }
      }
    } catch (e: any) {
      store.scanError = String(e)
    } finally {
      store.scanning = false
    }
  }

  async function setPriority(name: string, p: number) {
    try {
      const result = await App.SetPriority(name, p)
      store.updateScanResult(result)
      store.dirty = true
    } catch (e: any) {
      store.scanError = String(e)
    }
  }

  async function reorderGroup(names: string[], movedName: string) {
    try {
      const result = await App.ReorderConflictGroup(names, movedName)
      store.updateScanResult(result)
      store.dirty = true
    } catch (e: any) {
      store.scanError = String(e)
    }
  }

  async function writeModlist() {
    const result = await App.WriteModlist()
    store.updateScanResult(result)
    store.dirty = false
  }

  async function getApplyPreview() {
    return App.GetApplyPreview()
  }

  async function getConflictGroup(name: string) {
    return App.GetConflictGroup(name)
  }

  async function setModlistOrder(names: string[]) {
    try {
      const result = await App.SetModlistOrder(names)
      store.updateScanResult(result)
      store.dirty = true
    } catch (e: any) {
      store.scanError = String(e)
    }
  }

  async function groupConflicts() {
    try {
      const result = await App.GroupConflicts()
      store.updateScanResult(result)
      store.dirty = false
    } catch (e: any) {
      store.scanError = String(e)
    }
  }

  async function listBackups(): Promise<string[]> {
    return App.ListBackups()
  }

  async function restoreBackup(filename: string) {
    const result = await App.RestoreBackup(filename)
    store.updateScanResult(result)
  }

  async function getMO2Profiles(instanceDir: string): Promise<string[]> {
    try {
      const profiles = await App.GetMO2Profiles(instanceDir)
      store.mo2Profiles = profiles ?? []
      return store.mo2Profiles
    } catch (e: any) {
      store.scanError = String(e)
      store.mo2Profiles = []
      return []
    }
  }

  async function scanMO2(instanceDir: string, profile: string) {
    store.scanning = true
    store.scanError = ''
    try {
      const result = await App.ScanMO2(instanceDir, profile)
      store.mo2Dir = instanceDir
      store.mo2Profile = profile
      store.setScanResult(result)
      store.dirty = false
    } catch (e: any) {
      store.scanError = String(e)
    } finally {
      store.scanning = false
    }
  }

  return {
    registerEvents,
    loadConfig,
    pickFolder,
    runScan,
    setPriority,
    reorderGroup,
    writeModlist,
    getApplyPreview,
    getConflictGroup,
    setModlistOrder,
    groupConflicts,
    listBackups,
    restoreBackup,
    getMO2Profiles,
    scanMO2,
  }
}
