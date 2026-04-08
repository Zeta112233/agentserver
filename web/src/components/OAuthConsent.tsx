import { useEffect, useState } from 'react'
import { listMyWorkspaces, submitOAuthConsent, type Workspace } from '../lib/api'

interface OAuthConsentProps {
  challenge: string
}

export function OAuthConsent({ challenge }: OAuthConsentProps) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [selected, setSelected] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    listMyWorkspaces()
      .then((ws) => {
        setWorkspaces(ws)
        if (ws.length === 1) setSelected(ws[0].id)
        setLoading(false)
      })
      .catch(() => {
        setError('Failed to load workspaces')
        setLoading(false)
      })
  }, [])

  const handleSubmit = async (action: 'accept' | 'deny') => {
    if (action === 'accept' && !selected) {
      setError('Please select a workspace')
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const { redirect_to } = await submitOAuthConsent(challenge, selected, action)
      window.location.href = redirect_to
    } catch {
      setError('Failed to submit. Please try again.')
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-[var(--muted-foreground)]">Loading...</div>
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="w-full max-w-md border border-[var(--border)] rounded-lg p-6 space-y-6">
        <div className="text-center">
          <h2 className="text-lg font-semibold">Agent requests access</h2>
          <p className="text-sm text-[var(--muted-foreground)] mt-1">
            Select a workspace for the agent to join
          </p>
        </div>

        {workspaces.length === 0 ? (
          <div className="text-center text-sm text-[var(--muted-foreground)]">
            No workspaces available. Contact your administrator.
          </div>
        ) : (
          <div className="space-y-2">
            {workspaces.map((ws) => (
              <label
                key={ws.id}
                className={`flex items-center gap-3 p-3 rounded-md border cursor-pointer transition-colors ${
                  selected === ws.id
                    ? 'border-[var(--primary)] bg-[var(--primary)]/5'
                    : 'border-[var(--border)] hover:border-[var(--muted-foreground)]'
                }`}
              >
                <input
                  type="radio"
                  name="workspace"
                  value={ws.id}
                  checked={selected === ws.id}
                  onChange={() => setSelected(ws.id)}
                  className="accent-[var(--primary)]"
                />
                <span className="text-sm font-medium">{ws.name}</span>
              </label>
            ))}
          </div>
        )}

        <div className="space-y-2 text-sm text-[var(--muted-foreground)]">
          <p className="font-medium text-[var(--foreground)]">Permissions requested:</p>
          <ul className="space-y-1 ml-2">
            <li>Register as local agent</li>
            <li>Receive and execute tasks</li>
          </ul>
        </div>

        {error && (
          <div className="text-sm text-red-500 text-center">{error}</div>
        )}

        <div className="flex gap-3 justify-end">
          <button
            onClick={() => handleSubmit('deny')}
            disabled={submitting}
            className="px-4 py-2 text-sm border border-[var(--border)] rounded-md hover:bg-[var(--muted)]"
          >
            Deny
          </button>
          <button
            onClick={() => handleSubmit('accept')}
            disabled={submitting || !selected}
            className="px-4 py-2 text-sm bg-[var(--primary)] text-[var(--primary-foreground)] rounded-md hover:opacity-90 disabled:opacity-50"
          >
            {submitting ? 'Authorizing...' : 'Allow & Join'}
          </button>
        </div>
      </div>
    </div>
  )
}
