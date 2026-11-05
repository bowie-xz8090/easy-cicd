export function getRuntimeBasePath(): string {
  const assetScript = Array.from(document.scripts).find((script) => {
    if (!script.src) return false
    const pathname = new URL(script.src, window.location.href).pathname
    return pathname.includes('/assets/')
  })

  if (assetScript?.src) {
    const pathname = new URL(assetScript.src, window.location.href).pathname
    const assetIndex = pathname.indexOf('/assets/')
    if (assetIndex >= 0) {
      return pathname.slice(0, assetIndex)
    }
  }

  const parts = window.location.pathname.split('/').filter(Boolean)
  return parts.length > 0 ? `/${parts[0]}` : ''
}
