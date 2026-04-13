import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { MessageSquare, Box, ArrowRight } from 'lucide-react'
import {
  listWorkspaceIMChannels,
  getModelserverStatus,
  type IMChannel,
  type ModelserverStatus,
} from '../lib/api'

interface WorkspaceEmptyStateProps {
  workspaceId: string
}

export function WorkspaceEmptyState({ workspaceId }: WorkspaceEmptyStateProps) {
  const navigate = useNavigate()
  const [imChannels, setImChannels] = useState<IMChannel[]>([])
  const [msStatus, setMsStatus] = useState<ModelserverStatus | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    Promise.all([
      listWorkspaceIMChannels(workspaceId).then((r) => setImChannels(r.channels || [])).catch(() => setImChannels([])),
      getModelserverStatus(workspaceId).then(setMsStatus).catch(() => setMsStatus({ connected: false })),
    ]).finally(() => setLoading(false))
  }, [workspaceId])

  const goToSettings = () => navigate('/workspaces?tab=settings')

  const providerLabel = (p: string) =>
    p === 'telegram' ? 'Telegram' : p === 'matrix' ? 'Matrix' : 'WeChat'

  const imStatusText = () => {
    if (imChannels.length === 0) return 'No channels configured'
    const providers = [...new Set(imChannels.map((ch) => providerLabel(ch.provider)))]
    return `${imChannels.length} channel${imChannels.length > 1 ? 's' : ''}: ${providers.join(', ')}`
  }

  const msStatusText = () => {
    if (!msStatus?.connected) return 'Not connected'
    return `Connected to ${msStatus.project_name}`
  }

  return (
    <div className="flex flex-col items-center justify-center gap-6 h-full">
      <span className="text-[var(--muted-foreground)]">Select or create a sandbox</span>

      <div className="w-full max-w-sm">
        <div className="rounded-lg border border-[var(--border)] bg-[var(--card)]">
          <div className="px-4 py-3 border-b border-[var(--border)]">
            <span className="text-xs font-medium text-[var(--muted-foreground)] uppercase tracking-wide">Quick Setup</span>
          </div>
          <div className="flex flex-col divide-y divide-[var(--border)]">
            {/* IM Channels */}
            <div className="px-4 py-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2.5">
                  <div className={`flex items-center justify-center w-7 h-7 rounded-md ${imChannels.length > 0 ? 'bg-green-500/10' : 'bg-[var(--muted)]'}`}>
                    <MessageSquare size={14} className={imChannels.length > 0 ? 'text-green-400' : 'text-[var(--muted-foreground)]'} />
                  </div>
                  <div>
                    <div className="text-sm font-medium text-[var(--foreground)]">IM Channels</div>
                    <div className={`text-xs ${imChannels.length > 0 ? 'text-green-400' : 'text-[var(--muted-foreground)]'}`}>
                      {loading ? '\u00A0' : imStatusText()}
                    </div>
                  </div>
                </div>
                <button
                  onClick={goToSettings}
                  className="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)] hover:bg-[var(--secondary)] transition-colors"
                >
                  {imChannels.length > 0 ? 'Manage' : 'Configure'}
                  <ArrowRight size={12} />
                </button>
              </div>
            </div>

            {/* ModelServer */}
            <div className="px-4 py-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2.5">
                  <div className={`flex items-center justify-center w-7 h-7 rounded-md ${msStatus?.connected ? 'bg-blue-500/10' : 'bg-[var(--muted)]'}`}>
                    <Box size={14} className={msStatus?.connected ? 'text-blue-400' : 'text-[var(--muted-foreground)]'} />
                  </div>
                  <div>
                    <div className="text-sm font-medium text-[var(--foreground)]">ModelServer</div>
                    <div className={`text-xs ${msStatus?.connected ? 'text-blue-400' : 'text-[var(--muted-foreground)]'}`}>
                      {loading ? '\u00A0' : msStatusText()}
                    </div>
                  </div>
                </div>
                <button
                  onClick={goToSettings}
                  className="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)] hover:bg-[var(--secondary)] transition-colors"
                >
                  {msStatus?.connected ? 'Manage' : 'Connect'}
                  <ArrowRight size={12} />
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
