import type { Environment } from '../api'

const categories = [
  { label: '大赛项目', links: ['用户端', '管理端', '广州征集', '数据要素X', '数超官网'] },
  { label: '词元项目', links: ['词元官网', '词元管理端', '产业平台', '渠道管理端', '查询中心', '词元交易旧版'] },
  { label: '统一登录', links: ['OIDC登录页', 'OIDC管理端'] },
  { label: '运维项目', links: ['Grafana', 'Prometheus', 'Nacos'] },
]

const aliases: Record<string, string> = {
  渠道管理后台: '渠道管理端',
  词元交易: '词元交易旧版',
  OIDC登录: 'OIDC登录页',
  OIDC后台: 'OIDC管理端',
}

export function groupEnvironmentLinks(environment: Environment) {
  const configured = new Map<string, string>()
  for (const link of environment.extra ?? []) {
    const label = link.label.trim()
    configured.set(aliases[label] ?? label, (link.proxy_url || link.url).trim())
  }
  configured.set('用户端', environment.user_url?.trim() || configured.get('用户端') || '')
  configured.set('管理端', environment.admin_url?.trim() || configured.get('管理端') || '')

  const groups = categories.map(category => ({
    label: category.label,
    links: category.links.map(label => ({ label, url: configured.get(label) ?? '' })),
  }))
  const known = new Set(categories.flatMap(category => category.links))
  const other = Array.from(configured, ([label, url]) => ({ label, url })).filter(link => !known.has(link.label))
  if (other.length) groups.push({ label: '其他', links: other })
  return groups
}
