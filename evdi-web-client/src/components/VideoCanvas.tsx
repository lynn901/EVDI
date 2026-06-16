import { useEffect, useRef, useCallback } from 'react'
import { useConnectionStore } from '../stores/connectionStore'
import type { MouseMovePayload, MouseButtonPayload, MouseWheelPayload, KeyPayload } from '../types/signaling'

interface Props {
  stream: MediaStream
}

export const VideoCanvas: React.FC<Props> = ({ stream }) => {
  const videoRef = useRef<HTMLVideoElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const sendInput = useConnectionStore((s) => s.sendInput)

  useEffect(() => {
    const video = videoRef.current
    if (video && stream) {
      video.srcObject = stream
      video.play().catch((err) => {
        if (err.name !== 'AbortError') console.warn('[VideoCanvas] play() failed:', err)
      })
    }
  }, [stream])

  // Register all native event listeners with proper preventDefault handling.
  // React synthetic events don't allow { passive: false } for wheel,
  // and some events (dblclick, drag, selectstart) aren't easily handled in React.
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const getPos = (e: MouseEvent | WheelEvent) => {
      const video = videoRef.current
      const rect = container.getBoundingClientRect()
      if (!video || video.videoWidth === 0) return { x: 0, y: 0 }
      const x = Math.round((e.offsetX / rect.width) * video.videoWidth)
      const y = Math.round((e.offsetY / rect.height) * video.videoHeight)
      return { x, y }
    }

    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const { x, y } = getPos(e)

      // Normalize deltas to "lines" unit so the agent can interpret them consistently.
      // Browser WheelEvent.deltaMode:
      //   0 = DOM_DELTA_PIXEL → typical values: ±100 per notch (Chrome/Windows) or ±4 (macOS)
      //   1 = DOM_DELTA_LINE  → typical values: ±3 per notch (Firefox/Linux)
      //   2 = DOM_DELTA_PAGE  → typically ±1 per notch
      //
      // We normalize everything to "lines" because xdotool click 4/5 ≈ one line of scroll.
      let normX = e.deltaX
      let normY = e.deltaY
      switch (e.deltaMode) {
        case 0: // DOM_DELTA_PIXEL → convert pixels to lines (≈1 line per 40px, matching typical OS settings)
          normX = e.deltaX / 40
          normY = e.deltaY / 40
          break
        case 1: // DOM_DELTA_LINE → already in lines
          break
        case 2: // DOM_DELTA_PAGE → one page ≈ 30 lines
          normX = e.deltaX * 30
          normY = e.deltaY * 30
          break
      }

      sendInput('input.mouse_wheel', { delta_x: normX, delta_y: normY, x, y, delta_mode: 1 } satisfies MouseWheelPayload)
    }

    const onMouseDown = (e: MouseEvent) => {
      e.preventDefault()
      const { x, y } = getPos(e)
      sendInput('input.mouse_button', { button: e.button + 1, action: 'down', x, y } satisfies MouseButtonPayload)
    }

    const onMouseUp = (e: MouseEvent) => {
      e.preventDefault()
      const { x, y } = getPos(e)
      sendInput('input.mouse_button', { button: e.button + 1, action: 'up', x, y } satisfies MouseButtonPayload)
    }

    const onMouseMove = (e: MouseEvent) => {
      const { x, y } = getPos(e)
      sendInput('input.mouse_move', { x, y, display_id: 0 } satisfies MouseMovePayload)
    }

    const onKeyDown = (e: KeyboardEvent) => {
      e.preventDefault()
      e.stopPropagation()
      sendInput('input.key', { keycode: e.keyCode, action: 'down', shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey, capsLock: e.getModifierState('CapsLock') } satisfies KeyPayload)
    }

    const onKeyUp = (e: KeyboardEvent) => {
      e.preventDefault()
      e.stopPropagation()
      sendInput('input.key', { keycode: e.keyCode, action: 'up', shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey, capsLock: e.getModifierState('CapsLock') } satisfies KeyPayload)
    }

    // Block all browser default behaviors that interfere with remote desktop
    const blockDefault = (e: Event) => e.preventDefault()
    const blockDefaultAndStop = (e: Event) => { e.preventDefault(); e.stopPropagation() }

    container.addEventListener('wheel', onWheel, { passive: false })
    container.addEventListener('mousedown', onMouseDown)
    container.addEventListener('mouseup', onMouseUp)
    container.addEventListener('mousemove', onMouseMove)
    container.addEventListener('keydown', onKeyDown)
    container.addEventListener('keyup', onKeyUp)
    container.addEventListener('contextmenu', blockDefault)
    container.addEventListener('dblclick', blockDefaultAndStop)
    container.addEventListener('dragstart', blockDefault)
    container.addEventListener('selectstart', blockDefault)

    return () => {
      container.removeEventListener('wheel', onWheel)
      container.removeEventListener('mousedown', onMouseDown)
      container.removeEventListener('mouseup', onMouseUp)
      container.removeEventListener('mousemove', onMouseMove)
      container.removeEventListener('keydown', onKeyDown)
      container.removeEventListener('keyup', onKeyUp)
      container.removeEventListener('contextmenu', blockDefault)
      container.removeEventListener('dblclick', blockDefaultAndStop)
      container.removeEventListener('dragstart', blockDefault)
      container.removeEventListener('selectstart', blockDefault)
    }
  }, [sendInput])

  // Auto-focus container on click for keyboard capture
  const handleClick = useCallback(() => {
    containerRef.current?.focus()
  }, [])

  return (
    <div
      ref={containerRef}
      style={{ width: '100%', height: '100%', position: 'relative', outline: 'none', userSelect: 'none', cursor: 'none' }}
      onClick={handleClick}
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
