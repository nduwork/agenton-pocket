// Wire frames, mirrored from internal/protocol: [1 type][4 session_id BE][4 len BE][payload].
export const TYPE_CONTROL = 1;
export const TYPE_OUTPUT = 2;

export function encodeControl(msg) {
  const payload = new TextEncoder().encode(JSON.stringify(msg));
  const buf = new Uint8Array(9 + payload.length);
  const dv = new DataView(buf.buffer);
  dv.setUint8(0, TYPE_CONTROL);
  dv.setUint32(1, 0);              // control uses session_id 0; ids ride in JSON
  dv.setUint32(5, payload.length);
  buf.set(payload, 9);
  return buf;
}

export function decodeFrame(data) {
  const dv = new DataView(data);
  return {
    type: dv.getUint8(0),
    sessionId: dv.getUint32(1),
    payload: new Uint8Array(data, 9, dv.getUint32(5)),
  };
}

export function parseControl(payload) {
  return JSON.parse(new TextDecoder().decode(payload));
}
