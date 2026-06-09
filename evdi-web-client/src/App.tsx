import { ConfigProvider, theme } from 'antd'
import { ControlPanel } from './components/ControlPanel'
import { VideoCanvas } from './components/VideoCanvas'
import { useConnectionStore } from './stores/connectionStore'

function App() {
  const { connectionState, mediaStream } = useConnectionStore()

  return (
    <ConfigProvider theme={{ algorithm: theme.darkAlgorithm }}>
      <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: '#1a1a2e' }}>
        <ControlPanel />
        <div style={{ flex: 1, position: 'relative' }}>
          {connectionState === 'connected' && mediaStream ? (
            <VideoCanvas stream={mediaStream} />
          ) : (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#888' }}>
              {connectionState === 'connecting' ? '正在连接...' : '未连接'}
            </div>
          )}
        </div>
      </div>
    </ConfigProvider>
  )
}

export default App
