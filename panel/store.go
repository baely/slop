package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Screen is one canvas: its dimensions, its device key, and its blocks.
//
// W and H are the COMPOSITION size — the canvas the blocks are positioned on
// and the size the editor shows. Rotation is applied afterwards, so a 90 or
// 270 degree screen is delivered H x W. That is the intuitive reading for a
// layout editor, and with the default rotation of 0 it is identical to what
// the old single-layout version served.
type Screen struct {
	Name      string    `json:"name"`
	W         int       `json:"w"`
	H         int       `json:"h"`
	Rotate    int       `json:"rotate,omitempty"`
	Invert    bool      `json:"invert,omitempty"`
	DeviceKey string    `json:"device_key"`
	Blocks    []Block   `json:"blocks"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Version identifies this exact screen content for cache keys and ETags.
func (s Screen) Version() string { return fmt.Sprintf("%d", s.UpdatedAt.UnixNano()) }

func (s Screen) clone() Screen {
	out := s
	out.Blocks = make([]Block, len(s.Blocks))
	for i, b := range s.Blocks {
		out.Blocks[i] = b
		out.Blocks[i].Rows = append([]Row(nil), b.Rows...)
		out.Blocks[i].Items = append([]string(nil), b.Items...)
	}
	return out
}

// Granularity is the clock resolution this screen renders at. A screen with a
// seconds clock has to re-render every second; every other screen renders once
// a minute, which is what makes the ETag worth anything to a battery panel.
func (s Screen) Granularity() time.Duration {
	for _, b := range s.Blocks {
		if b.NeedsSeconds() {
			return time.Second
		}
	}
	return time.Minute
}

func (s Screen) DataBlocks() []Block {
	var out []Block
	for _, b := range s.Blocks {
		if b.Type == BlockData {
			out = append(out, b)
		}
	}
	return out
}

// ImageMeta is one uploaded picture.
type ImageMeta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	W         int       `json:"w"`
	H         int       `json:"h"`
	Bytes     int       `json:"bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// State is the whole persisted document.
type State struct {
	Version   int         `json:"version"`
	Screens   []Screen    `json:"screens"`
	Images    []ImageMeta `json:"images,omitempty"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// legacyConfig is the shape the previous single-layout version wrote. It is
// read once and migrated; a deployed panel keeps its device key, so a device
// already flashed with a URL keeps working across the upgrade.
type legacyConfig struct {
	Title     string `json:"title"`
	Rows      []Row  `json:"rows"`
	Footer    string `json:"footer"`
	DeviceKey string `json:"device_key"`
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Store holds the state in memory behind an RWMutex and persists it with an
// atomic write. Tiny data; a database would be a bigger liability than the file.
type Store struct {
	mu    sync.RWMutex
	state State
	path  string
	dir   string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "panel.json"), dir: dir}
	dirty := false
	b, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		var st State
		if err := json.Unmarshal(b, &st); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.path, err)
		}
		if len(st.Screens) == 0 {
			var legacy legacyConfig
			if err := json.Unmarshal(b, &legacy); err != nil {
				return nil, fmt.Errorf("parse %s: %w", s.path, err)
			}
			st = migrateLegacy(legacy)
			dirty = true
		}
		s.state = st
	case errors.Is(err, fs.ErrNotExist):
		s.state = State{}
	default:
		return nil, err
	}

	if len(s.state.Screens) == 0 {
		sc, err := newScreen("default", 800, 480)
		if err != nil {
			return nil, err
		}
		sc.Blocks = starterBlocks("clock-rows", sc)
		sc.UpdatedAt = time.Now().UTC()
		s.state.Screens = []Screen{sc}
		dirty = true
	}
	for i := range s.state.Screens {
		if s.state.Screens[i].DeviceKey == "" {
			key, err := newDeviceKey()
			if err != nil {
				return nil, err
			}
			s.state.Screens[i].DeviceKey = key
			dirty = true
		}
		if s.state.Screens[i].Blocks == nil {
			s.state.Screens[i].Blocks = []Block{}
		}
	}
	if s.state.Version != 2 {
		s.state.Version = 2
		dirty = true
	}
	if dirty {
		s.state.UpdatedAt = time.Now().UTC()
		if err := s.persist(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// migrateLegacy turns the old title/rows/footer document into a screen whose
// blocks reproduce the old fixed arrangement, so an existing panel looks the
// same the moment it restarts and can then be pulled apart.
func migrateLegacy(l legacyConfig) State {
	sc := Screen{Name: "default", W: 800, H: 480, DeviceKey: l.DeviceKey, UpdatedAt: time.Now().UTC()}
	title := l.Title
	if title == "" {
		title = "panel"
	}
	blocks := []Block{
		{Type: BlockText, X: 0, Y: 0, W: 800, H: 52, Anchor: "w", Align: "left",
			Text: title, Font: "go-bold", Size: 29, Invert: true},
		{Type: BlockClock, X: 20, Y: 52, W: 400, H: 132, Anchor: "w", Align: "left",
			Format: "24h", Font: "go-bold", Size: 96},
		{Type: BlockDate, X: 420, Y: 52, W: 360, H: 132, Anchor: "e", Align: "right",
			Format: "long", Font: "go-bold", Size: 26},
		{Type: BlockDivider, X: 0, Y: 184, W: 800, H: 3, Orient: "horizontal", Thickness: 3, Anchor: "nw", Align: "left"},
	}
	rows := l.Rows
	if len(rows) > 0 {
		if len(rows) > maxRows {
			rows = rows[:maxRows]
		}
		blocks = append(blocks, Block{
			Type: BlockRows, X: 20, Y: 196, W: 760, H: 232, Anchor: "nw", Align: "left",
			Rows: rows, Rules: true, Font: "pixel8x16", Size: 16,
		})
	}
	if l.Footer != "" {
		blocks = append(blocks, Block{
			Type: BlockText, X: 20, Y: 440, W: 500, H: 24, Anchor: "w", Align: "left",
			Text: l.Footer, Font: "pixel8x16", Size: 16,
		})
	}
	sc.Blocks = blocks
	return State{Version: 2, Screens: []Screen{sc}, UpdatedAt: time.Now().UTC()}
}

func newScreen(name string, w, h int) (Screen, error) {
	key, err := newDeviceKey()
	if err != nil {
		return Screen{}, err
	}
	return Screen{Name: name, W: w, H: h, DeviceKey: key, Blocks: []Block{}}, nil
}

// ---------------------------------------------------------------- accessors

func (s *Store) Screens() []Screen {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Screen, len(s.state.Screens))
	for i, sc := range s.state.Screens {
		out[i] = sc.clone()
	}
	return out
}

func (s *Store) Screen(name string) (Screen, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.state.Screens {
		if sc.Name == name {
			return sc.clone(), true
		}
	}
	return Screen{}, false
}

// DefaultScreen is the first screen: the one /screen.png serves.
func (s *Store) DefaultScreen() Screen {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Screens[0].clone()
}

func (s *Store) Images() []ImageMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ImageMeta(nil), s.state.Images...)
}

func (s *Store) ImageIDs() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := make(map[string]bool, len(s.state.Images))
	for _, im := range s.state.Images {
		m[im.ID] = true
	}
	return m
}

// StateJSON is the whole document, pretty-printed, for the escape hatch.
func (s *Store) StateJSON() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b) + "\n"
}

// ScreenJSON is one screen, pretty-printed.
func (s *Store) ScreenJSON(name string) (string, bool) {
	sc, ok := s.Screen(name)
	if !ok {
		return "", false
	}
	b, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return "", false
	}
	return string(b) + "\n", true
}

// ------------------------------------------------------------------- writes

// SaveScreen validates and replaces one screen by name. The device key and
// creation identity are preserved: they are not the user's to edit here.
func (s *Store) SaveScreen(name string, next Screen) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, sc := range s.state.Screens {
		if sc.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no screen named %q", name)
	}
	next.DeviceKey = s.state.Screens[idx].DeviceKey
	imageIDs := make(map[string]bool, len(s.state.Images))
	for _, im := range s.state.Images {
		imageIDs[im.ID] = true
	}
	if err := validateScreen(next, imageIDs); err != nil {
		return err
	}
	if next.Name != name {
		for i, sc := range s.state.Screens {
			if i != idx && sc.Name == next.Name {
				return fmt.Errorf("a screen named %q already exists", next.Name)
			}
		}
	}
	next.UpdatedAt = time.Now().UTC()
	s.state.Screens[idx] = next.clone()
	s.state.UpdatedAt = next.UpdatedAt
	return s.persist()
}

// ReplaceState swaps the entire document — the JSON escape hatch. Device keys
// are matched by screen name so pasting a layout does not silently kill a
// flashed device URL.
func (s *Store) ReplaceState(next State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(next.Screens) == 0 {
		return errors.New("at least one screen is required")
	}
	if len(next.Screens) > maxScreens {
		return fmt.Errorf("%d screens, the cap is %d", len(next.Screens), maxScreens)
	}
	old := map[string]string{}
	for _, sc := range s.state.Screens {
		old[sc.Name] = sc.DeviceKey
	}
	imageIDs := make(map[string]bool, len(s.state.Images))
	for _, im := range s.state.Images {
		imageIDs[im.ID] = true
	}
	seen := map[string]bool{}
	for i := range next.Screens {
		sc := next.Screens[i]
		if seen[sc.Name] {
			return fmt.Errorf("two screens are both named %q", sc.Name)
		}
		seen[sc.Name] = true
		if sc.DeviceKey == "" {
			if k, ok := old[sc.Name]; ok {
				sc.DeviceKey = k
			} else {
				k, err := newDeviceKey()
				if err != nil {
					return err
				}
				sc.DeviceKey = k
			}
		}
		if err := validateScreen(sc, imageIDs); err != nil {
			return err
		}
		sc.UpdatedAt = time.Now().UTC()
		next.Screens[i] = sc.clone()
	}
	next.Version = 2
	next.Images = append([]ImageMeta(nil), s.state.Images...)
	next.UpdatedAt = time.Now().UTC()
	s.state = next
	return s.persist()
}

func (s *Store) AddScreen(name string, w, h int) (Screen, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Screens) >= maxScreens {
		return Screen{}, fmt.Errorf("%d screens is the cap", maxScreens)
	}
	name = slug(name)
	if !nameRE.MatchString(name) {
		return Screen{}, fmt.Errorf("screen names are lowercase letters, digits and dashes, 1 to %d characters", maxNameLen)
	}
	for _, sc := range s.state.Screens {
		if sc.Name == name {
			return Screen{}, fmt.Errorf("a screen named %q already exists", name)
		}
	}
	sc, err := newScreen(name, w, h)
	if err != nil {
		return Screen{}, err
	}
	if err := validateScreen(sc, nil); err != nil {
		return Screen{}, err
	}
	sc.UpdatedAt = time.Now().UTC()
	s.state.Screens = append(s.state.Screens, sc)
	s.state.UpdatedAt = sc.UpdatedAt
	if err := s.persist(); err != nil {
		return Screen{}, err
	}
	return sc.clone(), nil
}

func (s *Store) DuplicateScreen(name string) (Screen, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Screens) >= maxScreens {
		return Screen{}, fmt.Errorf("%d screens is the cap", maxScreens)
	}
	var src *Screen
	for i := range s.state.Screens {
		if s.state.Screens[i].Name == name {
			src = &s.state.Screens[i]
			break
		}
	}
	if src == nil {
		return Screen{}, fmt.Errorf("no screen named %q", name)
	}
	dup := src.clone()
	dup.Name = s.uniqueName(name + "-copy")
	key, err := newDeviceKey()
	if err != nil {
		return Screen{}, err
	}
	dup.DeviceKey = key
	dup.UpdatedAt = time.Now().UTC()
	s.state.Screens = append(s.state.Screens, dup)
	s.state.UpdatedAt = dup.UpdatedAt
	if err := s.persist(); err != nil {
		return Screen{}, err
	}
	return dup.clone(), nil
}

// uniqueName assumes the write lock is held.
func (s *Store) uniqueName(base string) string {
	base = slug(base)
	if len(base) > maxNameLen {
		base = base[:maxNameLen]
	}
	taken := map[string]bool{}
	for _, sc := range s.state.Screens {
		taken[sc.Name] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; i < 100; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if len(cand) > maxNameLen {
			cand = cand[len(cand)-maxNameLen:]
		}
		if !taken[cand] {
			return cand
		}
	}
	return base + "-x"
}

func (s *Store) DeleteScreen(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Screens) == 1 {
		return errors.New("the last screen cannot be deleted")
	}
	for i, sc := range s.state.Screens {
		if sc.Name == name {
			s.state.Screens = append(s.state.Screens[:i], s.state.Screens[i+1:]...)
			s.state.UpdatedAt = time.Now().UTC()
			return s.persist()
		}
	}
	return fmt.Errorf("no screen named %q", name)
}

func (s *Store) RotateKey(name string) (string, error) {
	key, err := newDeviceKey()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Screens {
		if s.state.Screens[i].Name == name {
			s.state.Screens[i].DeviceKey = key
			s.state.Screens[i].UpdatedAt = time.Now().UTC()
			s.state.UpdatedAt = s.state.Screens[i].UpdatedAt
			if err := s.persist(); err != nil {
				return "", err
			}
			return key, nil
		}
	}
	return "", fmt.Errorf("no screen named %q", name)
}

func (s *Store) AddImage(meta ImageMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Images) >= maxImages {
		return fmt.Errorf("%d images is the cap; delete one first", maxImages)
	}
	s.state.Images = append(s.state.Images, meta)
	s.state.UpdatedAt = time.Now().UTC()
	return s.persist()
}

// DeleteImage removes the record and reports whether any block still uses it.
func (s *Store) DeleteImage(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.state.Screens {
		for _, b := range sc.Blocks {
			if b.Type == BlockImage && b.Image == id {
				return fmt.Errorf("screen %q still uses that image; remove the block first", sc.Name)
			}
		}
	}
	for i, im := range s.state.Images {
		if im.ID == id {
			s.state.Images = append(s.state.Images[:i], s.state.Images[i+1:]...)
			s.state.UpdatedAt = time.Now().UTC()
			return s.persist()
		}
	}
	return fmt.Errorf("no image %q", id)
}

// ---------------------------------------------------------------- validation

func validateScreen(sc Screen, imageIDs map[string]bool) error {
	if !nameRE.MatchString(sc.Name) {
		return fmt.Errorf("screen name %q is not valid: lowercase letters, digits and dashes, 1 to %d characters", sc.Name, maxNameLen)
	}
	if sc.W < minDim || sc.W > maxDim {
		return fmt.Errorf("screen %q: width must be between %d and %d pixels, got %d", sc.Name, minDim, maxDim, sc.W)
	}
	if sc.H < minDim || sc.H > maxDim {
		return fmt.Errorf("screen %q: height must be between %d and %d pixels, got %d", sc.Name, minDim, maxDim, sc.H)
	}
	if sc.W*sc.H > maxArea {
		return fmt.Errorf("screen %q: %dx%d is %d pixels, the cap is %d", sc.Name, sc.W, sc.H, sc.W*sc.H, maxArea)
	}
	switch sc.Rotate {
	case 0, 90, 180, 270:
	default:
		return fmt.Errorf("screen %q: rotate must be 0, 90, 180 or 270, got %d", sc.Name, sc.Rotate)
	}
	if len(sc.Blocks) > maxBlocks {
		return fmt.Errorf("screen %q has %d blocks, the cap is %d", sc.Name, len(sc.Blocks), maxBlocks)
	}
	data := 0
	for i, b := range sc.Blocks {
		if err := b.Normalize().Validate(i, sc, imageIDs); err != nil {
			return err
		}
		if b.Type == BlockData {
			data++
		}
	}
	if data > maxDataBlocks {
		return fmt.Errorf("screen %q has %d data blocks, the cap is %d", sc.Name, data, maxDataBlocks)
	}
	return nil
}

// ----------------------------------------------------------------- persistence

// persist writes to a temp file in the same directory, fsyncs it, renames it
// over the target, then fsyncs the directory. A crash leaves either the old
// file or the new one, never half of either. Caller holds the write lock.
func (s *Store) persist() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return atomicWrite(s.path, b, 0o600)
}

func atomicWrite(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".panel-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// ------------------------------------------------------------------- helpers

// deviceKeyAlphabet is Crockford-ish base32 without ambiguous characters, from
// the standard base32 alphabet with I, L, O and U removed by substitution.
var deviceKeyEnc = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// newDeviceKey returns a 128-bit capability id. It is not a password: it is an
// unguessable URL component so a battery panel can GET its image without
// carrying the app token.
func newDeviceKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(deviceKeyEnc.EncodeToString(b)), nil
}

// newID returns a short unguessable id for an uploaded image.
func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(deviceKeyEnc.EncodeToString(b)), nil
}

// clean strips control characters, collapses whitespace and enforces a rune cap.
func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return strings.TrimSpace(string(r))
}

// cleanMulti is clean but newlines survive: text blocks are allowed to wrap.
func cleanMulti(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	s = strings.Join(lines, "\n")
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return strings.Trim(string(r), " \n")
}

// slug lowercases and keeps only what a screen name may contain.
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == ' ' || r == '_':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > maxNameLen {
		out = strings.Trim(out[:maxNameLen], "-")
	}
	return out
}
