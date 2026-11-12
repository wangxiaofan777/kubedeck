import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Input, Select, Space, Spin, Switch, Typography } from 'antd'
import { PauseCircleOutlined, PlayCircleOutlined } from '@ant-design/icons'
import { api, ApiError } from '../../lib/api'

interface Props {
  cluster: string
  namespace: string
  pod: string
  containers: string[]
}

/** LogViewer 支持容器切换、上一实例、跟随与暂停/恢复（暂停期间继续接收但不高频渲染）。 */
export default function LogViewer({ cluster, namespace, pod, containers }: Props) {
  const [container, setContainer] = useState(containers[0] ?? '')
  const [previous, setPrevious] = useState(false)
  const [follow, setFollow] = useState(true)
  const [paused, setPaused] = useState(false)
  const [lines, setLines] = useState('')
  const [error, setError] = useState<unknown>()
  const [loading, setLoading] = useState(true)
  const [truncated, setTruncated] = useState(false)
  const [ended, setEnded] = useState(false)
  const [filter, setFilter] = useState('')

  const pausedRef = useRef(false)
  const bufferRef = useRef('')
  const preRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    pausedRef.current = paused
    if (!paused) {
      if (bufferRef.current) {
        setLines((current) => current + bufferRef.current)
        bufferRef.current = ''
      }
      const node = preRef.current
      if (node) node.scrollTop = node.scrollHeight
    }
  }, [paused])

  useEffect(() => {
    const controller = new AbortController()
    let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
    setLoading(true)
    setError(undefined)
    setLines('')
    setTruncated(false)
    setEnded(false)
    bufferRef.current = ''

    const url = api.logURL({
      cluster,
      namespace,
      name: pod,
      container: container || undefined,
      tailLines: 500,
      previous,
      follow,
      timestamps: true,
    })

    const run = async () => {
      try {
        const response = await api.logStream(url, controller.signal)
        if (response.headers.get('X-Log-Truncated') === 'true') setTruncated(true)
        if (!response.body) {
          setLines(await response.text())
          setLoading(false)
          return
        }
        reader = response.body.getReader()
        const decoder = new TextDecoder()
        while (true) {
          const { value, done } = await reader.read()
          if (done) break
          const text = decoder.decode(value, { stream: true })
          if (pausedRef.current) bufferRef.current += text
          else setLines((current) => current + text)
        }
        const rest = decoder.decode()
        if (rest) {
          if (pausedRef.current) bufferRef.current += rest
          else setLines((current) => current + rest)
        }
        setEnded(true)
        setLoading(false)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err)
        setLoading(false)
      }
    }
    void run()

    return () => {
      controller.abort()
      void reader?.cancel()
    }
  }, [cluster, namespace, pod, container, previous, follow])

  useEffect(() => {
    if (!paused) {
      const node = preRef.current
      if (node) node.scrollTop = node.scrollHeight
    }
  }, [lines, paused])

  const keyword = filter.trim().toLowerCase()
  const displayed = keyword
    ? lines
        .split('\n')
        .filter((line) => line.toLowerCase().includes(keyword))
        .join('\n')
    : lines
  const matchedLines = keyword ? displayed.split('\n').filter(Boolean).length : 0

  return (
    <div>
      <Space wrap style={{ marginBottom: 12 }}>
        <Select
          aria-label="容器"
          value={container}
          onChange={setContainer}
          style={{ width: 200 }}
          options={containers.map((name) => ({ value: name, label: name }))}
        />
        <Space size={4}>
          <Typography.Text>上一实例</Typography.Text>
          <Switch checked={previous} onChange={setPrevious} />
        </Space>
        <Space size={4}>
          <Typography.Text>跟随</Typography.Text>
          <Switch checked={follow} onChange={setFollow} />
        </Space>
        <Button
          icon={paused ? <PlayCircleOutlined /> : <PauseCircleOutlined />}
          onClick={() => setPaused((value) => !value)}
        >
          {paused ? '恢复' : '暂停'}
        </Button>
        <Input
          allowClear
          aria-label="过滤日志"
          placeholder="过滤日志"
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          style={{ width: 180 }}
        />
        {keyword && <Typography.Text type="secondary">{matchedLines} 行匹配</Typography.Text>}
        {follow && !loading && ended && (
          <Typography.Text type="secondary">日志流已结束</Typography.Text>
        )}
      </Space>

      {previous && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="当前展示的是上一次容器实例的日志"
        />
      )}
      {truncated && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="日志已超过单次上限（5 MB），仅显示前 5 MB。请缩小时间范围或使用 tailLines。"
        />
      )}
      {paused && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="已暂停渲染，日志仍在后台接收；恢复后不会丢失内容。"
        />
      )}
      <ErrorAlertInline error={error} />
      {loading && <Spin style={{ marginBottom: 8 }} />}
      <pre
        ref={preRef}
        aria-label="Pod 日志"
        style={{
          margin: 0,
          padding: 12,
          background: '#0b1020',
          color: '#d7e0ff',
          borderRadius: 6,
          minHeight: 300,
          maxHeight: 'calc(100vh - 360px)',
          overflow: 'auto',
          fontSize: 12,
          lineHeight: 1.55,
          whiteSpace: 'pre-wrap',
        }}
      >
        {displayed || (loading ? '' : keyword ? '没有匹配的日志' : '暂无日志输出')}
      </pre>
    </div>
  )
}

function ErrorAlertInline({ error }: { error: unknown }) {
  if (!error) return null
  const message =
    error instanceof ApiError
      ? `${error.message}${error.requestId ? `（${error.requestId}）` : ''}`
      : error instanceof Error
        ? error.message
        : '日志读取失败'
  return <Alert type="error" showIcon style={{ marginBottom: 12 }} message={message} />
}
