// Package zipx provides safe ZIP validation and extraction with limits
// against zip-slip, oversized archives, and special files.
package zipx

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// isPathWithinBase checks if absPath (absolute, resolved) is under baseAbs (absolute, resolved)
func isPathWithinBase(baseAbs, absPath string) bool {
	rel, err := filepath.Rel(baseAbs, absPath)
	if err != nil {
		return false
	}
	relClean := filepath.Clean(rel)
	return relClean != ".." && !strings.HasPrefix(relClean, ".."+string(filepath.Separator))
}

// Policy defines extraction limits and behavior.
type Policy struct {
	MaxFiles      int   // maximum number of files allowed
	MaxTotalBytes int64 // maximum total uncompressed bytes
	MaxFileBytes  int64 // maximum size per file
	AllowSymlinks bool  // whether symlinks are allowed
	PreserveTimes bool  // whether to preserve file mtimes
}

// DefaultPolicy returns conservative defaults: 20k files,
// 2 GiB total, 512 MiB per file, no symlinks, no times.
func DefaultPolicy() Policy {
	return Policy{
		MaxFiles:      20000,
		MaxTotalBytes: 2 << 30,   // 2 GiB
		MaxFileBytes:  512 << 20, // 512 MiB
	}
}

// Validate checks that zipPath is a readable ZIP file.
// Returns io.ErrUnexpectedEOF if it is not.
func Validate(zipPath string) (err error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		if errors.Is(err, zip.ErrFormat) || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("zip validate: %w", io.ErrUnexpectedEOF)
		}
		return fmt.Errorf("zip validate open: %w", err)
	}
	defer func() {
		if cerr := zr.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("zip validate close: %w", cerr)
		}
	}()

	return nil
}

// Unzip extracts srcZip into destDir according to policy p.
// It enforces limits, prevents zip-slip, and skips unsafe entries.
func Unzip(srcZip, destDir string, p Policy) (err error) {
	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := r.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close zip: %w", cerr))
		}
	}()

	// Create root dir with conservative perms
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}

	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}

	destReal := destAbs
	if dr, err := filepath.EvalSymlinks(destAbs); err == nil && dr != "" {
		destReal = dr
	}

	if p.MaxFiles > 0 && len(r.File) > p.MaxFiles {
		return fmt.Errorf("zip too many files: %d", len(r.File))
	}

	var totalWritten int64

	for _, f := range r.File {
		// --- Normalize and validate path ---
		name := strings.ReplaceAll(f.Name, `\`, `/`)

		// reject null bytes (defensive)
		if strings.IndexByte(name, 0) != -1 {
			return fmt.Errorf("invalid file name (NUL) in zip: %q", f.Name)
		}
		rel := path.Clean(name)

		// strip leading "/" and "./"
		for strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "./") {
			rel = strings.TrimPrefix(strings.TrimPrefix(rel, "/"), "./")
		}
		if rel == "" || rel == "." {
			continue
		}
		for seg := range strings.SplitSeq(rel, "/") {
			if seg == ".." {
				return fmt.Errorf("unsafe path traversal in zip (.. segment): %q", f.Name)
			}
		}

		cand := filepath.FromSlash(rel)
		// absolute or has volume name (Windows/UNC)
		if filepath.IsAbs(cand) || filepath.VolumeName(cand) != "" {
			return fmt.Errorf("unsafe absolute path in zip: %q", f.Name)
		}
		nativePath := filepath.Join(destDir, cand)

		// header hints — soft checks (still enforce per-file cap via copy)
		if p.MaxFileBytes > 0 && int64(f.UncompressedSize64) > p.MaxFileBytes {
			return fmt.Errorf("zip entry too big by header: %s (%d bytes)", f.Name, f.UncompressedSize64)
		}

		targetAbs, err := filepath.Abs(nativePath)
		if err != nil {
			return err
		}
		// must be strictly within destReal
		if !isPathWithinBase(destReal, targetAbs) {
			return fmt.Errorf("unsafe path escape: %q", f.Name)
		}

		info := f.FileInfo()
		mode := info.Mode()

		// Make sure parent exists
		if info.IsDir() {
			if err := os.MkdirAll(targetAbs, 0o755); err != nil {
				return err
			}
			// Optional: preserve times for dirs
			if p.PreserveTimes && !f.Modified.IsZero() {
				_ = os.Chtimes(targetAbs, f.Modified, f.Modified)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
			return err
		}

		// Parents must not contain symlinks that leave dest, ALWAYS check
		if bad, derr := pathHasSymlinkOutside(destReal, targetAbs); derr == nil && bad {
			return fmt.Errorf("unsafe symlink in parents for: %q", f.Name)
		} else if derr != nil && !os.IsNotExist(derr) { // not-exist is fine mid-extract
			return derr
		}

		// Skip device/pipe/socket entries outright
		if mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 {
			continue
		}

		// Handle symlinks explicitly if allowed; otherwise skip them
		if mode&os.ModeSymlink != 0 {
			if !p.AllowSymlinks {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			// Protect against huge "targets" embedded as content
			const maxLinkTarget = 1 << 20 // 1 MiB safety cap
			linkTargetBytes, rerr := io.ReadAll(io.LimitReader(rc, maxLinkTarget))
			_ = rc.Close()
			if rerr != nil {
				return fmt.Errorf("read symlink target: %w", rerr)
			}
			linkTarget := strings.TrimSpace(string(linkTargetBytes))
			if linkTarget == "" {
				return fmt.Errorf("empty symlink target: %q", f.Name)
			}
			// No absolute/volume targets
			if filepath.IsAbs(linkTarget) || filepath.VolumeName(linkTarget) != "" {
				return fmt.Errorf("absolute symlink target not allowed: %q -> %q", f.Name, linkTarget)
			}
			// Normalize a bit (keep relative)
			// If symlink target escapes on resolution at runtime, parent check above still blocks via EvalSymlinks
			_ = os.Remove(targetAbs) // best-effort replace

			// -- Fix: Check resolved destination and symlink target before creating symlink --
			// 1. Resolve parent directory's symlinks (already extracted so far).
			parentResolved, err := filepath.EvalSymlinks(filepath.Dir(targetAbs))
			if err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("symlink parent resolve error: %w", err)
				}
				// If parent doesn't exist, mkdirall above does it, so we fallback to intended parent
				parentResolved = filepath.Dir(targetAbs)
			}
			linkAbs := filepath.Join(parentResolved, filepath.Base(targetAbs))
			if !isPathWithinBase(destReal, linkAbs) {
				return fmt.Errorf("symlink destination escapes extraction root: %q", linkAbs)
			}
			// 2. Where would the symlink, if created, point to? (Relative to resolved parent.)
			targetCandidate := filepath.Join(parentResolved, linkTarget)
			// We can't EvalSymlinks on the new symlink yet, but check that the _synthetic resolution_ is within destReal.
			if !isPathWithinBase(destReal, targetCandidate) {
				return fmt.Errorf("symlink target escapes extraction root: %q -> %q", f.Name, linkTarget)
			}

			if err := os.Symlink(linkTarget, targetAbs); err != nil {
				return fmt.Errorf("create symlink: %w", err)
			}
			continue
		}

		// Handle regular file (and "unknown regular")
		rc, err := f.Open()
		if err != nil {
			return err
		}

		perm := mode.Perm()
		if perm == 0 {
			perm = 0o644
		}

		// Create a unique temp file next to the final destination.
		// This avoids ".partial" leftovers breaking future runs.
		tmpf, err := os.CreateTemp(filepath.Dir(targetAbs), filepath.Base(targetAbs)+".partial-*")
		if err != nil {
			_ = rc.Close()
			return err
		}
		tmp := tmpf.Name()

		// Best-effort set permissions on the temp file (some OSes may ignore until rename).
		_ = tmpf.Chmod(perm)

		n, werr := copyCapped(tmpf, rc, p.MaxFileBytes)

		// close writers/readers with proper precedence
		if cerr := tmpf.Close(); werr == nil && cerr != nil {
			werr = cerr
		}
		if cerr := rc.Close(); werr == nil && cerr != nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(tmp)
			return werr
		}

		// Update actual total written and enforce cap
		totalWritten += n
		if p.MaxTotalBytes > 0 && totalWritten > p.MaxTotalBytes {
			_ = os.Remove(tmp)
			return fmt.Errorf("zip too large uncompressed (actual): %d > %d", totalWritten, p.MaxTotalBytes)
		}

		// On Windows, rename over existing file may fail. Remove first.
		_ = os.Remove(targetAbs)
		if err := os.Rename(tmp, targetAbs); err != nil {
			_ = os.Remove(tmp)
			return err
		}

		if p.PreserveTimes && !f.Modified.IsZero() {
			_ = os.Chtimes(targetAbs, f.Modified, f.Modified)
		}
	}
	return nil
}

func pathHasSymlinkOutside(destRoot, file string) (bool, error) {
	rel, err := filepath.Rel(destRoot, file)
	if err != nil {
		return true, err
	}
	cur := destRoot
	for seg := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if seg == "" || seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return true, err
			}
			if real != destRoot && !strings.HasPrefix(real, destRoot+string(filepath.Separator)) {
				return true, nil
			}
		}
	}
	return false, nil
}

// copyCapped copies from src to dst up to max bytes,
// returning an error if max is exceeded.
func copyCapped(dst io.Writer, src io.Reader, max int64) (int64, error) {
	if max > 0 {
		lr := &io.LimitedReader{R: src, N: max + 1}
		n, err := io.Copy(dst, lr)
		if err != nil {
			return n, err
		}
		if lr.N == 0 {
			return n, fmt.Errorf("zip entry exceeds max size")
		}
		return n, nil
	}
	return io.Copy(dst, src)
}
