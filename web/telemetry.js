import { BrowserMetricsProvider } from '@ipfs-shipyard/ignite-metrics/BrowserMetricsProvider'

window.addEventListener('load', () => {
  const telemetry = new BrowserMetricsProvider({ appKey: 'b09437fd68bb257fef9b844a624fac0859b6224e' })
  window.telemetry = telemetry
  window.removeMetricsConsent = () => telemetry.removeConsent(['minimal'])
  window.addMetricsConsent = () => telemetry.addConsent(['minimal'])
})
