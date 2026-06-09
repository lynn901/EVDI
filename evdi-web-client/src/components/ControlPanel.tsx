import { Input, Button, Space, Typography } from 'antd'
import { useConnectionStore } from '../stores/connectionStore'
import { useWebRTC } from '../hooks/useWebRTC'
import { StatusIndicator } from './StatusIndicator'

const { Text } = Typography

export const ControlPanel: React.FC = () => {
  const { agentAddress, connectionState, setAgentAddress } = useConnectionStore()
  const { connect, disconnect } = useWebRTC()

  const isConnecting = connectionState === 'connecting'
  const isConnected = connectionState === 'connected'

  return (
    <div style={{ padding: '12px 16px', display: 'flex', alignItems: 'center', gap: 12, borderBottom: '1px solid #333' }}>
      <Text style={{ color: '#ccc' }}>EVDI</Text>
      <StatusIndicator state={connectionState} />
      <Input
        size="small"
        value={agentAddress}
        onChange={(e) => setAgentAddress(e.target.value)}
        disabled={isConnected || isConnecting}
        style={{ width: 320 }}
        placeholder="ws://localhost:8080/ws"
      />
      <Space>
        {!isConnected ? (
          <Button type="primary" size="small" onClick={connect} loading={isConnecting} disabled={isConnecting}>
            连接
          </Button>
        ) : (
          <Button danger size="small" onClick={disconnect}>
            断开
          </Button>
        )}
      </Space>
    </div>
  )
}
