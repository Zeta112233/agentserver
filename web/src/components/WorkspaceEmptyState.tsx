import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { MessageSquare, Box, Play, Monitor, Rocket, Check, ArrowRight } from 'lucide-react'
import {
  listWorkspaceIMChannels,
  getModelserverStatus,
  type IMChannel,
  type ModelserverStatus,
  type Sandbox,
} from '../lib/api'

interface WorkspaceEmptyStateProps {
  workspaceId: string
  sandboxes: Sandbox[]
}

export function WorkspaceEmptyState({ workspaceId, sandboxes }: WorkspaceEmptyStateProps) {
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

  const hasIM = imChannels.length > 0
  const hasMS = msStatus?.connected === true
  const nanoclawWithIM = sandboxes.find((s) => s.type === 'nanoclaw' && s.im_bindings && s.im_bindings.length > 0)
  const hasNanoclawIM = !!nanoclawWithIM
  const localSandboxes = sandboxes.filter((s) => s.is_local)
  const hasLocal = localSandboxes.length > 0
  const allDone = hasIM && hasMS && hasNanoclawIM && hasLocal

  const providerLabel = (p: string) =>
    p === 'telegram' ? 'Telegram' : p === 'matrix' ? 'Matrix' : p === 'weixin' ? 'WeChat' : p

  const steps: {
    icon: React.ReactNode
    title: string
    done: boolean
    description: string
    action?: { label: string; onClick: () => void }
  }[] = [
    {
      icon: <MessageSquare size={14} />,
      title: 'Connect IM Channel',
      done: hasIM,
      description: hasIM
        ? `${imChannels.length} channel${imChannels.length > 1 ? 's' : ''}: ${[...new Set(imChannels.map((ch) => providerLabel(ch.provider)))].join(', ')}`
        : 'Link WeChat, Telegram, or Matrix to receive messages',
      action: { label: hasIM ? 'Manage' : 'Configure', onClick: goToSettings },
    },
    {
      icon: <Box size={14} />,
      title: 'Connect ModelServer',
      done: hasMS,
      description: hasMS
        ? `Connected to ${msStatus!.project_name}`
        : 'Connect a ModelServer project for LLM inference',
      action: { label: hasMS ? 'Manage' : 'Connect', onClick: goToSettings },
    },
    {
      icon: <Play size={14} />,
      title: 'Start Nanoclaw Sandbox',
      done: hasNanoclawIM,
      description: hasNanoclawIM
        ? `${nanoclawWithIM!.name} bound to IM`
        : 'Create a nanoclaw sandbox and bind it to an IM channel',
      action: hasIM ? undefined : { label: 'Set up IM first', onClick: goToSettings },
    },
    {
      icon: <Monitor size={14} />,
      title: 'Connect Local Sandboxes',
      done: hasLocal,
      description: hasLocal
        ? `${localSandboxes.length} local sandbox${localSandboxes.length > 1 ? 'es' : ''} connected`
        : 'Run agentserver-agent locally to connect your dev environment',
    },
    {
      icon: <Rocket size={14} />,
      title: 'Start Working!',
      done: allDone,
      description: allDone
        ? 'Everything is set up — select a sandbox to begin'
        : 'Complete the steps above to get started',
    },
  ]

  return (
    <div className="flex flex-col items-center justify-center gap-6 h-full">
      <div className="text-center">
        <h2 className="text-base font-semibold text-[var(--foreground)] mb-1">Getting Started</h2>
        <p className="text-sm text-[var(--muted-foreground)]">Set up your workspace to start using agents</p>
      </div>

      <div className="w-full max-w-md">
        <div className="rounded-lg border border-[var(--border)] bg-[var(--card)]">
          <div className="flex flex-col">
            {steps.map((step, i) => (
              <div key={i} className={`px-4 py-3 ${i > 0 ? 'border-t border-[var(--border)]' : ''}`}>
                <div className="flex items-start gap-3">
                  {/* Step number / check */}
                  <div className={`flex items-center justify-center w-6 h-6 rounded-full shrink-0 mt-0.5 text-xs font-medium ${
                    step.done
                      ? 'bg-green-500/15 text-green-400'
                      : 'bg-[var(--muted)] text-[var(--muted-foreground)]'
                  }`}>
                    {step.done ? <Check size={12} strokeWidth={3} /> : i + 1}
                  </div>

                  {/* Content */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={step.done ? 'text-[var(--muted-foreground)]' : 'text-[var(--foreground)]'}>
                        {step.icon}
                      </span>
                      <span className={`text-sm font-medium ${step.done ? 'text-[var(--muted-foreground)]' : 'text-[var(--foreground)]'}`}>
                        {step.title}
                      </span>
                    </div>
                    <p className={`text-xs mt-0.5 ${step.done ? 'text-green-400' : 'text-[var(--muted-foreground)]'}`}>
                      {loading && i < 2 ? '\u00A0' : step.description}
                    </p>
                  </div>

                  {/* Action */}
                  {step.action && !step.done && (
                    <button
                      onClick={step.action.onClick}
                      className="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)] hover:bg-[var(--secondary)] transition-colors shrink-0"
                    >
                      {step.action.label}
                      <ArrowRight size={12} />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
