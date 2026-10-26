// Package importer turns what a provider delivered into a library entry:
// unpack, keep the game files, optionally convert discs to CHD, place them in
// the platform folder without ever overwriting, and ask RomM to scan.
package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Importer struct {
	// StagingRoot is where the provider delivers (its staging storage).
	StagingRoot string
	// LibraryRoot holds one folder per platform (RomM's roms/<fs_slug>).
	LibraryRoot string
	// ScanHook is romm-scan-hook's URL; empty disables scanning.
	ScanHook string
	http     *http.Client
}

func New(staging, library, hook string) *Importer {
	return &Importer{StagingRoot: staging, LibraryRoot: library, ScanHook: hook,
		http: &http.Client{Timeout: 15 * time.Second}}
}

// ErrNoDelivery: the provider's job says done but its files are not in
// staging (deleted, or delivered somewhere else). The download must be redone.
var ErrNoDelivery = errors.New("delivery folder missing")

// Clean empties a request's delivery folder, so a new attempt never mixes
// with what a failed one left (a half-written CHD).
func (im *Importer) Clean(subdir string) error {
	if subdir == "" || strings.Contains(subdir, "..") {
		return fmt.Errorf("refusing to clean %q", subdir)
	}
	return os.RemoveAll(filepath.Join(im.StagingRoot, subdir))
}

// Job is one import.
type Job struct {
	Subdir   string   // under StagingRoot, the request's delivery folder
	Platform string   // fs_slug
	Title    string   // game title, used to name a multi-file folder
	Formats  []string // wanted formats, best first (rules), to pick game files
	Convert  string   // "chd" or ""
	// Pick chooses one file among several cartridge files (a download with
	// one ROM per region); it returns an index into names.
	Pick func(names []string) int
	// AsFolder places even a single file in a folder named after the game
	// (a game with extras needs one: RomM attaches category subfolders).
	AsFolder bool
	// Into, for an extra: the folder under LibraryRoot that receives every
	// non-junk file as is ("psx/Crash Bandicoot/update"). Formats are ignored.
	Into string
	Step func(step string)
}

// Result is what was placed, relative to LibraryRoot (first segment = platform).
type Result struct {
	Placed string // "gba/Golden Sun (Europe).gba" or "psx/Crash Bandicoot"
	FSName string // the file or folder name RomM will report
}

var junk = map[string]bool{
	".nfo": true, ".txt": true, ".sfv": true, ".url": true, ".diz": true, ".md5": true, ".sha1": true,
	".jpg": true, ".jpeg": true, ".png": true, ".html": true, ".htm": true, ".db": true, ".ini": true,
	".lnk": true, ".torrent": true, ".exe": true, ".bat": true, ".cmd": true, ".scr": true, ".dat": true,
}

// archives the importer unpacks. zip and 7z stay packed when they hold a
// single cartridge game RomM can read as is.
var archives = map[string]bool{".rar": true, ".zip": true, ".7z": true, ".gz": true, ".tar": true}

// discParts belong together and make a multi-file game.
var discParts = map[string]bool{".cue": true, ".bin": true, ".img": true, ".sub": true, ".ccd": true,
	".gdi": true, ".raw": true, ".m3u": true, ".mds": true, ".mdf": true}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func ext(name string) string { return strings.ToLower(filepath.Ext(name)) }

func (im *Importer) Run(ctx context.Context, job Job) (Result, error) {
	step := job.Step
	if step == nil {
		step = func(string) {}
	}
	dir := filepath.Join(im.StagingRoot, job.Subdir)
	if _, err := os.Stat(dir); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNoDelivery, err)
	}

	step("unpacking")
	if err := im.unpack(ctx, dir, job); err != nil {
		return Result{}, err
	}

	if job.Into != "" {
		step("placing")
		res, err := im.placeExtra(dir, job)
		if err != nil {
			return Result{}, err
		}
		_ = os.RemoveAll(dir)
		if im.ScanHook != "" {
			step("scanning")
			if err := im.scan(ctx, job.Platform, res.Placed); err != nil {
				return res, err
			}
		}
		return res, nil
	}

	step("selecting files")
	files, err := gameFiles(dir, job.Formats)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, errors.New("no game file in the download")
	}

	// Several ROMs and no disc parts: a cartridge pack, keep the best one.
	if len(files) > 1 && !slices.ContainsFunc(files, func(f string) bool { return discParts[ext(f)] }) {
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = filepath.Base(f)
		}
		best := 0
		if job.Pick != nil {
			best = job.Pick(names)
		}
		files = []string{files[best]}
	}

	if job.Convert == "chd" {
		if converted, err := im.toCHD(ctx, dir, files, step); err != nil {
			return Result{}, err
		} else if converted != nil {
			files = converted
		}
	}

	step("placing")
	res, err := im.place(dir, files, job)
	if err != nil {
		return Result{}, err
	}
	_ = os.RemoveAll(dir)

	if im.ScanHook != "" {
		step("scanning")
		if err := im.scan(ctx, job.Platform, res.Placed); err != nil {
			return res, fmt.Errorf("placed %s but the scan hook failed: %w", res.Placed, err)
		}
	}
	return res, nil
}

// unpack extracts archives that hold more than one file, or a format RomM
// cannot read inside them, until nothing is left to unpack (nested archives).
func (im *Importer) unpack(ctx context.Context, dir string, job Job) error {
	for round := 0; round < 3; round++ {
		var packed []string
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && archives[ext(p)] {
				packed = append(packed, p)
			}
			return nil
		})
		if len(packed) == 0 {
			return nil
		}
		changed := false
		for _, archive := range packed {
			keep, err := keepPacked(ctx, archive, job.Formats)
			if err != nil {
				return err
			}
			if keep {
				continue
			}
			out := strings.TrimSuffix(archive, filepath.Ext(archive)) + ".unpacked"
			cmd := exec.CommandContext(ctx, "7z", "x", "-y", "-o"+out, archive)
			if b, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("unpacking %s: %v %s", filepath.Base(archive), err, lastLine(b))
			}
			os.Remove(archive)
			changed = true
		}
		if !changed {
			return nil
		}
	}
	return nil
}

// keepPacked: a zip or 7z holding exactly one file of a wanted cartridge
// format stays as is (RomM and the emulators read it).
func keepPacked(ctx context.Context, archive string, formats []string) (bool, error) {
	e := ext(archive)
	if (e != ".zip" && e != ".7z") || !slices.Contains(formats, strings.TrimPrefix(e, ".")) {
		return false, nil
	}
	out, err := exec.CommandContext(ctx, "7z", "l", "-slt", archive).Output()
	if err != nil {
		return false, fmt.Errorf("listing %s: %v", filepath.Base(archive), err)
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Path = ") {
			names = append(names, strings.TrimPrefix(line, "Path = "))
		}
	}
	// The first "Path" is the archive itself.
	if len(names) != 2 {
		return false, nil
	}
	inner := strings.TrimPrefix(ext(names[1]), ".")
	return slices.Contains(formats, inner) && !archives["."+inner], nil
}

// gameFiles lists the files that make the game, junk removed.
func gameFiles(dir string, formats []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		e := ext(p)
		switch {
		case junk[e]:
			os.Remove(p)
		case slices.Contains(formats, strings.TrimPrefix(e, ".")) || discParts[e]:
			files = append(files, p)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

// toCHD converts cue/gdi/iso images into CHD with chdman, when available.
// Returns nil when nothing was converted (no tool, nothing to convert).
func (im *Importer) toCHD(ctx context.Context, dir string, files []string, step func(string)) ([]string, error) {
	if _, err := exec.LookPath("chdman"); err != nil {
		return nil, nil
	}
	var sources []string
	for _, f := range files {
		if e := ext(f); e == ".cue" || e == ".gdi" || e == ".iso" {
			sources = append(sources, f)
		}
	}
	if len(sources) == 0 {
		return nil, nil
	}
	var out []string
	for i, src := range sources {
		step(fmt.Sprintf("converting to CHD (%d/%d)", i+1, len(sources)))
		dst := strings.TrimSuffix(src, filepath.Ext(src)) + ".chd"
		mode := "createcd"
		if ext(src) == ".iso" {
			mode = "createdvd"
		}
		// The host is a 4-core NUC shared with everything else: two threads,
		// lowest CPU and I/O priority. chdman's default (every core) starved
		// RomM itself during the first PS2 import (load 250, API timeouts).
		cmd := exec.CommandContext(ctx, "ionice", "-c3", "nice", "-n", "19",
			"chdman", mode, "-np", "2", "-i", src, "-o", dst)
		if b, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("chdman %s: %v %s", filepath.Base(src), err, lastLine(b))
		}
		out = append(out, dst)
	}
	// The images went into the CHDs; their parts are no longer needed.
	for _, f := range files {
		if !slices.Contains(out, f) {
			os.Remove(f)
		}
	}
	if len(out) > 1 {
		m3u := filepath.Join(dir, "disc.m3u")
		var b strings.Builder
		for _, f := range out {
			b.WriteString(filepath.Base(f) + "\n")
		}
		if err := os.WriteFile(m3u, []byte(b.String()), 0o644); err != nil {
			return nil, err
		}
		out = append(out, m3u)
	}
	return out, nil
}

// place moves the game into LibraryRoot/<platform>/: one file as is, several
// files into a folder named after the game. Never overwrites.
func (im *Importer) place(dir string, files []string, job Job) (Result, error) {
	platformDir := filepath.Join(im.LibraryRoot, job.Platform)
	if err := os.MkdirAll(platformDir, 0o755); err != nil {
		return Result{}, err
	}
	if len(files) == 1 && !job.AsFolder {
		name := unsafeName.ReplaceAllString(filepath.Base(files[0]), "_")
		target := freeName(filepath.Join(platformDir, name))
		if err := move(files[0], target); err != nil {
			return Result{}, err
		}
		base := filepath.Base(target)
		return Result{Placed: job.Platform + "/" + base, FSName: base}, nil
	}
	folder := strings.TrimSpace(unsafeName.ReplaceAllString(job.Title, " "))
	if folder == "" {
		folder = filepath.Base(dir)
	}
	target := freeName(filepath.Join(platformDir, folder))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Result{}, err
	}
	for _, f := range files {
		name := unsafeName.ReplaceAllString(filepath.Base(f), "_")
		if err := move(f, freeName(filepath.Join(target, name))); err != nil {
			return Result{}, err
		}
	}
	base := filepath.Base(target)
	return Result{Placed: job.Platform + "/" + base, FSName: base}, nil
}

// EnsureFolder makes sure a placed game is a folder, so extras can go into
// its category subfolders: a single file "ps2/Ico.iso" becomes
// "ps2/Ico/Ico.iso". RomM 5.3 recognises a ROM by content, so the move keeps
// its saves and history. Returns the folder's path relative to LibraryRoot.
func (im *Importer) EnsureFolder(placed string) (string, error) {
	full := filepath.Join(im.LibraryRoot, filepath.FromSlash(placed))
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return placed, nil
	}
	stem := strings.TrimSuffix(filepath.Base(full), filepath.Ext(full))
	folder := freeName(filepath.Join(filepath.Dir(full), stem))
	if err := os.Mkdir(folder, 0o755); err != nil {
		return "", err
	}
	if err := move(full, filepath.Join(folder, filepath.Base(full))); err != nil {
		os.Remove(folder)
		return "", err
	}
	return filepath.ToSlash(filepath.Join(filepath.Dir(placed), filepath.Base(folder))), nil
}

// placeExtra moves every remaining file of an extra into job.Into.
func (im *Importer) placeExtra(dir string, job Job) (Result, error) {
	target := filepath.Join(im.LibraryRoot, filepath.FromSlash(job.Into))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Result{}, err
	}
	n := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if junk[ext(p)] {
			return os.Remove(p)
		}
		n++
		return move(p, freeName(filepath.Join(target, unsafeName.ReplaceAllString(d.Name(), "_"))))
	})
	if err != nil {
		return Result{}, err
	}
	if n == 0 {
		return Result{}, errors.New("nothing to place in the download")
	}
	return Result{Placed: job.Into, FSName: filepath.Base(job.Into)}, nil
}

// freeName adds " (2)", " (3)"... before the extension until nothing exists there.
func freeName(p string) string {
	if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return p
	}
	e := filepath.Ext(p)
	stem := strings.TrimSuffix(p, e)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, e)
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate
		}
	}
}

// move renames (same NFS share: instant), copying only across filesystems.
func move(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

func (im *Importer) scan(ctx context.Context, platform, placed string) error {
	body, _ := json.Marshal(map[string]any{"storage": "roms", "subdir": platform, "paths": []string{placed}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, im.ScanHook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := im.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}
