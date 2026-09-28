import type { RuntimeContainerConfig, RuntimeModule } from '../api/types'

export function effectiveRuntimeName(configuredRuntime?: string, detectedRuntime?: string, runtimeHint?: string): string {
  return [configuredRuntime, detectedRuntime, runtimeHint]
    .map((value) => value?.trim().toLowerCase() ?? '')
    .find(Boolean) ?? ''
}

export function updatePHPModuleSelection(modules: RuntimeModule[], name: string, enabled: boolean): RuntimeModule[] {
  const normalizedName = name.trim()
  const withoutModule = modules.filter((item) => item.name !== normalizedName)
  return enabled ? [...withoutModule, { name: normalizedName }] : withoutModule
}

export function preparePHPModuleConfig(config: RuntimeContainerConfig): RuntimeContainerConfig {
  return {
    ...config,
    runtime: config.runtime.trim() || 'php',
  }
}
