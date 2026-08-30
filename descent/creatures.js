// Zone table. Colors are [top, bottom] of the water column gradient at the
// zone's *start* depth; the renderer interpolates between adjacent stops.
const ZONES = [
  { name: 'SUNLIT ZONE',   depth: 0,     top: [58, 154, 196], bottom: [13, 84, 122],  glow: '#dff6ff' },
  { name: 'TWILIGHT ZONE', depth: 200,   top: [21, 90, 138],  bottom: [7, 45, 78],    glow: '#9fe8ff' },
  { name: 'MIDNIGHT ZONE', depth: 1000,  top: [7, 40, 70],    bottom: [3, 18, 38],    glow: '#5ff2d6' },
  { name: 'ABYSSAL ZONE',  depth: 4000,  top: [3, 15, 30],    bottom: [1, 6, 15],     glow: '#8f9dff' },
  { name: 'HADAL ZONE',    depth: 6000,  top: [1, 6, 14],     bottom: [0, 2, 6],      glow: '#ffd7e8' },
  { name: 'THE RIFT',      depth: 10935, top: [2, 1, 8],      bottom: [0, 0, 0],      glow: '#ff9d6b' },
  { name: 'THE RIFT',      depth: 30000, top: [3, 0, 6],      bottom: [0, 0, 0],      glow: '#ff9d6b' },
];

// Silhouette archetypes. All face right; viewBox 0 0 100 60.
// Each is raw inner-SVG markup; fill/stroke are set by the renderer.
const ARCHETYPES = {
  fish: `<path d="M10 30 C 24 13, 58 11, 76 26 L91 15 L86 30 L91 45 L76 34 C 58 49, 24 47, 10 30 Z"/><circle class="eye" cx="24" cy="27" r="2.4"/>`,
  angler: `<path d="M12 34 C 20 15, 54 9, 76 25 L88 19 L83 33 L88 46 L74 40 C 54 53, 21 51, 12 34 Z"/><path class="line" d="M30 17 Q 20 2, 36 6" fill="none"/><circle class="lure" cx="37" cy="6" r="3.2"/><circle class="eye" cx="27" cy="27" r="2.6"/>`,
  jelly: `<path d="M22 28 Q 50 -4, 78 28 Q 64 36, 50 34 Q 36 36, 22 28 Z"/><path class="line" d="M34 32 q -5 12, 1 24 M46 34 q -1 13, -4 24 M56 34 q 2 13, 5 24 M66 32 q 6 11, 2 22" fill="none"/>`,
  squid: `<path d="M10 30 C 22 18, 42 15, 56 23 L58 37 C 44 45, 22 42, 10 30 Z M14 30 L2 18 L10 30 L2 42 Z"/><path class="line" d="M57 27 q 18 -4, 34 2 M58 31 q 20 1, 36 8 M57 34 q 16 8, 28 16" fill="none"/><circle class="eye" cx="46" cy="28" r="3"/>`,
  eel: `<path d="M6 32 Q 16 18, 28 28 T 52 28 T 76 28 Q 86 32, 94 26 L92 34 Q 84 40, 74 35 T 50 35 T 26 35 Q 14 42, 6 32 Z"/><circle class="eye" cx="86" cy="29" r="1.8"/>`,
  whale: `<path d="M6 33 C 16 15, 50 10, 72 21 C 79 24, 83 27, 87 25 L96 17 L92 31 L96 44 L85 36 C 68 48, 26 50, 6 33 Z"/><circle class="eye" cx="20" cy="27" r="2"/>`,
  shark: `<path d="M8 32 C 24 21, 46 18, 68 26 L82 15 L80 29 L94 33 L79 38 C 58 46, 26 44, 8 32 Z M38 21 L47 7 L54 21 Z"/><circle class="eye" cx="20" cy="29" r="2"/>`,
  octopus: `<path d="M34 24 Q 50 4, 66 24 Q 70 32, 63 36 L 37 36 Q 30 32, 34 24 Z"/><path class="line" d="M38 36 q -6 10, -12 14 M46 37 q -2 12, -8 18 M54 37 q 2 12, 8 18 M62 36 q 6 10, 12 14" fill="none"/><circle class="eye" cx="43" cy="26" r="2.4"/><circle class="eye" cx="57" cy="26" r="2.4"/>`,
  crab: `<path d="M30 26 Q 50 14, 70 26 Q 76 34, 68 40 L 32 40 Q 24 34, 30 26 Z"/><path class="line" d="M32 38 l -10 8 M40 40 l -6 10 M60 40 l 6 10 M68 38 l 10 8 M34 26 q -8 -8, -2 -14 M66 26 q 8 -8, 2 -14" fill="none"/>`,
  blob: `<path d="M26 32 Q 28 16, 46 16 Q 66 12, 72 26 Q 80 34, 68 42 Q 52 50, 38 44 Q 22 42, 26 32 Z"/>`,
  turtle: `<path d="M26 30 Q 40 14, 62 20 Q 76 24, 74 32 Q 70 42, 50 42 Q 32 42, 26 30 Z M70 26 Q 82 20, 88 26 Q 82 32, 72 32 Z M34 40 l -8 8 M56 42 l -4 9 M32 22 l -8 -6"/>`,
  sunfish: `<path d="M30 30 Q 38 12, 58 14 L 66 2 L 70 18 Q 78 24, 76 32 Q 78 40, 70 44 L 66 58 L 58 46 Q 38 48, 30 30 Z"/><circle class="eye" cx="42" cy="26" r="2.6"/>`,
  weird: `<path d="M20 30 Q 30 12, 50 18 Q 62 8, 70 20 Q 84 22, 78 34 Q 84 46, 66 44 Q 54 54, 44 44 Q 24 48, 20 30 Z"/><circle class="eye" cx="38" cy="28" r="2.2"/><circle class="eye" cx="56" cy="30" r="2.2"/><circle class="eye" cx="66" cy="26" r="1.6"/>`,
};

// value: research per photograph (pre-multipliers).
// depth: first-sighting depth in metres; discovered when the sub passes it.
const CREATURES = [
  // ── sunlit ──
  { id: 'moonjelly',  name: 'Moon Jellyfish',    depth: 15,   value: 8,     shape: 'jelly',   size: 46,
    lore: 'No brain, no heart, no bones. It has been drifting like this for five hundred million years.' },
  { id: 'turtle',     name: 'Green Sea Turtle',  depth: 45,   value: 12,    shape: 'turtle',  size: 60,
    lore: 'She is older than you. She will outlive your submersible.' },
  { id: 'mackerel',   name: 'Mackerel Shoal',    depth: 80,   value: 10,    shape: 'fish',    size: 34,
    lore: 'A thousand bodies turning as one. Nobody is in charge.' },
  { id: 'dolphin',    name: 'Bottlenose Dolphin', depth: 130, value: 16,    shape: 'whale',   size: 62,
    lore: 'It circles the hull twice, decides you are boring, and leaves.' },
  { id: 'sunfish',    name: 'Ocean Sunfish',     depth: 185,  value: 20,    shape: 'sunfish', size: 64,
    lore: 'Two and a half tonnes of placid indifference, sunning itself sideways.' },
  // ── twilight ──
  { id: 'lanternfish', name: 'Lanternfish',      depth: 300,  value: 32,    shape: 'fish',    size: 30,
    lore: 'The most abundant vertebrate on Earth. Almost no one has seen one alive.' },
  { id: 'combjelly',  name: 'Comb Jelly',        depth: 390,  value: 38,    shape: 'jelly',   size: 40,
    lore: 'Rows of cilia scatter your floodlights into slow rainbows. It predates almost everything.' },
  { id: 'hatchetfish', name: 'Hatchetfish',      depth: 470,  value: 44,    shape: 'fish',    size: 32,
    lore: 'A mirror with a face. Its belly glows to erase its own shadow.' },
  { id: 'barreleye',  name: 'Barreleye',         depth: 600,  value: 55,    shape: 'blob',    size: 40,
    lore: 'Its skull is a glass cockpit. The eyes inside point upward, watching for silhouettes.' },
  { id: 'vampsquid',  name: 'Vampire Squid',     depth: 750,  value: 68,    shape: 'squid',   size: 48,
    lore: 'Vampyroteuthis infernalis — the vampire squid from hell. It eats snow and means no harm.' },
  { id: 'humboldt',   name: 'Humboldt Squid',    depth: 860,  value: 76,    shape: 'squid',   size: 58,
    lore: 'It flashes red and white at you. Researchers who were grabbed describe the colour changing as it decided.' },
  { id: 'spermwhale', name: 'Sperm Whale',       depth: 950,  value: 90,    shape: 'whale',   size: 92,
    lore: 'Hunting a kilometre down on one held breath, as its family has for thirty million years. The clicks pass through your hull.' },
  // ── midnight ──
  { id: 'angler',     name: 'Anglerfish',        depth: 1200, value: 140,   shape: 'angler',  size: 46,
    lore: 'The lure is a colony of glowing bacteria. The fish grew a lamp and the lamp grew a tenant.' },
  { id: 'viperfish',  name: 'Viperfish',         depth: 1500, value: 180,   shape: 'eel',     size: 44,
    lore: 'Teeth too long for its own mouth. It swims with its jaws ajar, like scissors.' },
  { id: 'gulper',     name: 'Gulper Eel',        depth: 1800, value: 220,   shape: 'eel',     size: 56,
    lore: 'Mostly hinge. It can swallow prey larger than itself, and often regrets it.' },
  { id: 'dumbo',      name: 'Dumbo Octopus',     depth: 2100, value: 260,   shape: 'octopus', size: 44,
    lore: 'It flaps a pair of ear-like fins and hovers, politely, at the edge of the light.' },
  { id: 'giantsquid', name: 'Giant Squid',       depth: 2400, value: 340,   shape: 'squid',   size: 88,
    lore: 'Architeuthis. For four centuries, only bodies. No one filmed a living one until 2012.' },
  { id: 'bloodybelly', name: 'Bloodybelly Comb Jelly', depth: 2700, value: 380, shape: 'jelly', size: 38,
    lore: 'Blood-red, because down here red is the same as invisible. Its stomach glows; the red hides its meals.' },
  { id: 'sixgill',    name: 'Sixgill Shark',     depth: 3000, value: 430,   shape: 'shark',   size: 80,
    lore: 'A design older than trees. It has six gills because it never saw a reason to change.' },
  { id: 'zombieworm', name: 'Zombie Worms',      depth: 3500, value: 480,   shape: 'crab',    size: 30,
    lore: 'A whale fell here once. These are what remain of the ones who came to the funeral.' },
  // ── abyssal ──
  { id: 'tripod',     name: 'Tripod Fish',       depth: 4200, value: 700,   shape: 'fish',    size: 40,
    lore: 'It stands on three stilts of fin, face into the current, and waits. It may wait for weeks.' },
  { id: 'seapig',     name: 'Sea Pig',           depth: 4500, value: 800,   shape: 'blob',    size: 36,
    lore: 'A translucent pink sea cucumber on legs, herding across the mud with hundreds of its kind.' },
  { id: 'grenadier',  name: 'Grenadier',         depth: 4800, value: 900,   shape: 'fish',    size: 50,
    lore: 'The rattail. Wherever something dies in the abyss, these arrive within the hour. Every time.' },
  { id: 'bigfin',     name: 'Bigfin Squid',      depth: 5100, value: 1100,  shape: 'squid',   size: 74,
    lore: 'Elbowed arms trailing filaments eight metres long. Fewer than thirty sightings exist. This is one now.' },
  { id: 'cusk',       name: 'Faceless Cusk',     depth: 5400, value: 1250,  shape: 'eel',     size: 42,
    lore: 'Rediscovered in 2017, a century after the first. Its face is not missing. It is just not where faces go.' },
  { id: 'isopod',     name: 'Giant Isopod',      depth: 5700, value: 1400,  shape: 'crab',    size: 44,
    lore: 'A woodlouse the size of a cat. It can go five years between meals and looks it.' },
  // ── hadal ──
  { id: 'snailfish',  name: 'Hadal Snailfish',   depth: 7000, value: 2600,  shape: 'blob',    size: 40,
    lore: 'Soft, pink, almost gelatine. The deepest-living fish are not monsters. They are delicate.' },
  { id: 'amphipod',   name: 'Giant Amphipod',    depth: 7800, value: 3200,  shape: 'crab',    size: 38,
    lore: 'A shrimp-like scavenger, pale as paper, grown huge on patience and the dead.' },
  { id: 'abyssobrotula', name: 'Abyssobrotula',  depth: 8370, value: 4000,  shape: 'eel',     size: 40,
    lore: 'The deepest fish ever recovered — 8,370 metres. You are now looking at one at home.' },
  { id: 'xenophyophore', name: 'Xenophyophore',  depth: 9500, value: 5200,  shape: 'weird',   size: 36,
    lore: 'A single cell the size of your fist, building itself a castle of sediment. One cell.' },
  { id: 'mariana',    name: 'Ethereal Snailfish', depth: 10200, value: 6500, shape: 'blob',   size: 42,
    lore: 'It swims like a slow handkerchief. Eight tonnes of pressure per square inch, and it is fine.' },
  { id: 'floor',      name: 'Challenger Deep',   depth: 10935, value: 8000, shape: 'weird',   size: 50,
    lore: 'The floor of the world. More people have stood on the Moon. The sonar says the sediment is... hollow.' },
  // ── the rift ──
  { id: 'lattice',    name: 'Pale Lattice',      depth: 12000, value: 16000, shape: 'weird',  size: 56,
    lore: 'It is not on any chart. It repeats like coral, like circuitry. It was not grown. It was written.' },
  { id: 'chorus',     name: 'The Chorus',        depth: 15000, value: 30000, shape: 'jelly',  size: 60,
    lore: 'The hydrophone picks up something like singing. The waveform is perfectly, impossibly regular.' },
  { id: 'eyes',       name: 'Eyes',              depth: 20000, value: 60000, shape: 'weird',  size: 64,
    lore: 'The floodlights find retinas. Only retinas. Spaced like streetlights, all the way down.' },
  { id: 'itself',     name: 'The Descent Itself', depth: 30000, value: 120000, shape: 'weird', size: 72,
    lore: 'The instruments disagree about everything except one reading: you are still descending. Surface. Tell someone.' },
];

// Wrecks rest at (roughly) historical depths. Detected when the sub passes
// them; salvaged later for research; each grants a permanent relic effect.
const WRECKS = [
  { id: 'ghostnet',    name: 'Ghost Net',        depth: 120,   cost: 60,      eff: { photo: 0.10 },  effText: 'photographs +10%',
    lore: 'Ghost gear — a drift net that has kept fishing, crewless, for years. You cut it loose.' },
  { id: 'cable',       name: 'Telegraph Cable',  depth: 480,   cost: 350,     eff: { ping: 0.15 },   effText: 'sonar pings +15%',
    lore: 'Once, every word between two continents hummed through this. Now it rests, furred with anemones.' },
  { id: 'bathysphere', name: 'The Bathysphere',  depth: 923,   cost: 1200,    eff: { grantBonus: 1 }, effText: '+1 grant every resurface',
    lore: 'A steel ball on a cable. In 1934 two men folded themselves inside and became the first to see the midnight water. It is smaller than you imagined.' },
  { id: 'submarine',   name: 'Lost Submarine',   depth: 1600,  cost: 4000,    eff: { research: 0.10 }, effText: 'all research +10%',
    lore: 'Fifty years listed as overdue. The hatches are shut. You log the position and leave the rest be.' },
  { id: 'titanic',     name: 'RMS Titanic',      depth: 3803,  cost: 18000,   eff: { research: 0.15 }, effText: 'all research +15%',
    lore: 'You knew it was here. Everyone knows. It is still a shock — the bow upright, patient, growing rust like coral.' },
  { id: 'flightrec',   name: 'Flight Recorder',  depth: 3980,  cost: 30000,   eff: { drone: 0.25 },  effText: 'drones 25% faster',
    lore: 'Two years of searching for a box the size of a loaf of bread. The ocean returns nothing until it is asked properly.' },
  { id: 'johnston',    name: 'USS Johnston',     depth: 6456,  cost: 120000,  eff: { descent: 0.15 }, effText: 'descent +15%',
    lore: 'She charged a fleet so others could run, and sank still firing. The deepest warship ever surveyed, guns trained on nothing.' },
  { id: 'trieste',     name: 'Trieste Ballast',  depth: 10890, cost: 600000,  eff: { ping: 0.25, research: 0.05 }, effText: 'pings +25% · research +5%',
    lore: 'Nine tonnes of iron shot, released in 1960 so two men could float home from the deepest place on Earth. A bronze snowdrift, undisturbed.' },
  { id: 'door',        name: 'The Door',         depth: 14000, cost: 3500000, eff: { research: 0.20 }, effText: 'all research +20%',
    lore: 'It is a door. It is ajar. Nothing about the sediment suggests a building was ever attached.' },
];

// Expedition records: one-time milestones with permanent effects.
// cond(S, helpers) — helpers.zoneDone(i) = all species of zone i logged.
const MILESTONES = [
  { id: 'm200',    name: 'Pressure Test',        desc: 'reach 200 m',              eff: { research: 0.05 }, effText: 'research +5%',   cond: S => S.allMax >= 200 },
  { id: 'm1k',     name: 'Kilometre Club',       desc: 'reach 1 000 m',            eff: { descent: 0.05 },  effText: 'descent +5%',    cond: S => S.allMax >= 1000 },
  { id: 'm4k',     name: 'Onto the Plain',       desc: 'reach 4 000 m',            eff: { research: 0.10 }, effText: 'research +10%',  cond: S => S.allMax >= 4000 },
  { id: 'm6k',     name: 'Hadal',                desc: 'reach 6 000 m',            eff: { descent: 0.10 },  effText: 'descent +10%',   cond: S => S.allMax >= 6000 },
  { id: 'mfloor',  name: 'Floor of the World',   desc: 'reach 10 935 m',           eff: { research: 0.15 }, effText: 'research +15%',  cond: S => S.allMax >= 10935 },
  { id: 'shutter1', name: 'Shutterbug',          desc: '25 photographs',           eff: { photo: 0.10 },    effText: 'photographs +10%', cond: S => S.photos >= 25 },
  { id: 'shutter2', name: 'Field Guide',         desc: '200 photographs',          eff: { photo: 0.15 },    effText: 'photographs +15%', cond: S => S.photos >= 200 },
  { id: 'pings',   name: 'Active Sonar',         desc: '100 sonar pings',          eff: { ping: 0.25 },     effText: 'pings +25%',     cond: S => S.pings >= 100 },
  { id: 'ears',    name: 'Good Ear',             desc: 'investigate 10 contacts',  eff: { contact: 0.15 },  effText: 'contacts 15% sooner', cond: S => S.contactsDone >= 10 },
  { id: 'home',    name: 'Homecoming',           desc: 'resurface once',           eff: { research: 0.10 }, effText: 'research +10%',  cond: S => S.resurfaces >= 1 },
  { id: 'salvor',  name: 'Salvor',               desc: 'salvage a wreck',          eff: { ping: 0.10 },     effText: 'pings +10%',     cond: S => (S.relics || []).length >= 1 },
  { id: 'set0',    name: 'Sunlit Survey',        desc: 'log every sunlit species',   eff: { photo: 0.10 },    effText: 'photographs +10%', cond: (S, h) => h.zoneDone(0) },
  { id: 'set1',    name: 'Twilight Survey',      desc: 'log every twilight species', eff: { research: 0.10 }, effText: 'research +10%',  cond: (S, h) => h.zoneDone(1) },
  { id: 'set2',    name: 'Midnight Survey',      desc: 'log every midnight species', eff: { descent: 0.10 },  effText: 'descent +10%',   cond: (S, h) => h.zoneDone(2) },
  { id: 'set3',    name: 'Abyssal Survey',       desc: 'log every abyssal species',  eff: { research: 0.15 }, effText: 'research +15%',  cond: (S, h) => h.zoneDone(3) },
  { id: 'set4',    name: 'Hadal Survey',         desc: 'log every hadal species',    eff: { photo: 0.20 },    effText: 'photographs +20%', cond: (S, h) => h.zoneDone(4) },
  { id: 'set5',    name: 'Rift Survey',          desc: 'log everything below the floor', eff: { research: 0.25 }, effText: 'research +25%', cond: (S, h) => h.zoneDone(5) },
];

// Dry dock: permanent purchases, paid in expedition grants (✦).
const DOCK = [
  { key: 'funding',  name: 'Grant Funding',      max: Infinity, cost: n => n + 1,                 desc: '+10% all research per level' },
  { key: 'pilots',   name: 'Veteran Pilots',     max: Infinity, cost: n => n + 1,                 desc: '+8% descent speed per level' },
  { key: 'biologist', name: 'Staff Biologist',   max: Infinity, cost: n => n + 1,                 desc: '+15% photograph value per level' },
  { key: 'sensors',  name: 'Rare-Earth Sensors', max: 5,        cost: n => 3 * Math.pow(2, n),    desc: '+2% rare sighting chance per level' },
  { key: 'keel',     name: 'Reinforced Keel',    max: 5,        cost: n => [4, 10, 24, 60, 150][n], desc: 'begin every expedition with the next hull tier already fitted' },
  { key: 'refit',    name: 'Standing Refit',     max: 4,        cost: n => [5, 12, 30, 75][n],    desc: 'begin with floodlights & sonar at this level' },
];
