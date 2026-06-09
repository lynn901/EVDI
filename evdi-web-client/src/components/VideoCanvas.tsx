import { useEffect, useRef, useCallback } from 'react'
import { useWebRTC } from '../hooks/useWebRTC'
import type { MouseMovePayload, MouseButtonPayload, MouseWheelPayload, KeyPayload } from '../types/signaling'

interface Props {
  stream: MediaStream
}

export const VideoCanvas: React.FC<Props> = ({ stream }) => {
  const videoRef = useRef<HTMLVideoElement>(null)
  const { sendDataChannelMessage } = useWebRTC()

  useEffect(() => {
    if (videoRef.current && stream) {
      videoRef.current.srcObject = stream
    }
  }, [stream])

  const getRelativePos = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const video = videoRef.current
    if (!video) return { x: e.nativeEvent.offsetX, y: e.nativeEvent.offsetY }

    const videoRatio = video.videoWidth / video.videoHeight
    const containerRatio = rect.width / rect.height

    let renderWidth: number, renderHeight: number, offsetX: number, offsetY: number
    if (containerRatio > videoRatio) {
      renderHeight = rect.height
      renderWidth = renderHeight * videoRatio
      offsetX = (rect.width - renderWidth) / 2
      offsetY = 0
    } else {
      renderWidth = rect.width
      renderHeight = renderWidth / videoRatio
      offsetX = 0
      offsetY = (rect.height - renderHeight) / 2
    }

    return {
      x: Math.round(((e.nativeEvent.offsetX - offsetX) / renderWidth) * video.videoWidth),
      y: Math.round(((e.nativeEvent.offsetY - offsetY) / renderHeight) * video.videoHeight),
    }
  }, [])

  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    sendDataChannelMessage('control', 'input.mouse_move', { x, y, display_id: 0 } satisfies MouseMovePayload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleMouseDown = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    sendDataChannelMessage('control', 'input.mouse_button', { button: e.button + 1, action: 'down', x, y } satisfies MouseButtonPayload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleMouseUp = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    sendDataChannelMessage('control', 'input.mouse_button', { button: e.button + 1, action: 'up', x, y } satisfies MouseButtonPayload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleWheel = useCallback((e: React.WheelEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    sendDataChannelMessage('control', 'input.mouse_wheel', { delta_x: e.deltaX, delta_y: e.deltaY, x, y } satisfies MouseWheelPayload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    e.preventDefault()
    sendDataChannelMessage('control', 'input.key', { keycode: e.keyCode, action: 'down', shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey } satisfies KeyPayload)
  }, [sendDataChannelMessage])

  const handleKeyUp = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    e.preventDefault()
    sendDataChannelMessage('control', 'input.key', { keycode: e.keyCode, action: 'up', shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey } satisfies KeyPayload)
  }, [sendDataChannelMessage])

  return (
    <div
      style={{ width: '100%', height: '100%', position: 'relative', outline: 'none' }}
      onMouseMove={handleMouseMove}
      onMouseDown={handleMouseDown}
      onMouseUp={handleMouseUp}
      onWheel={handleWheel}
      onKeyDown={handleKeyDown}
      onKeyUp={handleKeyUp}
      tabIndex={0}
    >
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted
        style={{ width: '100%', height: '100%', objectFit: 'contain', pointerEvents: 'none' }}
      />
    </div>
  )
}
