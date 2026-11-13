import { useCallback, useEffect, useRef, useState } from 'react'
import { Spin } from 'antd'
import Editor, { loader } from '@monaco-editor/react'
import type { editor as monacoEditor } from 'monaco-editor'
import { monaco } from '../../lib/monaco'

loader.config({ monaco })

interface Props {
  value: string
  onChange?: (value: string) => void
  readOnly?: boolean
  height?: number
}

/**
 * YamlCodeEditor 是 Monaco 的展示层：负责挂载、尺寸校正与只读状态。
 * YamlEditor（编辑已有对象）与新建弹窗共用，避免布局 workaround 出现两份。
 */
export default function YamlCodeEditor({ value, onChange, readOnly = false, height = 420 }: Props) {
  const [hostReady, setHostReady] = useState(false)
  const editorWrapperRef = useRef<HTMLDivElement | null>(null)
  const editorRef = useRef<monacoEditor.IStandaloneCodeEditor>()
  const nodeObserverRef = useRef<ResizeObserver>()
  const wrapperObserverRef = useRef<ResizeObserver>()

  // Ant Tabs 切换或受控 value 更新时，Monaco 偶尔会按 0 尺寸布局并塌缩成 5×5。
  // 只要外层容器仍有宽度，就主动重新布局，保证后续输入可见。
  const ensureLayout = useCallback(() => {
    const instance = editorRef.current
    const wrapper = editorWrapperRef.current
    if (!instance || !wrapper || wrapper.clientWidth === 0) return
    const node = instance.getDomNode()
    if (node && node.clientWidth <= 10) instance.layout()
  }, [])

  const scheduleLayout = useCallback(() => {
    window.setTimeout(ensureLayout, 0)
  }, [ensureLayout])

  const attachWrapper = useCallback(
    (node: HTMLDivElement | null) => {
      wrapperObserverRef.current?.disconnect()
      editorWrapperRef.current = node
      if (!node) return
      if (node.clientWidth > 0) setHostReady(true)
      const observer = new ResizeObserver(() => {
        if (node.clientWidth > 0) setHostReady(true)
        scheduleLayout()
      })
      observer.observe(node)
      wrapperObserverRef.current = observer
    },
    [scheduleLayout],
  )

  useEffect(() => {
    const timer = window.setTimeout(ensureLayout, 0)
    return () => window.clearTimeout(timer)
  }, [value, ensureLayout])

  useEffect(() => () => nodeObserverRef.current?.disconnect(), [])

  // 标签页切换动画期间容器尺寸会短暂为 0，Monaco 可能先按 5×5 完成布局，
  // 且不再收到后续尺寸变化。挂载后的一小段时间内持续校正，等动画稳定即可。
  useEffect(() => {
    const started = Date.now()
    const timer = window.setInterval(() => {
      ensureLayout()
      if (Date.now() - started > 2000) window.clearInterval(timer)
    }, 100)
    return () => window.clearInterval(timer)
  }, [ensureLayout])

  return (
    <div ref={attachWrapper}>
      {hostReady ? (
        <Editor
          height={`${height}px`}
          language="yaml"
          value={value}
          onMount={(instance) => {
            editorRef.current = instance
            scheduleLayout()
            const node = instance.getDomNode()
            if (node) {
              nodeObserverRef.current?.disconnect()
              const observer = new ResizeObserver(() => {
                scheduleLayout()
              })
              observer.observe(node)
              nodeObserverRef.current = observer
            }
          }}
          onChange={(next) => onChange?.(next ?? '')}
          options={{
            readOnly,
            minimap: { enabled: false },
            fontSize: 13,
            scrollBeyondLastLine: false,
            automaticLayout: true,
            tabSize: 2,
          }}
        />
      ) : (
        <div style={{ height, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Spin />
        </div>
      )}
    </div>
  )
}