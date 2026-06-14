// Each preset is a complete program: it must define pixel(x, y).
window.PRESETS = [
  {
    name: "identity",
    code:
`// The starting point: copy every pixel unchanged.
// get(x, y) reads the ORIGINAL photo -> [r, g, b, a]  (0-255)
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  return [r, g, b];
}`
  },
  {
    name: "grayscale",
    code:
`function pixel(x, y) {
  const [r, g, b] = get(x, y);
  // luminance-weighted, so it looks right to the eye
  const v = 0.299 * r + 0.587 * g + 0.114 * b;
  return [v, v, v];
}`
  },
  {
    name: "invert",
    code:
`function pixel(x, y) {
  const [r, g, b] = get(x, y);
  return [255 - r, 255 - g, 255 - b];
}`
  },
  {
    name: "sepia",
    code:
`function pixel(x, y) {
  const [r, g, b] = get(x, y);
  return [
    0.393*r + 0.769*g + 0.189*b,
    0.349*r + 0.686*g + 0.168*b,
    0.272*r + 0.534*g + 0.131*b,
  ];
}`
  },
  {
    name: "posterize",
    code:
`// Crush each channel down to a few steps.
const STEPS = slider("steps", 5, 2, 12);
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const q = v => Math.round(v / 255 * (STEPS - 1)) / (STEPS - 1) * 255;
  return [q(r), q(g), q(b)];
}`
  },
  {
    name: "box blur",
    code:
`// Average a pixel with its 8 neighbours.
function pixel(x, y) {
  let r = 0, g = 0, b = 0;
  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      const p = get(x + dx, y + dy);
      r += p[0]; g += p[1]; b += p[2];
    }
  }
  return [r / 9, g / 9, b / 9];
}`
  },
  {
    name: "sharpen",
    code:
`// A 3x3 sharpening kernel pulling against the neighbours.
const K = [ 0, -1,  0,
           -1,  5, -1,
            0, -1,  0 ];
function pixel(x, y) {
  let r = 0, g = 0, b = 0, i = 0;
  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      const p = get(x + dx, y + dy), k = K[i++];
      r += p[0] * k; g += p[1] * k; b += p[2] * k;
    }
  }
  return [r, g, b];
}`
  },
  {
    name: "edges",
    code:
`// Sobel edge detection — uses the brightness of every neighbour.
const GX = [-1,0,1, -2,0,2, -1,0,1];
const GY = [-1,-2,-1, 0,0,0, 1,2,1];
function lum(p) { return 0.299*p[0] + 0.587*p[1] + 0.114*p[2]; }
function pixel(x, y) {
  let sx = 0, sy = 0, i = 0;
  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      const l = lum(get(x + dx, y + dy));
      sx += l * GX[i]; sy += l * GY[i]; i++;
    }
  }
  const m = Math.hypot(sx, sy);
  return [m, m, m];
}`
  },
  {
    name: "emboss",
    code:
`// Difference across the diagonal gives a 3D ridge.
function pixel(x, y) {
  const a = get(x - 1, y - 1);
  const b = get(x + 1, y + 1);
  const v = 128 + (b[0]-a[0]) + (b[1]-a[1]) + (b[2]-a[2]);
  return [v, v, v];
}`
  },
  {
    name: "chromatic",
    code:
`// Pull the red and blue channels apart for a lens-fringe look.
const SHIFT = slider("shift", 6, 0, 30);
function pixel(x, y) {
  const r = get(x - SHIFT, y)[0];
  const g = get(x, y)[1];
  const b = get(x + SHIFT, y)[2];
  return [r, g, b];
}`
  },
  {
    name: "pixelate",
    code:
`// Snap each pixel to the colour of its block's top-left corner.
const SIZE = slider("block", 12, 2, 60);
function pixel(x, y) {
  const bx = Math.floor(x / SIZE) * SIZE;
  const by = Math.floor(y / SIZE) * SIZE;
  return get(bx, by);
}`
  },
  {
    name: "ripple",
    code:
`// Sample from a wobbling offset to warp the image.
function pixel(x, y) {
  const ox = Math.sin(y / 14) * 8;
  const oy = Math.cos(x / 14) * 8;
  return get(Math.round(x + ox), Math.round(y + oy));
}`
  },
  {
    name: "threshold",
    code:
`// Pure black & white at a brightness cutoff.
const T = slider("cutoff", 128, 0, 255);
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const lum = 0.299*r + 0.587*g + 0.114*b;
  const v = lum >= T ? 255 : 0;
  return [v, v, v];
}`
  },
  {
    name: "brightness",
    code:
`// Add a flat amount to every channel (negative darkens).
const AMOUNT = slider("amount", 45, -150, 150);
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  return [r + AMOUNT, g + AMOUNT, b + AMOUNT];
}`
  },
  {
    name: "contrast",
    code:
`// Push values away from mid-grey (C > 1 = more punch).
const C = slider("contrast", 1.6, 0, 3);
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const f = v => (v - 128) * C + 128;
  return [f(r), f(g), f(b)];
}`
  },
  {
    name: "saturation",
    code:
`// Pull each channel away from the pixel's own grey.
const S = slider("saturation", 1.9, 0, 4);
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const l = 0.299*r + 0.587*g + 0.114*b;
  const f = v => l + (v - l) * S;
  return [f(r), f(g), f(b)];
}`
  },
  {
    name: "hue rotate",
    code:
`// Spin the colour wheel while keeping brightness.
const DEG = slider("degrees", 90, 0, 360);
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const a = DEG * Math.PI / 180, c = Math.cos(a), s = Math.sin(a);
  return [
    r*(.213+c*.787-s*.213) + g*(.715-c*.715-s*.715) + b*(.072-c*.072+s*.928),
    r*(.213-c*.213+s*.143) + g*(.715+c*.285+s*.140) + b*(.072-c*.072-s*.283),
    r*(.213-c*.213-s*.787) + g*(.715-c*.715+s*.715) + b*(.072+c*.928+s*.072),
  ];
}`
  },
  {
    name: "gaussian",
    code:
`// Smooth 5x5 blur, weighted toward the centre.
const K = [ 1, 4, 6, 4, 1,
            4,16,24,16, 4,
            6,24,36,24, 6,
            4,16,24,16, 4,
            1, 4, 6, 4, 1 ];
function pixel(x, y) {
  let r = 0, g = 0, b = 0, i = 0;
  for (let dy = -2; dy <= 2; dy++) {
    for (let dx = -2; dx <= 2; dx++) {
      const p = get(x + dx, y + dy), k = K[i++];
      r += p[0]*k; g += p[1]*k; b += p[2]*k;
    }
  }
  return [r / 256, g / 256, b / 256];
}`
  },
  {
    name: "motion blur",
    code:
`// Smear each pixel into the ones to its left.
const LEN = slider("length", 14, 1, 40);
function pixel(x, y) {
  let r = 0, g = 0, b = 0;
  for (let d = 0; d < LEN; d++) {
    const p = get(x - d, y);
    r += p[0]; g += p[1]; b += p[2];
  }
  return [r / LEN, g / LEN, b / LEN];
}`
  },
  {
    name: "unsharp",
    code:
`// Sharpen by amplifying the gap from a blurred copy.
const AMOUNT = 1.4;
function blur(x, y) {
  let r = 0, g = 0, b = 0;
  for (let dy = -1; dy <= 1; dy++)
    for (let dx = -1; dx <= 1; dx++) {
      const p = get(x + dx, y + dy);
      r += p[0]; g += p[1]; b += p[2];
    }
  return [r/9, g/9, b/9];
}
function pixel(x, y) {
  const o = get(x, y), bl = blur(x, y);
  return [
    o[0] + (o[0] - bl[0]) * AMOUNT,
    o[1] + (o[1] - bl[1]) * AMOUNT,
    o[2] + (o[2] - bl[2]) * AMOUNT,
  ];
}`
  },
  {
    name: "dilate",
    code:
`// Keep the brightest neighbour — light bleeds outward.
function pixel(x, y) {
  let r = 0, g = 0, b = 0;
  for (let dy = -1; dy <= 1; dy++)
    for (let dx = -1; dx <= 1; dx++) {
      const p = get(x + dx, y + dy);
      if (p[0] > r) r = p[0];
      if (p[1] > g) g = p[1];
      if (p[2] > b) b = p[2];
    }
  return [r, g, b];
}`
  },
  {
    name: "erode",
    code:
`// Keep the darkest neighbour — dark areas grow.
function pixel(x, y) {
  let r = 255, g = 255, b = 255;
  for (let dy = -1; dy <= 1; dy++)
    for (let dx = -1; dx <= 1; dx++) {
      const p = get(x + dx, y + dy);
      if (p[0] < r) r = p[0];
      if (p[1] < g) g = p[1];
      if (p[2] < b) b = p[2];
    }
  return [r, g, b];
}`
  },
  {
    name: "outline",
    code:
`// Brightest minus darkest neighbour = clean outlines.
function pixel(x, y) {
  let mx = 0, mn = 255;
  for (let dy = -1; dy <= 1; dy++)
    for (let dx = -1; dx <= 1; dx++) {
      const p = get(x + dx, y + dy);
      const l = (p[0] + p[1] + p[2]) / 3;
      if (l > mx) mx = l;
      if (l < mn) mn = l;
    }
  const v = mx - mn;
  return [v, v, v];
}`
  },
  {
    name: "vignette",
    code:
`// Darken toward the corners by distance from centre.
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const cx = width / 2, cy = height / 2;
  const d = Math.hypot(x - cx, y - cy) / Math.hypot(cx, cy);
  const f = 1 - Math.pow(d, 2.2) * 0.9;
  return [r * f, g * f, b * f];
}`
  },
  {
    name: "duotone",
    code:
`// Map shadows->highlights onto a two-colour ramp.
const DARK  = [ 18,  28,  58];
const LIGHT = [240, 200, 150];
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const t = (0.299*r + 0.587*g + 0.114*b) / 255;
  return [
    DARK[0] + (LIGHT[0]-DARK[0]) * t,
    DARK[1] + (LIGHT[1]-DARK[1]) * t,
    DARK[2] + (LIGHT[2]-DARK[2]) * t,
  ];
}`
  },
  {
    name: "solarize",
    code:
`// Invert only the channels above a threshold (Sabattier).
const T = 128;
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const f = v => v < T ? v : 255 - v;
  return [f(r), f(g), f(b)];
}`
  },
  {
    name: "dither",
    code:
`// 1-bit image using an ordered 4x4 Bayer matrix.
const BAYER = [ 0, 8, 2,10,
               12, 4,14, 6,
                3,11, 1, 9,
               15, 7,13, 5 ];
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const l = (0.299*r + 0.587*g + 0.114*b) / 255;
  const t = (BAYER[(y & 3) * 4 + (x & 3)] + 0.5) / 16;
  const v = l > t ? 255 : 0;
  return [v, v, v];
}`
  },
  {
    name: "thermal",
    code:
`// Brightness mapped onto a heat-camera colour ramp.
const RAMP = [
  [  0,  0, 40], [  0, 40,180], [  0,200,200], [120,220, 40],
  [255,210,  0], [255, 40,  0], [255,255,255],
];
function pixel(x, y) {
  const [r, g, b] = get(x, y);
  const t = (0.299*r + 0.587*g + 0.114*b) / 255;
  const p = t * (RAMP.length - 1);
  const i = Math.min(RAMP.length - 2, Math.floor(p)), f = p - i;
  const a = RAMP[i], c = RAMP[i + 1];
  return [
    a[0] + (c[0]-a[0]) * f,
    a[1] + (c[1]-a[1]) * f,
    a[2] + (c[2]-a[2]) * f,
  ];
}`
  },
  {
    name: "frosted glass",
    code:
`// Sample a random nearby pixel — like looking through glass.
const R = 4;
function pixel(x, y) {
  const dx = Math.round((Math.random()*2 - 1) * R);
  const dy = Math.round((Math.random()*2 - 1) * R);
  return get(x + dx, y + dy);
}`
  },
  {
    name: "swirl",
    code:
`// Rotate the sampling point more the closer it is to centre.
const STRENGTH = slider("strength", 3.2, 0, 8);
const RADIUS = slider("radius", 240, 20, 500);
function pixel(x, y) {
  const cx = width / 2, cy = height / 2;
  let dx = x - cx, dy = y - cy;
  const d = Math.hypot(dx, dy);
  if (d < RADIUS) {
    const a = (1 - d / RADIUS) * STRENGTH;
    const c = Math.cos(a), s = Math.sin(a);
    [dx, dy] = [dx*c - dy*s, dx*s + dy*c];
  }
  return get(Math.round(cx + dx), Math.round(cy + dy));
}`
  },
];
