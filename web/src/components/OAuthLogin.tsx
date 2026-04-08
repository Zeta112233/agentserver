import { useState } from 'react'
import { Login } from './Login'
import { submitOAuthLogin } from '../lib/api'

interface OAuthLoginProps {
  challenge: string
}

export function OAuthLogin({ challenge }: OAuthLoginProps) {
  const [error, setError] = useState('')

  const handleLoginSuccess = async () => {
    try {
      const { redirect_to } = await submitOAuthLogin(challenge)
      window.location.href = redirect_to
    } catch {
      setError('Failed to complete OAuth login. Please try again.')
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="w-full max-w-md space-y-4">
        <div className="text-center mb-6">
          <h2 className="text-lg font-semibold">Sign in to authorize agent</h2>
          <p className="text-sm text-[var(--muted-foreground)]">
            An agent is requesting access to your account
          </p>
        </div>
        {error && (
          <div className="text-sm text-red-500 text-center">{error}</div>
        )}
        <Login onSuccess={handleLoginSuccess} />
      </div>
    </div>
  )
}
