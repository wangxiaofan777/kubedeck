import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button, Result } from 'antd'

interface Props {
  children: ReactNode
}

interface State {
  error?: Error
}

/** ErrorBoundary 防止单个渲染异常导致整页白屏。 */
export default class ErrorBoundary extends Component<Props, State> {
  state: State = {}

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // 保留控制台日志，便于用户反馈时定位。
    console.error('render error', error, info.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <Result
        status="500"
        title="页面渲染出错"
        subTitle={this.state.error.message}
        extra={
          <Button type="primary" onClick={() => window.location.reload()}>
            重新加载
          </Button>
        }
      />
    )
  }
}