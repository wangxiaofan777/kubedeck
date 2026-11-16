import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Input, Select, Space, Spin, Switch, Typography } from 'antd'
import { PauseCircleOutlined, PlayCircleOutlined } from '@ant-design/icons'
import { api, ApiError } from '../../lib/api'

interface Props {
  cluster: string
  namespace: string
  pod: string
  containers: string[]
}

const MAX_LINES = 20_000
const MAX_RENDER = 5_000
const FLUSH_MS = 120

/** LogViewer 用环形缓冲限制内存，按帧批量渲染，并明确区分正常结束与异常断开。 */
export default function LogViewer({ cluster, namespace, pod, containers }: Props) {
  const [container, setContainer] = useState(containers[0] ?? '')
  const [previous, setPrevious] = useState(false)
  const [follow, setFollow] = useState(true)
  const [paused, setPaused] = useState(false)
  const [lines, setLines] = useState<string[]>([])
  const [dropped, setDropped] = useState(0)
  const [error, setError] = useState<unknown>()
  const [loading, setLoading] = useState(true)
  const [truncated, setTruncated] = useState(false)
  const [ended, setEnded] = useState(false)
  const [filter, setFilter] = useState('')
  const [stick, setStick] = useState(true)

  const pausedRef = useRef(false)
  const linesRef = useRef<string[]>([])
  const partialRef = useRef('')
  const droppedRef = useRef(0)
  const renderTimer = useRef<number>()
  const preRef = useRef<HTMLPreElement>(null)
  const stickRef = useRef(true)

  pausedRef.current = paused
  stickRef.current = stick

  // 容器列表变化（切换 Pod）时校正选择。
  useEffect(() => {
    if (!containers.includes(container)) setContainer(containers[0] ?? '')
  }, [containers, container])

  const scheduleRender = () => {
    if (renderTimer.current) return
    renderTimer.current = window.setTimeout(() => {
      renderTimer.current = undefined
      setLines([...linesRef.current])
      setDropped(droppedRef.current)
    }, FLUSH_MS)
  }

  const pushText = (text: string) => {
    const combined = partialRef.current + text
    const parts = combined.split('\n')
    partialRef.current = parts.pop() ?? ''
    if (parts.length === 0) return
    linesRef.current.push(...parts)
    const overflow = linesRef.current.length - MAX_LINES
    if (overflow > 0) {
      linesRef.current.splice(0, overflow)
      droppedRef.current += overflow
    }
    if (!pausedRef.current) scheduleRender()
  }

  useEffect(() => {
    const controller = new AbortController()
    let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
    linesRef.current = []
    partialRef.current = ''
    droppedRef.current = 0
    setLines([])
    setDropped(0)
    setLoading(true)
    setError(undefined)
    setTruncated(false)
    setEnded(false)
    setStick(true)

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
          const text = await response.text()
          if (text) pushText(text)
          setLoading(false)
          setEnded(true)
          scheduleRender()
          return
        }
        reader = response.body.getReader()
        const decoder = new TextDecoder()
        let first = true
        while (true) {
          const { value, done } = await reader.read()
          if (done) break
          if (first) {
            first = false
            setLoading(false)
          }
          pushText(decoder.decode(value, { stream: true }))
        }
        const rest = decoder.decode()
        if (rest) pushText(rest)
        if (partialRef.current) {
          pushText('\n')
        }
        setEnded(true)
        setLoading(false)
        scheduleRender()
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
      if (renderTimer.current) {
        window.clearTimeout(renderTimer.current)
        renderTimer.current = undefined
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, namespace, pod, container, previous, follow])

  useEffect(() => {
    if (!paused) scheduleRender()
  }, [paused])

  useEffect(() => {
    if (!follow || !stickRef.current) return
    const node = preRef.current
    if (node) node.scrollTop = node.scrollHeight
  }, [lines, follow])

  const keyword = filter.trim().toLowerCase()
  const displayed = useMemo(() => {
    const source = keyword
      ? lines.filter((line) => line.toLowerCase().includes(keyword))
      : lines
    if (source.length <= MAX_RENDER) return source.join('\n')
    return ['（仅显示最近 5000 行，完整内容请缩小时间范围或使用过滤）', ...source.slice(-MAX_RENDER)].join('\n')
  }, [lines, keyword])
  const matched = useMemo(
    () => (keyword ? lines.filter((line) => line.toLowerCase().includes(keyword)).length : 0),
    [lines, keyword],
  )

  const onScroll = () => {
    const node = preRef.current
    if (!node) return
    const atBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 40
    setStick(atBottom)
  }

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
          {paused ? '恢复渲染' : '暂停渲染'}
        </Button>
        {!stick && follow && (
          <Button
            size="small"
            onClick={() => {
              setStick(true)
              const node = preRef.current
              if (node) node.scrollTop = node.scrollHeight
            }}
          >
            回到底部
          </Button>
        )}
        <Input
          allowClear
          aria-label="过滤日志"
          placeholder="过滤日志（不区分大小写）"
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          style={{ width: 220 }}
        />
        {keyword && (
          <Typography.Text type="secondary">{matched} 行匹配</Typography.Text>
        )}
        {follow && !loading && ended && <Typography.Text type="secondary">日志流已结束</Typography.Text>}
      </Space>

      {!follow && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="已关闭跟随，只显示当前已接收的内容"
        />
      )}
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
          message="已暂停渲染，日志仍在后台接收（最多保留 2 万行）；恢复后显示最新内容。"
        />
      )}
      {dropped > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={`已丢弃最早的 ${dropped} 行，避免浏览器内存持续增长`}
        />
      )}
      {error ? <LogError error={error} /> : null}
      {loading && (
        <div style={{ padding: 12 }}>
          <Spin size="small" /> <span className="kd-muted">等待日志输出…</span>
        </div>
      )}
      <pre ref={preRef} className="kd-log" aria-label="Pod 日志" onScroll={onScroll}>
        {displayed || (!loading && !error ? (keyword ? '没有匹配的日志' : '暂无日志输出') : '')}
      </pre>
    </div>
  )
}

function LogError({ error }: { error: unknown }) {
  const message =
    error instanceof ApiError
      ? `${error.message}${error.requestId ? `（${error.requestId}）` : ''}`
      : error instanceof Error
        ? error.message
        : '日志读取失败'
  return <Alert type="error" showIcon style={{ marginBottom: 12 }} message={message} />
}