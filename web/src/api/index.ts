import axios from 'axios'
import { getRuntimeBasePath } from '../utils/basePath'

// --- TypeScript Interfaces ---

export interface Project {
  owner: string
  name: string
  full_name: string
  label: string
  mode: 'single' | 'monorepo'
  selectable: boolean
  components: ProjectComponent[]
  clone_url: string
}

export interface ProjectComponent {
  key: string
  label: string
  path: string
}

export interface Branch {
  name: string
  commit_id: string
  commit_message: string
  commit_time?: string
}

export interface Tag {
  name: string
  commit_id: string
  commit_message: string
  commit_time?: string
}

export interface Environment {
  key: string
  label: string
  disabled: boolean
  need_password: boolean
  targets: DeploymentTarget[]
  user_url: string
  admin_url: string
  extra: { label: string; url: string }[] | null
}

export interface DeploymentTarget {
  key: string
  label: string
  server: string
  server_label: string
  server_host: string
  server_port: number
}

export interface DeployRequest {
  project_owner: string
  project_name: string
  branch: string
  ref_type: 'branch' | 'tag'
  commit_id?: string
  commit_message?: string
  commit_time?: string
  environment: string
  component?: string
  target: string
  deploy_password?: string
}

export interface DeployResponse {
  task_id: string
  status: string
  created_at: string
}

export interface TaskStatus {
  task_id: string
  status: string
  project_name: string
  branch: string
  environment: string
  created_at: string
  updated_at: string
}

export interface TaskLogs {
  task_id: string
  logs: string
}

export interface DeployRecord {
  id: string
  project_owner: string
  project_name: string
  project_label: string
  component: string
  component_label: string
  sub_project: string
  sub_project_label: string
  target: string
  target_label: string
  server: string
  branch: string
  commit_message: string
  commit_time?: string
  environment: string
  status: string
  created_at: string
  finished_at: string
}

export interface RecordFilter {
  project?: string
  project_keyword?: string
  sub_project_keyword?: string
  environment?: string
  status?: string
  page?: number
  page_size?: number
}

export interface RecordListResponse {
	total: number
	records: DeployRecord[]
}

export interface EditableConfig {
  environments: Record<string, EditableEnvironment>
  projects: ProjectBuildConfig
  servers: ServerSummary[]
}

export interface ServerSummary {
  key: string
  label: string
  host: string
  port: number
  deploy_path: string
}

export interface ArtifactConfig {
  key: string
  output: string
  upload_as: string
}

export interface BuildConfig {
  command: string
  artifacts: ArtifactConfig[]
}

export interface DeploymentConfig {
  label: string
  server: string
  build_command?: string
  script: string
  disabled: boolean | null
  build_override?: BuildConfig | null
}

export interface UnitEnvironmentConfig {
  disabled: boolean | null
  build: BuildConfig
  deployments: Record<string, DeploymentConfig>
}

export interface ComponentBuildConfig {
  label: string
  path: string
  build_type: '' | 'frontend' | 'backend'
  environments: Record<string, UnitEnvironmentConfig>
}

export interface ProjectBuildConfigItem {
  label: string
  mode: 'single' | 'monorepo'
  build_type: '' | 'frontend' | 'backend'
  environments: Record<string, UnitEnvironmentConfig>
  components: Record<string, ComponentBuildConfig>
}

export type ProjectBuildConfig = Record<string, ProjectBuildConfigItem>

export interface EnvironmentExtraLink {
  label: string
  url: string
}

export interface EditableEnvironment {
  label: string
  disabled: boolean
  servers: string[]
  links: {
    user_url: string
    admin_url: string
    extra: EnvironmentExtraLink[]
  }
}

export interface EnvironmentAccessConfig {
  disabled: boolean
  servers: string[]
  links: EditableEnvironment['links']
}

// --- API Error ---

export class ApiError extends Error {
  code: number

  constructor(code: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
}

// --- Axios Instance ---

const api = axios.create({
  baseURL: getRuntimeBasePath(),
  timeout: 30000,
})

api.interceptors.response.use(
  (response) => {
    const body = response.data
    if (body.code !== 0) {
      throw new ApiError(body.code, body.message || '请求失败')
    }
    return body.data
  },
  (error) => {
    if (error instanceof ApiError) {
      throw error
    }
    const message =
      error.response?.data?.message || error.message || '网络请求失败'
    throw new ApiError(-1, message)
  },
)

// --- API Functions ---

export async function fetchSiteInfo(): Promise<{ title: string }> {
  return api.get('/api/site-info') as unknown as { title: string }
}

export async function fetchProjects(): Promise<Project[]> {
  return api.get('/api/projects') as unknown as Project[]
}

export async function fetchBranches(
  owner: string,
  repo: string,
): Promise<Branch[]> {
  return api.get(
    `/api/projects/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/branches`,
  ) as unknown as Branch[]
}

export async function fetchTags(
  owner: string,
  repo: string,
): Promise<Tag[]> {
  return api.get(
    `/api/projects/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/tags`,
  ) as unknown as Tag[]
}

export async function fetchEnvironments(
  project?: string,
  component?: string,
): Promise<Environment[]> {
  const params: Record<string, string> = {}
  if (project) params.project = project
  if (component) params.component = component
  return api.get('/api/environments', { params }) as unknown as Environment[]
}

export async function triggerDeploy(
  request: DeployRequest,
): Promise<DeployResponse> {
  return api.post('/api/deploy', request) as unknown as DeployResponse
}

export async function fetchTaskStatus(taskId: string): Promise<TaskStatus> {
  return api.get(
    `/api/deploy/${encodeURIComponent(taskId)}/status`,
  ) as unknown as TaskStatus
}

export async function fetchTaskLogs(taskId: string): Promise<TaskLogs> {
  return api.get(
    `/api/deploy/${encodeURIComponent(taskId)}/logs`,
  ) as unknown as TaskLogs
}

export async function fetchRecords(
  filter?: RecordFilter,
): Promise<RecordListResponse> {
  return api.get('/api/deploy/records', {
    params: filter,
  }) as unknown as RecordListResponse
}

export async function clearDeployHistory(): Promise<{ deleted: number }> {
  return api.delete('/api/deploy/records') as unknown as { deleted: number }
}

function adminHeaders(adminToken: string) {
  return { Authorization: `Bearer ${adminToken}` }
}

export async function fetchEditableConfig(adminToken: string): Promise<EditableConfig> {
  return api.get('/api/config/editor', { headers: adminHeaders(adminToken) }) as unknown as EditableConfig
}

export async function updateEditableConfig(
  adminToken: string,
  config: EditableConfig,
): Promise<EditableConfig> {
  return api.put('/api/config/editor', config, { headers: adminHeaders(adminToken) }) as unknown as EditableConfig
}

export async function updateProjectBuildConfig(
  adminToken: string,
  projects: ProjectBuildConfig,
): Promise<ProjectBuildConfig> {
  return api.put('/api/config/projects', projects, { headers: adminHeaders(adminToken) }) as unknown as ProjectBuildConfig
}

export async function updateEnvironmentAccess(
  adminToken: string,
  environment: string,
  access: EnvironmentAccessConfig,
): Promise<EnvironmentAccessConfig> {
  return api.put(
    `/api/config/environments/${encodeURIComponent(environment)}`,
    access,
    { headers: adminHeaders(adminToken) },
  ) as unknown as EnvironmentAccessConfig
}

export async function cancelDeploy(taskId: string): Promise<{ message: string }> {
  return api.post(
    `/api/deploy/${encodeURIComponent(taskId)}/cancel`,
  ) as unknown as { message: string }
}
