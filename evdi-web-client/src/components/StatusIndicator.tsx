import { Tag } from 'antd'
import type { ConnectionState } from '../types/signaling'

const stateConfig: Record<ConnectionState, { color: string; label: string }> = {
  disconnected: { color: 'default', label: '未连接' },
  connecting: { color: 'processing', label: '连接中' },
  connected: { color: 'success', label: '已连接' },
  error: { color: 'error', label: '错误' },
}

interface Props {
  state: ConnectionState
}

export const StatusIndicator: React.FC<Props> = ({ state }) => {
  const config = stateConfig[state]
  return <Tag color={config.color}>{config.label}</Tag>
}
