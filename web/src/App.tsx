import { useState, useEffect, useCallback } from 'react'
import { Loader2, ExternalLink, Clock, Activity, Cpu, MemoryStick, Timer, Hash, MessageSquare, ChevronLeft, ChevronRight } from 'lucide-react'
import { checkAuth, listWorkspaces, listSandboxes, getMe, getSandboxUsage, getSandboxTraces, type Workspace, type Sandbox, type UsageSummary, type TraceItem } from './lib/api'
import { Login } from './components/Login'
import { SandboxList } from './components/SandboxList'
import { AdminPanel } from './components/AdminPanel'

export interface UserInfo {
  id: string
  username: string
  email: string
  name?: string | null
  picture?: string | null
  role: string
}

const TRACES_PER_PAGE = 20

function formatTokens(n: number): string {
  return n.toLocaleString()
}

export default function App() {
  const [authed, setAuthed] = useState<boolean | null>(null)
  const [user, setUser] = useState<UserInfo | null>(null)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState<string | null>(null)
  const [sandboxes, setSandboxes] = useState<Sandbox[]>([])
  const [activeSandboxId, setActiveSandboxId] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [showAdmin, setShowAdmin] = useState(false)

  // Usage & traces state
  const [usageData, setUsageData] = useState<UsageSummary[] | null>(null)
  const [traces, setTraces] = useState<TraceItem[]>([])
  const [tracesTotal, setTracesTotal] = useState(0)
  const [tracesPage, setTracesPage] = useState(0)

  const refreshSandboxes = useCallback(async () => {
    if (!selectedWorkspaceId) return
    try {
      const list = await listSandboxes(selectedWorkspaceId)
      setSandboxes(list)
    } catch {
      // ignore
    }
  }, [selectedWorkspaceId])

  // On auth, fetch workspaces and auto-select the first one.
  useEffect(() => {
    checkAuth().then((ok) => {
      setAuthed(ok)
      if (ok) {
        listWorkspaces().then((ws) => {
          setWorkspaces(ws)
          if (ws.length > 0) {
            setSelectedWorkspaceId(ws[0].id)
          }
        }).catch(() => {})
        getMe().then(setUser).catch(() => {})
      }
    })
  }, [])

  // On workspace change, fetch sandboxes for that workspace.
  useEffect(() => {
    if (selectedWorkspaceId) {
      refreshSandboxes()
      setActiveSandboxId(null)
    } else {
      setSandboxes([])
      setActiveSandboxId(null)
    }
  }, [selectedWorkspaceId, refreshSandboxes])

  // Fetch usage and traces when active sandbox changes.
  useEffect(() => {
    setUsageData(null)
    setTraces([])
    setTracesTotal(0)
    setTracesPage(0)
    if (!activeSandboxId) return
    getSandboxUsage(activeSandboxId).then((r) => setUsageData(r.usage || [])).catch(() => {})
    getSandboxTraces(activeSandboxId, TRACES_PER_PAGE, 0).then((r) => {
      setTraces(r.traces || [])
      setTracesTotal(r.total || 0)
    }).catch(() => {})
  }, [activeSandboxId])

  // Fetch traces when page changes.
  useEffect(() => {
    if (!activeSandboxId || tracesPage === 0) return
    getSandboxTraces(activeSandboxId, TRACES_PER_PAGE, tracesPage * TRACES_PER_PAGE).then((r) => {
      setTraces(r.traces || [])
      setTracesTotal(r.total || 0)
    }).catch(() => {})
  }, [activeSandboxId, tracesPage])

  const handleSelectWorkspace = useCallback((id: string) => {
    setSelectedWorkspaceId(id || null)
  }, [])

  const handleLogout = useCallback(() => {
    setAuthed(false)
    setUser(null)
    setWorkspaces([])
    setSelectedWorkspaceId(null)
    setSandboxes([])
    setActiveSandboxId(null)
  }, [])

  if (authed === null) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <span className="text-[var(--muted-foreground)]">Loading...</span>
      </div>
    )
  }

  if (!authed) {
    return (
      <Login
        onSuccess={() => {
          setAuthed(true)
          listWorkspaces().then((ws) => {
            setWorkspaces(ws)
            if (ws.length > 0) {
              setSelectedWorkspaceId(ws[0].id)
            }
          }).catch(() => {})
          getMe().then(setUser).catch(() => {})
        }}
      />
    )
  }

  const activeSandboxData = sandboxes.find((s) => s.id === activeSandboxId)

  // Compute aggregate usage totals.
  const totalRequests = usageData ? usageData.reduce((s, u) => s + u.requestCount, 0) : 0
  const totalInput = usageData ? usageData.reduce((s, u) => s + u.inputTokens, 0) : 0
  const totalOutput = usageData ? usageData.reduce((s, u) => s + u.outputTokens, 0) : 0
  const totalCacheRead = usageData ? usageData.reduce((s, u) => s + u.cacheReadInputTokens, 0) : 0
  const totalCacheWrite = usageData ? usageData.reduce((s, u) => s + u.cacheCreationInputTokens, 0) : 0
  const totalPages = Math.ceil(tracesTotal / TRACES_PER_PAGE)

  let mainContent
  if (creating) {
    mainContent = (
      <div className="flex flex-col items-center justify-center gap-3">
        <Loader2 size={24} className="animate-spin text-[var(--muted-foreground)]" />
        <span className="text-[var(--muted-foreground)]">Creating sandbox...</span>
      </div>
    )
  } else if (activeSandboxId && activeSandboxData) {
    const isRunning = activeSandboxData.status === 'running'
    const isOffline = activeSandboxData.status === 'offline'
    const isOpenClaw = activeSandboxData.type === 'openclaw'
    const sandboxUrl = isOpenClaw ? activeSandboxData.openclawUrl : activeSandboxData.opencodeUrl
    const buttonLabel = isOpenClaw ? 'Open OpenClaw' : 'Open OpenCode'
    const fallbackLabel = isOpenClaw ? 'OpenClaw' : 'OpenCode'
    mainContent = (
      <div className="flex flex-col items-center gap-6 w-full max-w-2xl px-6 overflow-y-auto max-h-full py-8">
        <div className="w-full rounded-lg border border-[var(--border)] bg-[var(--card)] p-6">
          <h2 className="text-lg font-semibold text-[var(--foreground)] mb-4">{activeSandboxData.name}</h2>
          <div className="flex flex-col gap-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-[var(--muted-foreground)]">Status:</span>
              <span className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ${
                isRunning
                  ? 'bg-green-500/10 text-green-500'
                  : isOffline
                    ? 'bg-red-500/10 text-red-500'
                    : activeSandboxData.status === 'paused'
                      ? 'bg-yellow-500/10 text-yellow-500'
                      : 'bg-gray-500/10 text-[var(--muted-foreground)]'
              }`}>
                <span className={`inline-block h-1.5 w-1.5 rounded-full ${
                  isRunning
                    ? 'bg-green-500'
                    : isOffline
                      ? 'bg-red-500'
                      : activeSandboxData.status === 'paused'
                        ? 'bg-yellow-500'
                        : 'bg-gray-500'
                }`} />
                {activeSandboxData.status}
              </span>
              {activeSandboxData.isLocal && (
                <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-400">
                  local
                </span>
              )}
            </div>
            <div className="flex items-center gap-2 text-[var(--muted-foreground)]">
              <Clock size={14} />
              <span>Created: {new Date(activeSandboxData.createdAt).toLocaleString()}</span>
            </div>
            {activeSandboxData.lastActivityAt && (
              <div className="flex items-center gap-2 text-[var(--muted-foreground)]">
                <Activity size={14} />
                <span>Last active: {new Date(activeSandboxData.lastActivityAt).toLocaleString()}</span>
              </div>
            )}
            {activeSandboxData.idleTimeout != null && (
              <div className="flex items-center gap-2 text-[var(--muted-foreground)]">
                <Timer size={14} />
                <span>Idle timeout: {activeSandboxData.idleTimeout >= 60 ? `${Math.round(activeSandboxData.idleTimeout / 60)} min` : `${activeSandboxData.idleTimeout}s`}</span>
              </div>
            )}
            {!activeSandboxData.isLocal && activeSandboxData.cpu ? (
              <div className="flex items-center gap-2 text-[var(--muted-foreground)]">
                <Cpu size={14} />
                <span>CPU: {(activeSandboxData.cpu / 1000).toFixed(1)} cores</span>
              </div>
            ) : null}
            {!activeSandboxData.isLocal && activeSandboxData.memory ? (
              <div className="flex items-center gap-2 text-[var(--muted-foreground)]">
                <MemoryStick size={14} />
                <span>Memory: {Math.round(activeSandboxData.memory / (1024 * 1024))} MB</span>
              </div>
            ) : null}
          </div>
        </div>

        {isRunning && sandboxUrl ? (
          <a
            href={sandboxUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 rounded-md bg-[var(--primary)] px-4 py-2 text-sm font-medium text-[var(--primary-foreground)] hover:opacity-90 transition-opacity"
          >
            <ExternalLink size={16} />
            {buttonLabel}
          </a>
        ) : (
          <span className="text-sm text-[var(--muted-foreground)]">
            {isOffline ? 'Agent is offline. Reconnect the local agent to access.' : isRunning ? `${fallbackLabel} URL not configured` : `Sandbox must be running to open ${fallbackLabel}`}
          </span>
        )}

        {/* Usage Summary */}
        {usageData && usageData.length > 0 && (
          <div className="w-full rounded-lg border border-[var(--border)] bg-[var(--card)] p-6">
            <h3 className="text-sm font-semibold text-[var(--foreground)] mb-3 flex items-center gap-2">
              <Hash size={14} />
              Usage
            </h3>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm mb-4">
              <div>
                <div className="text-[var(--muted-foreground)] text-xs">Requests</div>
                <div className="text-[var(--foreground)] font-medium">{formatTokens(totalRequests)}</div>
              </div>
              <div>
                <div className="text-[var(--muted-foreground)] text-xs">Input tokens</div>
                <div className="text-[var(--foreground)] font-medium">{formatTokens(totalInput)}</div>
              </div>
              <div>
                <div className="text-[var(--muted-foreground)] text-xs">Output tokens</div>
                <div className="text-[var(--foreground)] font-medium">{formatTokens(totalOutput)}</div>
              </div>
              <div>
                <div className="text-[var(--muted-foreground)] text-xs">Cache read</div>
                <div className="text-[var(--foreground)] font-medium">{formatTokens(totalCacheRead)}</div>
              </div>
              <div>
                <div className="text-[var(--muted-foreground)] text-xs">Cache write</div>
                <div className="text-[var(--foreground)] font-medium">{formatTokens(totalCacheWrite)}</div>
              </div>
            </div>
            {usageData.length > 1 && (
              <div className="border-t border-[var(--border)] pt-3">
                <div className="text-xs text-[var(--muted-foreground)] mb-2">Per model</div>
                <div className="flex flex-col gap-1.5">
                  {usageData.map((u) => (
                    <div key={`${u.provider}-${u.model}`} className="flex items-center justify-between text-xs">
                      <span className="text-[var(--foreground)] font-mono truncate mr-2">{u.model}</span>
                      <span className="text-[var(--muted-foreground)] whitespace-nowrap">{formatTokens(u.requestCount)} req</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Traces Table */}
        {traces.length > 0 && (
          <div className="w-full rounded-lg border border-[var(--border)] bg-[var(--card)] p-6">
            <h3 className="text-sm font-semibold text-[var(--foreground)] mb-3 flex items-center gap-2">
              <MessageSquare size={14} />
              Traces
              <span className="text-xs font-normal text-[var(--muted-foreground)]">({tracesTotal})</span>
            </h3>
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-[var(--muted-foreground)] border-b border-[var(--border)]">
                    <th className="text-left py-2 pr-3 font-medium">Source</th>
                    <th className="text-right py-2 px-3 font-medium">Requests</th>
                    <th className="text-right py-2 px-3 font-medium">Input</th>
                    <th className="text-right py-2 px-3 font-medium">Output</th>
                    <th className="text-right py-2 pl-3 font-medium">Last active</th>
                  </tr>
                </thead>
                <tbody>
                  {traces.map((t) => (
                    <tr key={t.id} className="border-b border-[var(--border)] last:border-0">
                      <td className="py-2 pr-3 text-[var(--foreground)] font-mono truncate max-w-[140px]">{t.source || t.id.slice(0, 8)}</td>
                      <td className="py-2 px-3 text-right text-[var(--muted-foreground)]">{t.requestCount}</td>
                      <td className="py-2 px-3 text-right text-[var(--muted-foreground)]">{formatTokens(t.totalInputTokens)}</td>
                      <td className="py-2 px-3 text-right text-[var(--muted-foreground)]">{formatTokens(t.totalOutputTokens)}</td>
                      <td className="py-2 pl-3 text-right text-[var(--muted-foreground)] whitespace-nowrap">{new Date(t.updatedAt).toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {totalPages > 1 && (
              <div className="flex items-center justify-between mt-3 pt-3 border-t border-[var(--border)]">
                <span className="text-xs text-[var(--muted-foreground)]">
                  Page {tracesPage + 1} of {totalPages}
                </span>
                <div className="flex gap-2">
                  <button
                    onClick={() => setTracesPage((p) => Math.max(0, p - 1))}
                    disabled={tracesPage === 0}
                    className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs text-[var(--foreground)] border border-[var(--border)] hover:bg-[var(--accent)] disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    <ChevronLeft size={12} />
                    Prev
                  </button>
                  <button
                    onClick={() => setTracesPage((p) => Math.min(totalPages - 1, p + 1))}
                    disabled={tracesPage >= totalPages - 1}
                    className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs text-[var(--foreground)] border border-[var(--border)] hover:bg-[var(--accent)] disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    Next
                    <ChevronRight size={12} />
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    )
  } else {
    mainContent = (
      <span className="text-[var(--muted-foreground)]">
        Select or create a sandbox
      </span>
    )
  }

  return (
    <div className="flex h-screen">
      <SandboxList
        workspaces={workspaces}
        setWorkspaces={setWorkspaces}
        selectedWorkspaceId={selectedWorkspaceId}
        onSelectWorkspace={handleSelectWorkspace}
        sandboxes={sandboxes}
        setSandboxes={setSandboxes}
        activeSandboxId={activeSandboxId}
        onSelectSandbox={setActiveSandboxId}
        onRefreshSandboxes={refreshSandboxes}
        creating={creating}
        setCreating={setCreating}
        user={user}
        onLogout={handleLogout}
        onShowAdmin={user?.role === 'admin' ? () => setShowAdmin(true) : undefined}
      />
      <div className="flex flex-1 items-center justify-center bg-[var(--background)]">
        {showAdmin ? (
          <AdminPanel onBack={() => setShowAdmin(false)} />
        ) : (
          mainContent
        )}
      </div>
    </div>
  )
}
