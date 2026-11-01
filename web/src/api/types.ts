export type Role = 'admin' | 'operator' | 'viewer'

export interface User {
  id: number
  username: string
  role: Role
  disabled: boolean
  createdAt: number
  lastLoginAt?: number
}

export interface AuthStatus {
  setupRequired: boolean
  authenticated: boolean
  user?: User
  csrfToken?: string
}

export interface LoginResponse {
  user: User
  csrfToken: string
}

export interface CaptureSession {
  id: string
  dir: string
  captureDate: number
  fluxzyVersion: string
  status: string
  exchangeCount: number
  totalBytes: number
  lastIndexedAt: number
  rulesHash: string
  pinned: boolean
  active?: boolean
  online?: boolean
  hasRaw?: boolean
  hasCold?: boolean
}

export interface Stats {
  sessionId: string
  total: number
  pending: number
  errors: number
  reqBytes: number
  respBytes: number
  status2xx: number
  status3xx: number
  status4xx: number
  status5xx: number
  rps: number
  bytesPerSec: number
  lastEventAt: number
}

export interface CaptureStatus {
  online: boolean
  activeSessionId: string
  stats: Stats
  indexing: boolean
  activeSession?: CaptureSession
  proxyAddress: string
}

export interface ExchangeRow {
  id: number
  connectionId: number
  method: string
  scheme: string
  host: string
  port: number
  path: string
  url: string
  status: number
  contentType: string
  reqSize: number
  respSize: number
  durationMs: number
  startedAt: number
  pending: boolean
  isWebSocket: boolean
  hasError: boolean
  tags: string[]
  agent: string
  httpVersion: string
  egressIp: string
  comment: string
  secure: boolean
}

export interface HeaderField {
  name: string
  value: string
  forwarded?: boolean
}

export interface ExchangeDetail {
  exchange: ExchangeRow
  request: {
    method: string
    scheme: string
    path: string
    authority: string
    url: string
    headers: HeaderField[]
  }
  response?: {
    statusCode: number
    headers: HeaderField[]
  }
  metrics: {
    startedAt: string
    durationMs: number
    totalSent: number
    totalReceived: number
    requestHeaderLength: number
    responseHeaderLength: number
    reusingConnection: boolean
    downStreamClientAddress: string
    downStreamClientPort: number
    downStreamLocalAddress: string
    downStreamLocalPort: number
    times: Record<string, string>
  }
  tags: string[]
  agent?: { id: number; friendlyName: string }
  clientErrors: {
    errorCode: number
    message: string
    exceptionMessage?: string
    networkErrorCode?: string
  }[]
  processInfo?: { processId: number; processPath?: string; processArguments?: string }
  connection?: {
    id: number
    httpVersion: string
    hostName: string
    port: number
    secure: boolean
    requestProcessed: number
    localPort: number
    localAddress: string
    remoteAddress: string
    dnsStart?: string
    dnsEnd?: string
    tcpOpening?: string
    tcpOpened?: string
    sslStart?: string
    sslEnd?: string
  }
  webSocketMessages?: {
    id: number
    direction: number
    opCode: number
    length: number
    writtenLength: number
    data?: string
    messageStart?: string
    messageEnd?: string
    frameCount: number
  }[]
  requestTrailers?: HeaderField[]
  responseTrailers?: HeaderField[]
  bodies: {
    requestExists: boolean
    responseExists: boolean
    requestSize: number
    responseSize: number
  }
  expired: boolean
}

export interface ExchangeList {
  items: ExchangeRow[]
  nextCursor: number
  cold: boolean
  sampled: boolean
  count: number
}

export interface OptionDef {
  value: string
  label: string
}

export interface FieldDef {
  path: string
  label: string
  type: 'string' | 'int' | 'bool' | 'select' | 'textarea' | 'intlist' | 'stringlist' | 'headers' | 'json'
  required?: boolean
  options?: OptionDef[]
  default?: unknown
  placeholder?: string
  help?: string
}

export interface TypeDef {
  typeKind: string
  label: string
  category: string
  description: string
  scope: string
  fields: FieldDef[]
  kind: 'filter' | 'action'
}

export interface RuleSchema {
  filters: TypeDef[]
  actions: TypeDef[]
}

export interface RuleDraft {
  name: string
  enabled: boolean
  filter: Record<string, any>
  actions: Record<string, any>[]
}

export interface RuleModel {
  rules: RuleDraft[]
}

export interface ParsedRule {
  name: string
  filter: Record<string, any>
  actions: Record<string, any>[]
  filterKind: string
  actionKinds: string[]
}

export interface ConfigVersion {
  id: number
  yaml?: string
  yamlHash: string
  authorId?: number
  authorName: string
  note: string
  validationStatus: string
  validationError: string
  status: string
  activeSessionId: string
  createdAt: number
}

export interface RulesStatus {
  yaml: string
  hash: string
  exists: boolean
  isDefault: boolean
  pending: boolean
  activeSessionId: string
  activeSince: number
  activeRules?: unknown
  modifiedAt: number
  summary: ParsedRule[]
  versions: ConfigVersion[]
  parseError?: string
}

export interface ValidationResult {
  valid: boolean
  skipped: boolean
  errors: string[]
  warnings: string[]
  output?: string
  durationMs: number
}

export interface AuditEntry {
  id: number
  userId?: number
  username: string
  action: string
  target: string
  detail: string
  ip: string
  userAgent: string
  createdAt: number
}

export interface Aggregate {
  bucket: number
  sessionId?: string
  count: number
  reqBytes: number
  respBytes: number
  status2xx: number
  status3xx: number
  status4xx: number
  status5xx: number
  errors: number
}

export interface HostCount {
  host: string
  count: number
}

export interface RetentionSettings {
  hotHours: number
  coldDays: number
  samplePercent: number
}

export interface CAInfo {
  source: 'custom' | 'embedded'
  subject: string
  issuer: string
  commonName: string
  serialNumber: string
  notBefore: number
  notAfter: number
  fingerprintSha256: string
  path: string
  size: number
  updatedAt: number
  daysRemaining: number
}

export interface SystemInfo {
  version: string
  runtimeState?: string
  fluxzyVersion?: string
  fluxzyRelease?: string
  binarySource?: string
  activeSessionId: string
  online: boolean
  proxyAddress: string
  dataDir: string
  configDir: string
  consoleDir: string
  rulesFile: string
  sessionCount: number
  indexErrors: number
  cliAvailable: boolean
  hotRetention: string
  coldRetention: string
  samplePercent: number
  retention?: RetentionSettings
  now: number
}

export interface RuntimeSupervisorStatus {
  state: string
  pid: number
  activeSessionId: string
  binaryVersion: string
  binaryRelease: string
  binaryPath: string
  binarySource: string
  startedAt: number
  uptimeMs: number
  lastExitCode?: number
  lastError: string
  consecutiveFailures: number
  restartCount: number
  nextBackoffMs: number
  intentional: boolean
}

export interface StartupConfig {
  listenAddr: string
  mode: string
  reversePort: number
  maxUpstreamConnections: number
  requestBuffer: number
  maxCaptureCount: number
  serveH2: boolean
  parseUA: boolean
  noCertCache: boolean
  skipSSLDecryption: boolean
}

export interface RuntimeConfigInfo {
  settings: StartupConfig
  defaults: StartupConfig
  hash: string
  appliedHash: string
  appliedSessionId: string
  pending: boolean
}

export interface FluxzyBinaryInfo {
  path: string
  source: string
  version: string
  baseVersion: string
  commit: string
  releaseTag: string
  bundledRelease: string
  checkedAt: number
}

export interface RuntimeStatus {
  supervisor: RuntimeSupervisorStatus
  capture: CaptureStatus
  config?: RuntimeConfigInfo
  binary?: FluxzyBinaryInfo
  binaryError?: string
  proxyAddress?: string
}

export interface RotationSettings {
  enabled: boolean
  intervalHours: number
  maxExchanges: number
  maxSessionBytes: number
  minFreeBytes: number
  emergencyFreeBytes: number
  checkSeconds: number
}

export interface DiskInfo {
  freeBytes: number
  totalBytes: number
  usedPercent: number
}

export interface RotationStatus {
  settings: RotationSettings
  defaults: RotationSettings
  disk: DiskInfo
  activeSessionId: string
  sessionBytes: number
  lastRotatedAt: number
  lastReason: string
  nextCheckAt: number
}

export interface LogLine {
  seq: number
  at: number
  stream: string
  text: string
}
