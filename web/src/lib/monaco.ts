import * as monaco from 'monaco-editor'
import { configureMonacoYaml } from 'monaco-yaml'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'

// 本地加载 worker，避免 @monaco-editor/react 默认从 CDN 拉取（CSP 只允许同源）。
self.MonacoEnvironment = {
  getWorker(_moduleId: string, label: string) {
    if (label === 'yaml') return new YamlWorker()
    return new EditorWorker()
  },
}

// KubeDeck 的基础 Kubernetes Schema：约束顶层字段与常见类型，
// 让 YAML 编辑器能在输入阶段发现拼写错误和类型错误。
const kubernetesSchema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  required: ['apiVersion', 'kind', 'metadata'],
  additionalProperties: false,
  properties: {
    apiVersion: { type: 'string', minLength: 1 },
    kind: {
      type: 'string',
      enum: [
        'Pod',
        'Deployment',
        'StatefulSet',
        'DaemonSet',
        'ReplicaSet',
        'Job',
        'CronJob',
        'Service',
        'Ingress',
        'ConfigMap',
        'Secret',
        'PersistentVolume',
        'PersistentVolumeClaim',
        'Namespace',
        'Node',
        'Event',
      ],
    },
    metadata: {
      type: 'object',
      properties: {
        name: { type: 'string' },
        namespace: { type: 'string' },
        labels: { type: 'object', additionalProperties: { type: 'string' } },
        annotations: { type: 'object', additionalProperties: { type: 'string' } },
        resourceVersion: { type: 'string' },
        generation: { type: 'integer' },
      },
    },
    spec: {
      type: 'object',
      properties: {
        replicas: { type: 'integer', minimum: 0 },
        paused: { type: 'boolean' },
        selector: { type: 'object' },
        template: { type: 'object' },
        containers: { type: 'array', items: { type: 'object' } },
        initContainers: { type: 'array', items: { type: 'object' } },
        ports: { type: 'array', items: { type: 'object' } },
        type: { type: 'string' },
        serviceName: { type: 'string' },
        schedule: { type: 'string' },
        rules: { type: 'array', items: { type: 'object' } },
        tls: { type: 'array', items: { type: 'object' } },
        volumeName: { type: 'string' },
        storageClassName: { type: 'string' },
      },
    },
    status: { type: 'object' },
    data: { type: 'object' },
    binaryData: { type: 'object' },
    stringData: { type: 'object' },
    type: { type: 'string' },
    immutable: { type: 'boolean' },
  },
}

configureMonacoYaml(monaco, {
  enableSchemaRequest: false,
  hover: true,
  completion: true,
  validate: true,
  format: {},
  schemas: [
    {
      uri: 'inmemory://kubedeck/kubernetes.json',
      fileMatch: ['*'],
      schema: kubernetesSchema,
    },
  ],
})

export { monaco }
