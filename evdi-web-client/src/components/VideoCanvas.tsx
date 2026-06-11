import { useEffect, useRef, useCallback } from 'react'
import { useWebRTC } from '../hooks/useWebRTC'
import type { MouseMovePayload, MouseButtonPayload, MouseWheelPayload, KeyPayload } from '../types/signaling'

interface Props {
  stream: MediaStream
}

export const VideoCanvas: React.FC<Props> = ({ stream }) => {
  const videoRef = useRef<HTMLVideoElement>(null)
  const { sendInputMessage } = useWebRTC()

  useEffect(() => {
    const video = videoRef.current
    if (video && stream) {
      video.srcObject = stream
      const p = video.play()
      if (p) p.catch((err) => { if (err.name !== 'AbortError') console.warn('[VideoCanvas] play() failed:', err) })
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
    sendInputMessage('input.mouse_move', { x, y, display_id: 0 } satisfies MouseMovePayload)
  }, [getRelativePos, sendInputMessage])

  const handleMouseDown = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    sendInputMessage('input.mouse_button', { button: e.button + 1, action: 'down', x, y } satisfies MouseButtonPayload)
  }, [getRelativePos, sendInputMessage])

  const handleMouseUp = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    sendInputMessage('input.mouse_button', { button: e.button + 1, action: 'up', x, y } satisfies MouseButtonPayload)
  }, [getRelativePos, sendInputMessage])

  const handleWheel = useCallback((e: React.WheelEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    sendInputMessage('input.mouse_wheel', { delta_x: e.deltaX, delta_y: e.deltaY, x, y } satisfies MouseWheelPayload)
  }, [getRelativePos, sendInputMessage])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    e.preventDefault()
    sendInputMessage('input.key', { keycode: e.keyCode, action: 'down', shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey } satisfies KeyPayload)
  }, [sendInputMessage])

  const handleKeyUp = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    e.preventDefault()
    sendInputMessage('input.key', { keycode: e.keyCode, action: 'up', shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey } satisfies KeyPayload)
  }, [sendInputMessage])

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
