// Before the report switches were split, console.detail controlled all three entries.
// Keep that preference until an explicit value is saved for each new switch.
export function withSeparateReportModules<T extends Record<string, boolean>>(
  consoleConfig: T
): T {
  if (typeof consoleConfig.detail !== 'boolean') return consoleConfig

  return {
    ...consoleConfig,
    overview: consoleConfig.overview ?? consoleConfig.detail,
    usage_report: consoleConfig.usage_report ?? consoleConfig.detail,
  }
}
