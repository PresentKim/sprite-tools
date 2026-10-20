// 색상 유틸. 색은 리틀 엔디언 RGBA 를 담은 uint32 하나로 다룹니다 (Uint8ClampedArray 의 R,G,B,A 바이트 순서와 같음).

export const pack = (r, g, b, a = 255) => (((a << 24) | (b << 16) | (g << 8) | r) >>> 0);

export function unpack(c) {
  return { r: c & 255, g: (c >>> 8) & 255, b: (c >>> 16) & 255, a: c >>> 24 };
}

// 완전 투명한 픽셀은 RGB 값과 상관없이 같은 색으로 취급하기 위해 0 으로 맞춥니다.
export const norm = (c) => (c >>> 24 === 0 ? 0 : c);

const h2 = (n) => n.toString(16).padStart(2, '0');

// 알파가 255 면 #rrggbb, 아니면 #rrggbbaa
export function toHex(c) {
  const { r, g, b, a } = unpack(c);
  return `#${h2(r)}${h2(g)}${h2(b)}${a === 255 ? '' : h2(a)}`;
}

// #rgb, #rgba, #rrggbb, #rrggbbaa ('#' 는 생략 가능). 잘못된 값이면 null.
export function parseHex(s) {
  let t = String(s).trim().replace(/^#/, '');
  if (!/^[0-9a-fA-F]+$/.test(t)) return null;
  if (t.length === 3 || t.length === 4) t = [...t].map((ch) => ch + ch).join('');
  if (t.length !== 6 && t.length !== 8) return null;
  const v = (i) => parseInt(t.slice(i, i + 2), 16);
  return pack(v(0), v(2), v(4), t.length === 8 ? v(6) : 255);
}

export function css(c) {
  const { r, g, b, a } = unpack(c);
  return `rgba(${r},${g},${b},${(a / 255).toFixed(3)})`;
}

// 두 색의 채널별 최대 차이 (둘 다 완전 투명이면 0)
export function diff(a, b) {
  if (a >>> 24 === 0 && b >>> 24 === 0) return 0;
  return Math.max(
    Math.abs((a & 255) - (b & 255)),
    Math.abs(((a >>> 8) & 255) - ((b >>> 8) & 255)),
    Math.abs(((a >>> 16) & 255) - ((b >>> 16) & 255)),
    Math.abs((a >>> 24) - (b >>> 24)),
  );
}

// Uint8ClampedArray <-> base64 (Go 와 주고받는 픽셀 형식)
export function toBase64(bytes) {
  const CH = 0x8000;
  let s = '';
  for (let i = 0; i < bytes.length; i += CH) s += String.fromCharCode.apply(null, bytes.subarray(i, i + CH));
  return btoa(s);
}

export function fromBase64(b64, out) {
  const s = atob(b64);
  const n = Math.min(s.length, out.length);
  for (let i = 0; i < n; i++) out[i] = s.charCodeAt(i);
  return out;
}

// 비곱셈 RGBA 두 색의 source-over 합성 (src 를 dst 위에)
export function over(src, dst) {
  const sa = src >>> 24;
  if (sa === 255 || dst >>> 24 === 0) return src;
  if (sa === 0) return dst;
  const da = dst >>> 24;
  const oa = sa + da * (255 - sa) / 255;
  const ch = (s, d) => Math.round((s * sa + d * da * (255 - sa) / 255) / oa);
  return pack(ch(src & 255, dst & 255), ch((src >>> 8) & 255, (dst >>> 8) & 255), ch((src >>> 16) & 255, (dst >>> 16) & 255), Math.round(oa));
}
