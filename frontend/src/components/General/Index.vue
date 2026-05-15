<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Dialogs } from '@wailsio/runtime'
import ListItem from '../Setting/ListRow.vue'
import LanguageSwitcher from '../Setting/LanguageSwitcher.vue'
import ThemeSetting from '../Setting/ThemeSetting.vue'
import { fetchAppSettings, saveAppSettings, type AppSettings } from '../../services/appSettings'
import {
  fetchConfigImportStatus,
  fetchConfigImportStatusForFile,
  importFromCcSwitch,
  importFromCustomFile,
  type ConfigImportResult,
  type ConfigImportStatus,
} from '../../services/configImport'
import {
  fetchPricingStatus,
  updatePricingFromUpstream,
  updatePricingFromFile,
  resetPricingToBuiltin,
  type PricingStatus,
} from '../../services/modelPricing'
import { showToast } from '../../utils/toast'
import BaseButton from '../common/BaseButton.vue'

const router = useRouter()
const { t } = useI18n()
const heatmapEnabled = ref(true)
const homeTitleVisible = ref(true)
const autoStartEnabled = ref(false)
const settingsLoading = ref(true)
const saveBusy = ref(false)
const importStatus = ref<ConfigImportStatus | null>(null)
const customImportStatus = ref<ConfigImportStatus | null>(null)
const importBusy = ref(false)
const pricingStatus = ref<PricingStatus | null>(null)
const pricingBusy = ref(false)
const pricingAction = ref<'update' | 'import' | 'reset' | ''>('')

const goBack = () => {
  router.push('/')
}

const loadAppSettings = async () => {
  settingsLoading.value = true
  try {
    const data = await fetchAppSettings()
    heatmapEnabled.value = data?.show_heatmap ?? true
    homeTitleVisible.value = data?.show_home_title ?? true
    autoStartEnabled.value = data?.auto_start ?? false
  } catch (error) {
    console.error('failed to load app settings', error)
    heatmapEnabled.value = true
    homeTitleVisible.value = true
    autoStartEnabled.value = false
  } finally {
    settingsLoading.value = false
  }
}

const persistAppSettings = async () => {
  if (settingsLoading.value || saveBusy.value) return
  saveBusy.value = true
  try {
    const payload: AppSettings = {
      show_heatmap: heatmapEnabled.value,
      show_home_title: homeTitleVisible.value,
      auto_start: autoStartEnabled.value,
    }
    await saveAppSettings(payload)
    window.dispatchEvent(new CustomEvent('app-settings-updated'))
  } catch (error) {
    console.error('failed to save app settings', error)
  } finally {
    saveBusy.value = false
  }
}

onMounted(() => {
  void loadAppSettings()
  void loadImportStatus()
  void loadPricingStatus()
})

const loadImportStatus = async () => {
  try {
    importStatus.value = await fetchConfigImportStatus()
  } catch (error) {
    console.error('failed to load cc-switch import status', error)
    importStatus.value = null
  }
}

const activeImportStatus = computed(() => customImportStatus.value ?? importStatus.value)
const hasCustomSelection = computed(() => Boolean(customImportStatus.value))
const shouldShowDefaultMissingHint = computed(() => {
  if (hasCustomSelection.value) return false
  const status = importStatus.value
  if (!status) return false
  return !status.config_exists
})
const pendingProviders = computed(() => activeImportStatus.value?.pending_provider_count ?? 0)
const pendingServers = computed(() => activeImportStatus.value?.pending_mcp_count ?? 0)
const configPath = computed(() => activeImportStatus.value?.config_path ?? '')
const canImportDefault = computed(() => {
  const status = importStatus.value
  if (!status) return false
  return Boolean(status.pending_providers || status.pending_mcp)
})
const canImportCustom = computed(() => {
  const status = customImportStatus.value
  if (!status) return false
  return Boolean(status.pending_providers || status.pending_mcp)
})
const canImportActive = computed(() =>
  hasCustomSelection.value ? canImportCustom.value : canImportDefault.value,
)
const showImportRow = computed(() => Boolean(importStatus.value) || hasCustomSelection.value)
const importPathLabel = computed(() => {
  if (!configPath.value) return ''
  return t('components.general.import.path', { path: configPath.value })
})
const importDetailLabel = computed(() => {
  if (shouldShowDefaultMissingHint.value) {
    return t('components.general.import.missingDefault')
  }
  if (!activeImportStatus.value) {
    return t('components.general.import.noFile')
  }
  const detail = canImportActive.value
    ? t('components.general.import.detail', {
        providers: pendingProviders.value,
        servers: pendingServers.value,
      })
    : t('components.general.import.synced')
  if (!importPathLabel.value) return detail
  return `${importPathLabel.value} · ${detail}`
})
const importButtonText = computed(() => {
  if (importBusy.value) {
    return t('components.general.import.importing')
  }
  if (hasCustomSelection.value) {
    return t('components.general.import.confirm')
  }
  if (shouldShowDefaultMissingHint.value || canImportDefault.value) {
    return t('components.general.import.cta')
  }
  return t('components.general.import.syncedButton')
})
const primaryButtonDisabled = computed(() => importBusy.value || !canImportActive.value)
const secondaryButtonLabel = computed(() =>
  hasCustomSelection.value
    ? t('components.general.import.clear')
    : t('components.general.import.upload'),
)
const secondaryButtonVariant = computed(() => 'outline' as const)

const processImportResult = async (result?: ConfigImportResult | null) => {
  if (!result) return
  if (hasCustomSelection.value && result.status?.config_path === customImportStatus.value?.config_path) {
    customImportStatus.value = result.status
  } else {
    importStatus.value = result.status
  }
  const importedProviders = result.imported_providers ?? 0
  const importedServers = result.imported_mcp ?? 0
  if (importedProviders > 0 || importedServers > 0) {
    showToast(
      t('components.main.importConfig.success', {
        providers: importedProviders,
        servers: importedServers,
      })
    )
  } else if (result.status?.config_exists) {
    showToast(t('components.main.importConfig.empty'))
  }
  await loadImportStatus()
}

const handleImportClick = async () => {
  if (importBusy.value || !importStatus.value || !canImportDefault.value) return
  importBusy.value = true
  try {
    const result = await importFromCcSwitch()
    await processImportResult(result)
  } catch (error) {
    console.error('failed to import cc-switch config', error)
    showToast(t('components.main.importConfig.error'), 'error')
  } finally {
    importBusy.value = false
  }
}

const handleConfirmCustomImport = async () => {
  const path = customImportStatus.value?.config_path
  if (!path || importBusy.value || !canImportCustom.value) return
  importBusy.value = true
  try {
    const result = await importFromCustomFile(path)
    await processImportResult(result)
  } catch (error) {
    console.error('failed to import custom cc-switch config', error)
    showToast(t('components.main.importConfig.error'), 'error')
  } finally {
    importBusy.value = false
  }
}

const handlePrimaryImport = async () => {
  if (hasCustomSelection.value) {
    await handleConfirmCustomImport()
  } else {
    await handleImportClick()
  }
}

const handleUploadClick = async () => {
  if (importBusy.value) return
  let selectedPath = ''
  try {
    const selection = await Dialogs.OpenFile({
      Title: t('components.general.import.uploadTitle'),
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsOtherFiletypes: false,
      Filters: [
        {
          DisplayName: 'JSON (*.json)',
          Pattern: '*.json',
        },
      ],
      AllowsMultipleSelection: false,
    })
    selectedPath = Array.isArray(selection) ? selection[0] : selection
    if (!selectedPath) return
    const status = await fetchConfigImportStatusForFile(selectedPath)
    customImportStatus.value = status
  } catch (error) {
    console.error('failed to load custom cc-switch config status', error)
    showToast(t('components.general.import.loadError'), 'error')
  }
}

const clearCustomSelection = () => {
  customImportStatus.value = null
}

const handleSecondaryImportAction = async () => {
  if (hasCustomSelection.value) {
    clearCustomSelection()
  } else {
    await handleUploadClick()
  }
}

const loadPricingStatus = async () => {
  try {
    pricingStatus.value = await fetchPricingStatus()
  } catch (error) {
    console.error('failed to load pricing status', error)
    pricingStatus.value = null
  }
}

const formatPricingDate = (value: string): string => {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

const pricingSourceLabel = computed(() => {
  const source = pricingStatus.value?.source || 'builtin'
  const key = `components.general.pricing.source.${source}`
  const translated = t(key)
  return translated === key ? source : translated
})

const pricingMainLabel = computed(() =>
  t('components.general.pricing.label', { source: pricingSourceLabel.value }),
)

const pricingSubLabel = computed(() => {
  const count = pricingStatus.value?.model_count ?? 0
  const updatedAt = formatPricingDate(pricingStatus.value?.updated_at || '')
  if (!updatedAt) {
    return t('components.general.pricing.subLabelNever', { count })
  }
  return t('components.general.pricing.subLabel', { count, updatedAt })
})

const pricingUpdateLabel = computed(() =>
  pricingBusy.value && pricingAction.value === 'update'
    ? t('components.general.pricing.buttons.updating')
    : t('components.general.pricing.buttons.update'),
)

const handlePricingUpdate = async () => {
  if (pricingBusy.value) return
  pricingBusy.value = true
  pricingAction.value = 'update'
  try {
    const status = await updatePricingFromUpstream()
    pricingStatus.value = status
    showToast(
      t('components.general.pricing.toast.updateSuccess', { count: status.model_count }),
    )
  } catch (error) {
    console.error('failed to update pricing', error)
    showToast(t('components.general.pricing.toast.fetchError'), 'error')
  } finally {
    pricingBusy.value = false
    pricingAction.value = ''
  }
}

const handlePricingImport = async () => {
  if (pricingBusy.value) return
  let selectedPath = ''
  try {
    const selection = await Dialogs.OpenFile({
      Title: t('components.general.pricing.uploadTitle'),
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsOtherFiletypes: false,
      Filters: [
        {
          DisplayName: 'JSON (*.json)',
          Pattern: '*.json',
        },
      ],
      AllowsMultipleSelection: false,
    })
    selectedPath = Array.isArray(selection) ? selection[0] : selection
  } catch (error) {
    console.error('failed to open pricing JSON', error)
    showToast(t('components.general.pricing.toast.importError'), 'error')
    return
  }
  if (!selectedPath) return
  pricingBusy.value = true
  pricingAction.value = 'import'
  try {
    const status = await updatePricingFromFile(selectedPath)
    pricingStatus.value = status
    showToast(
      t('components.general.pricing.toast.importSuccess', { count: status.model_count }),
    )
  } catch (error) {
    console.error('failed to import pricing', error)
    showToast(t('components.general.pricing.toast.importError'), 'error')
  } finally {
    pricingBusy.value = false
    pricingAction.value = ''
  }
}

const handlePricingReset = async () => {
  if (pricingBusy.value) return
  const confirmed = window.confirm(t('components.general.pricing.confirmReset'))
  if (!confirmed) return
  pricingBusy.value = true
  pricingAction.value = 'reset'
  try {
    const status = await resetPricingToBuiltin()
    pricingStatus.value = status
    showToast(t('components.general.pricing.toast.resetSuccess'))
  } catch (error) {
    console.error('failed to reset pricing', error)
    showToast(t('components.general.pricing.toast.resetError'), 'error')
  } finally {
    pricingBusy.value = false
    pricingAction.value = ''
  }
}
</script>

<template>
  <div class="main-shell general-shell">
    <div class="global-actions">
      <p class="global-eyebrow">{{ $t('components.general.title.application') }}</p>
      <button class="ghost-icon" :aria-label="$t('components.general.buttons.back')" @click="goBack">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M15 18l-6-6 6-6"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
    </div>

    <div class="general-page">
      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.application') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.heatmap')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="heatmapEnabled"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.homeTitle')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="homeTitleVisible"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.autoStart')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="autoStartEnabled"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem
            v-if="showImportRow"
            :label="$t('components.general.import.label')"
            :sub-label="importDetailLabel"
          >
            <div class="import-actions">
              <BaseButton
                size="sm"
                variant="outline"
                type="button"
                :disabled="primaryButtonDisabled"
                @click="handlePrimaryImport"
              >
                {{ importButtonText }}
              </BaseButton>
              <BaseButton
                size="sm"
                :variant="secondaryButtonVariant"
                type="button"
                :disabled="importBusy"
                @click="handleSecondaryImportAction"
              >
                {{ secondaryButtonLabel }}
              </BaseButton>
              <BaseButton
                v-if="hasCustomSelection"
                size="sm"
                variant="outline"
                type="button"
                :disabled="importBusy"
                @click="handleUploadClick"
              >
                {{ $t('components.general.import.reupload') }}
              </BaseButton>
            </div>
          </ListItem>

        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.pricing.title') }}</h2>
        <div class="mac-panel">
          <ListItem :label="pricingMainLabel" :sub-label="pricingSubLabel">
            <div class="import-actions">
              <BaseButton
                size="sm"
                variant="outline"
                type="button"
                :disabled="pricingBusy"
                @click="handlePricingUpdate"
              >
                {{ pricingUpdateLabel }}
              </BaseButton>
              <BaseButton
                size="sm"
                variant="outline"
                type="button"
                :disabled="pricingBusy"
                @click="handlePricingImport"
              >
                {{ $t('components.general.pricing.buttons.import') }}
              </BaseButton>
              <BaseButton
                size="sm"
                variant="outline"
                type="button"
                :disabled="pricingBusy || pricingStatus?.source === 'builtin'"
                @click="handlePricingReset"
              >
                {{ $t('components.general.pricing.buttons.reset') }}
              </BaseButton>
            </div>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.exterior') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.language')">
            <LanguageSwitcher />
          </ListItem>
          <ListItem :label="$t('components.general.label.theme')">
            <ThemeSetting />
          </ListItem>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.import-actions {
  display: flex;
  gap: 0.35rem;
  justify-content: flex-end;
  flex-wrap: wrap;
}

.import-actions .btn {
  min-width: 56px;
  padding: 0.3rem 0.75rem;
  font-size: 0.7rem;
}

.import-actions .btn-outline,
.import-actions .btn-ghost {
  padding-inline: 0.75rem;
}
</style>
