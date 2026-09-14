package asset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DiskCacheConfig configures the persistent thumbnail cache of the project
// plan, section 9, point 4.
//
// An empty Dir switches disk caching off entirely — "Disk-Caching ist
// abschaltbar" — and then nothing is read, written, scanned or created.
type DiskCacheConfig struct {
	// Dir is the directory the cache lives in. Empty switches the cache off.
	// The directory is created on demand.
	Dir string

	// Budget is the ceiling in bytes for the files in Dir. Zero selects
	// [DefaultDiskBudget]. When a write would exceed it, least recently used
	// entries are deleted until it fits.
	Budget int64

	// Now supplies the clock, for tests. Nil selects [time.Now].
	Now func() time.Time
}

// DefaultDiskBudget is the disk cache ceiling when none is configured: 256 MiB,
// which holds about a thousand 256 pixel thumbnails in the raw format the cache
// uses.
const DefaultDiskBudget int64 = 256 << 20

// diskCache is the persistent thumbnail store.
//
// # File format
//
// Thumbnails are stored as raw premultiplied RGBA behind a fixed header, not as
// PNG. The point of the cache is to be much cheaper than decoding the original,
// and re-encoding a thumbnail as PNG gives back a large part of what it saves:
// a 256 by 256 entry is 256 kB raw against roughly 60 kB compressed, and the
// budget is expressed in bytes anyway, so the cost of the choice is visible and
// configurable. A hit is a read and a header check.
//
// The header carries the full key. A file whose header does not match the key
// it was found under, or whose payload does not match its CRC, is treated as a
// miss and deleted: a truncated write from a power failure, a half written file
// from a crash and a deliberately corrupted one are the same case and must not
// reach the screen as garbage pixels.
type diskCache struct {
	dir    string
	budget int64
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]diskEntry // by file name
	total   int64
	scanned bool

	hits, misses, writes, evictions, corrupt, errs atomic.Uint64
	bytesRead, bytesWritten                        atomic.Uint64
	// sweptTemps counts the half written files a crashed process left
	// behind and this one deleted; see [diskCache.scan].
	sweptTemps atomic.Uint64
}

type diskEntry struct {
	size int64
	used time.Time
}

const (
	// tmpPrefix is the prefix of the temporary file a write goes through.
	// It is not a valid cache key — keys are hex — so a leftover can be
	// told apart from an entry by its name alone.
	tmpPrefix   = ".tmp-"
	diskMagic   = 0x47494654 // "GIFT"
	diskVersion = 1
	// diskHeaderFixed is magic, version, width, height, orientation, ladder
	// size, processing version, payload length and CRC.
	diskHeaderFixed = 4 + 2 + 2 + 4 + 4 + 4 + 4 + 4 + 4
)

func newDiskCache(cfg DiskCacheConfig) *diskCache {
	if cfg.Dir == "" {
		return nil
	}
	b := cfg.Budget
	if b <= 0 {
		b = DefaultDiskBudget
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &diskCache{
		dir:     filepath.Join(cfg.Dir, "v1"),
		budget:  b,
		now:     now,
		entries: make(map[string]diskEntry),
	}
}

// scan builds the occupancy index once, on first use.
//
// It walks the directory, which is O(entries) and happens outside the frame
// path. It is not done in the constructor so that a pipeline that never gets a
// cache miss never touches the file system.
func (d *diskCache) scan() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.scanned {
		return
	}
	d.scanned = true
	ents, err := os.ReadDir(d.dir)
	if err != nil {
		return // absent directory is an empty cache, not an error
	}
	for _, sub := range ents {
		if !sub.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(d.dir, sub.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if strings.HasPrefix(name, tmpPrefix) {
				// A half written entry from a process that died
				// between CreateTemp and Rename. It is not an entry:
				// its name is not a cache key, so [diskCache.path]
				// would resolve it to the wrong directory and
				// [diskCache.remove] would silently fail, leaving the
				// bytes on disk and counted in total for ever.
				// Collect it here, which is the only place that knows
				// where it really is.
				_ = os.Remove(filepath.Join(d.dir, sub.Name(), name))
				d.sweptTemps.Add(1)
				continue
			}
			fi, err := f.Info()
			if err != nil {
				continue
			}
			d.entries[name] = diskEntry{size: fi.Size(), used: fi.ModTime()}
			d.total += fi.Size()
		}
	}
}

func (d *diskCache) path(name string) string {
	return filepath.Join(d.dir, name[:2], name)
}

// get reads the entry for k, or reports a miss.
func (d *diskCache) get(k Key) (w, h int, pix []byte, ok bool) {
	if d == nil || k.Revision == "" {
		return 0, 0, nil, false
	}
	d.scan()
	name := k.fileName()
	raw, err := os.ReadFile(d.path(name))
	if err != nil {
		d.misses.Add(1)
		return 0, 0, nil, false
	}
	w, h, pix, err = decodeDiskEntry(raw, k)
	if err != nil {
		d.corrupt.Add(1)
		d.remove(name)
		return 0, 0, nil, false
	}
	d.hits.Add(1)
	d.bytesRead.Add(uint64(len(raw)))
	d.mu.Lock()
	if e, exists := d.entries[name]; exists {
		e.used = d.now()
		d.entries[name] = e
	}
	d.mu.Unlock()
	return w, h, pix, true
}

// put writes the entry for k and evicts down to the budget.
//
// A key without a revision is not written: see [ErrNoRevision].
func (d *diskCache) put(k Key, t *Thumbnail) error {
	if d == nil {
		return nil
	}
	if k.Revision == "" {
		return ErrNoRevision
	}
	d.scan()
	buf := encodeDiskEntry(k, t)
	name := k.fileName()
	dir := filepath.Join(d.dir, name[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		d.errs.Add(1)
		return err
	}
	// Write to a temporary file in the same directory and rename, so a
	// crash leaves either the old entry or the new one and never half of
	// either. A reader that still sees a torn file is caught by the CRC.
	tmp, err := os.CreateTemp(dir, tmpPrefix+"*")
	if err != nil {
		d.errs.Add(1)
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		d.errs.Add(1)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		d.errs.Add(1)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		os.Remove(tmpName)
		d.errs.Add(1)
		return err
	}
	d.writes.Add(1)
	d.bytesWritten.Add(uint64(len(buf)))

	d.mu.Lock()
	if old, ok := d.entries[name]; ok {
		d.total -= old.size
	}
	d.entries[name] = diskEntry{size: int64(len(buf)), used: d.now()}
	d.total += int64(len(buf))
	victims := d.pickVictims(name)
	d.mu.Unlock()

	for _, v := range victims {
		d.remove(v)
		d.evictions.Add(1)
	}
	return nil
}

// pickVictims returns the least recently used entries to delete so that the
// total fits the budget. It runs under the lock and does no I/O.
func (d *diskCache) pickVictims(keep string) []string {
	if d.total <= d.budget {
		return nil
	}
	type cand struct {
		name string
		e    diskEntry
	}
	all := make([]cand, 0, len(d.entries))
	for n, e := range d.entries {
		if n == keep {
			continue
		}
		all = append(all, cand{n, e})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].e.used.Before(all[j].e.used) })
	var out []string
	total := d.total
	for _, c := range all {
		if total <= d.budget {
			break
		}
		out = append(out, c.name)
		total -= c.e.size
	}
	return out
}

func (d *diskCache) remove(name string) {
	if err := os.Remove(d.path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		d.errs.Add(1)
	}
	d.mu.Lock()
	if e, ok := d.entries[name]; ok {
		d.total -= e.size
		delete(d.entries, name)
	}
	d.mu.Unlock()
}

func encodeDiskEntry(k Key, t *Thumbnail) []byte {
	pix := t.pix
	buf := make([]byte, diskHeaderFixed+len(pix))
	binary.LittleEndian.PutUint32(buf[0:], diskMagic)
	binary.LittleEndian.PutUint16(buf[4:], diskVersion)
	binary.LittleEndian.PutUint16(buf[6:], uint16(k.Orientation.Normalised()))
	binary.LittleEndian.PutUint32(buf[8:], uint32(t.w))
	binary.LittleEndian.PutUint32(buf[12:], uint32(t.h))
	binary.LittleEndian.PutUint32(buf[16:], uint32(k.Size))
	binary.LittleEndian.PutUint32(buf[20:], k.ProcessingVersion)
	binary.LittleEndian.PutUint32(buf[24:], uint32(len(pix)))
	copy(buf[diskHeaderFixed:], pix)
	binary.LittleEndian.PutUint32(buf[28:], crc32.Checksum(pix, crcTable))
	return buf
}

var crcTable = crc32.MakeTable(crc32.Castagnoli)

// errCorrupt is internal: every caller turns it into a miss.
var errCorrupt = errors.New("cache entry is corrupt")

func decodeDiskEntry(raw []byte, k Key) (w, h int, pix []byte, err error) {
	if len(raw) < diskHeaderFixed {
		return 0, 0, nil, errCorrupt
	}
	if binary.LittleEndian.Uint32(raw[0:]) != diskMagic ||
		binary.LittleEndian.Uint16(raw[4:]) != diskVersion {
		return 0, 0, nil, errCorrupt
	}
	if Orientation(binary.LittleEndian.Uint16(raw[6:])) != k.Orientation.Normalised() ||
		int(binary.LittleEndian.Uint32(raw[16:])) != k.Size ||
		binary.LittleEndian.Uint32(raw[20:]) != k.ProcessingVersion {
		// The file name is a hash of the whole key, so this can only be a
		// hash collision or a file put there by something else. Either
		// way it is not the entry that was asked for.
		return 0, 0, nil, errCorrupt
	}
	w = int(binary.LittleEndian.Uint32(raw[8:]))
	h = int(binary.LittleEndian.Uint32(raw[12:]))
	n := int(binary.LittleEndian.Uint32(raw[24:]))
	want := binary.LittleEndian.Uint32(raw[28:])
	if w <= 0 || h <= 0 || n != w*h*4 || diskHeaderFixed+n != len(raw) {
		return 0, 0, nil, errCorrupt
	}
	pix = raw[diskHeaderFixed:]
	if crc32.Checksum(pix, crcTable) != want {
		return 0, 0, nil, fmt.Errorf("%w: checksum mismatch", errCorrupt)
	}
	return w, h, pix, nil
}

// --- revision hints ----------------------------------------------------------

// hint is what the pipeline remembers about a source whose revision only a
// transfer reveals, so that a warm start does not have to ask the network.
type hint struct {
	Revision string
	Expires  time.Time
	W, H     int
	O        Orientation
	MIME     string
}

const hintMagic = 0x47494648 // "GIFH"

func (d *diskCache) getHint(ns string, id ID) (hint, bool) {
	if d == nil {
		return hint{}, false
	}
	raw, err := os.ReadFile(d.path(hintName(ns, id)))
	if err != nil || len(raw) < 4 || binary.LittleEndian.Uint32(raw) != hintMagic {
		return hint{}, false
	}
	var h hint
	p := raw[4:]
	rd := func() (string, bool) {
		if len(p) < 4 {
			return "", false
		}
		n := int(binary.LittleEndian.Uint32(p))
		p = p[4:]
		if n < 0 || n > len(p) {
			return "", false
		}
		s := string(p[:n])
		p = p[n:]
		return s, true
	}
	rev, ok1 := rd()
	mt, ok2 := rd()
	if !ok1 || !ok2 || len(p) < 8+4+4+2 {
		return hint{}, false
	}
	h.Revision, h.MIME = rev, mt
	h.Expires = time.Unix(0, int64(binary.LittleEndian.Uint64(p)))
	h.W = int(binary.LittleEndian.Uint32(p[8:]))
	h.H = int(binary.LittleEndian.Uint32(p[12:]))
	h.O = Orientation(binary.LittleEndian.Uint16(p[16:]))
	return h, true
}

func (d *diskCache) putHint(ns string, id ID, h hint) {
	if d == nil || h.Revision == "" {
		return
	}
	var buf []byte
	buf = binary.LittleEndian.AppendUint32(buf, hintMagic)
	for _, s := range []string{h.Revision, h.MIME} {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(s)))
		buf = append(buf, s...)
	}
	buf = binary.LittleEndian.AppendUint64(buf, uint64(h.Expires.UnixNano()))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(h.W))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(h.H))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(h.O))

	name := hintName(ns, id)
	dir := filepath.Join(d.dir, name[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		d.errs.Add(1)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf, 0o644); err != nil {
		d.errs.Add(1)
		return
	}
	d.mu.Lock()
	if old, ok := d.entries[name]; ok {
		d.total -= old.size
	}
	d.entries[name] = diskEntry{size: int64(len(buf)), used: d.now()}
	d.total += int64(len(buf))
	d.mu.Unlock()
}

// DiskCacheStats are the persistent cache counters.
type DiskCacheStats struct {
	// Enabled is false when no directory was configured.
	Enabled bool
	// Hits and Misses are lookup outcomes. Corrupt entries count as misses
	// as well as in Corrupt.
	Hits, Misses uint64
	// Writes is the number of entries stored and Evictions the number
	// deleted to stay inside the budget.
	Writes, Evictions uint64
	// Corrupt is the number of entries rejected by the header or CRC check.
	// Non zero after a clean run means something else is writing into the
	// cache directory.
	Corrupt uint64
	// Errors is the number of file system errors. They never fail a request;
	// a cache that cannot be written is a slow cache, not a broken gallery.
	Errors uint64
	// BytesRead and BytesWritten are the transfer volumes.
	BytesRead, BytesWritten uint64
	// SweptTemps is the number of half written files left by a process that
	// died mid write and deleted by the first scan of this one. A steady
	// trickle means something is killing the application during a cache
	// write.
	SweptTemps uint64
	// Bytes and Entries are the current occupancy, and Budget the ceiling.
	Bytes   int64
	Entries int
	Budget  int64
}

func (d *diskCache) snapshot() DiskCacheStats {
	if d == nil {
		return DiskCacheStats{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return DiskCacheStats{
		Enabled:      true,
		Hits:         d.hits.Load(),
		Misses:       d.misses.Load(),
		Writes:       d.writes.Load(),
		Evictions:    d.evictions.Load(),
		Corrupt:      d.corrupt.Load(),
		Errors:       d.errs.Load(),
		SweptTemps:   d.sweptTemps.Load(),
		BytesRead:    d.bytesRead.Load(),
		BytesWritten: d.bytesWritten.Load(),
		Bytes:        d.total,
		Entries:      len(d.entries),
		Budget:       d.budget,
	}
}
