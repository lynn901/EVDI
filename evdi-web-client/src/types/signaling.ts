export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'

export interface SignalingMessage {
  type: 'offer' | 'answer' | 'ice' | 'ping' | 'pong' | 'input.mouse_move' | 'input.mouse_button' | 'input.mouse_wheel' | 'input.key' | 'clipboard.push' | 'ctrl.resize'
  data: unknown
}

export interface SDPPayload {
  sdp: string
  type: string
}

export interface ICEPayload {
  candidate: string
  sdpMid: string
  sdpMLineIndex: number
}

export interface DataChannelMessage {
  v: number
  type: string
  ts: number
  seq: number
  payload: unknown
}

export interface MouseMovePayload {
  x: number
  y: number
  display_id: number
}

export interface MouseButtonPayload {
  button: number
  action: 'down' | 'up'
  x: number
  y: number
}

export interface MouseWheelPayload {
  delta_x: number
  delta_y: number
  x: number
  y: number
}

export interface KeyPayload {
  keycode: number
  action: 'down' | 'up'
  shift: boolean
  ctrl: boolean
  alt: boolean
}

export interface ClipboardPayload {
  data: string
  mime_type: string
}

export interface ResizePayload {
  width: number
  height: number
}
