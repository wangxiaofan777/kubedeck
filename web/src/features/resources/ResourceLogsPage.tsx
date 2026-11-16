import { Alert } from 'antd'
import LogViewer from '../logs/LogViewer'
import { useResourceDetail } from './ResourceDetailLayout'

export default function ResourceLogsPage() {
  const { cluster, namespace, name, containers } = useResourceDetail()
  if (!namespace) {
    return <Alert type="warning" showIcon message="Pod 缺少命名空间，无法读取日志。" />
  }
  return <LogViewer cluster={cluster} namespace={namespace} pod={name} containers={containers} />
}