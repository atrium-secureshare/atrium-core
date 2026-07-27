import { Component, type ReactNode } from 'react'
import i18n from '@/i18n'
import { ErrorScreen } from './ErrorScreen'

// Last-resort catch for a render error. A class because React exposes error
// catching only through the class lifecycle; recovery is a full reload since the
// component tree that threw cannot be trusted to navigate in place.
export class ErrorBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false }

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true }
  }

  render() {
    if (!this.state.failed) return this.props.children
    return (
      <div className="flex min-h-svh items-center justify-center px-6">
        <ErrorScreen
          title={i18n.t('errorBoundary.title')}
          action={
            <a href="/" className="hover:underline">
              {i18n.t('errorBoundary.home')}
            </a>
          }
        />
      </div>
    )
  }
}
