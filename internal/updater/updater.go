package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	repoOwnerRepo = "yerlinson10/Brinco-CLI"
	repoLatestAPI = "https://api.github.com/repos/" + repoOwnerRepo + "/releases/latest"
	repoReleasesAPI = "https://api.github.com/repos/" + repoOwnerRepo + "/releases?per_page=100"

	userAgent = "Brinco-CLI-updater/1.0"

	apiRequestTimeout    = 45 * time.Second
	downloadAssetTimeout = 20 * time.Minute

	maxReleaseAssetBytes = 256 << 20 // 256 MiB
	maxChecksumFileBytes = 1 << 20   // 1 MiB

	maxHTTPRetries = 4
	initialBackoff = 300 * time.Millisecond
)

var defaultHTTPClient = &http.Client{
	Timeout: 0, // deadlines come from context per request
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

type ghRelease struct {
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	TagName    string `json:"tag_name"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check consulta GitHub y compara la última versión con currentVersion (semver cuando aplica).
func Check(currentVersion string) (latest string, available bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout+30*time.Second)
	defer cancel()

	rel, err := fetchRelease(ctx)
	if err != nil {
		return "", false, err
	}
	latest = strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	current := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")
	if latest == "" {
		return "", false, fmt.Errorf("release sin tag_name válido")
	}
	avail, err := shouldOfferUpdate(current, latest)
	if err != nil {
		return "", false, err
	}
	return latest, avail, nil
}

// Apply descarga el artefacto para esta plataforma, verifica integridad e instala el binario.
func Apply(currentVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), downloadAssetTimeout+apiRequestTimeout+5*time.Minute)
	defer cancel()

	rel, err := fetchRelease(ctx)
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	current := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")
	up, err := shouldOfferUpdate(current, latest)
	if err != nil {
		return err
	}
	if !up {
		if localVersionAhead(current, latest) {
			fmt.Println("Tu version es mas reciente que el ultimo release en GitHub.")
			return nil
		}
		fmt.Println("Ya estas en la ultima version.")
		return nil
	}

	archiveName := fmt.Sprintf("brinco_%s_%s_%s", latest, runtime.GOOS, runtime.GOARCH)
	wantedExt := ".tar.gz"
	if runtime.GOOS == "windows" {
		wantedExt = ".zip"
	}
	var assetURL, checksumsURL string
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, archiveName) && strings.HasSuffix(a.Name, wantedExt) {
			assetURL = a.URL
		}
		if a.Name == "checksums.txt" {
			checksumsURL = a.URL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no se encontro artefacto para %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if envRequireChecksum() && checksumsURL == "" {
		return fmt.Errorf("BRINCO_UPDATE_REQUIRE_CHECKSUM activo pero el release no incluye checksums.txt")
	}

	tmpDir, err := os.MkdirTemp("", "brinco-update-*")
	if err != nil {
		return fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "release"+wantedExt)
	if err := download(ctx, assetURL, archivePath, maxReleaseAssetBytes, progressWriter()); err != nil {
		return fmt.Errorf("descarga del release: %w", err)
	}

	if checksumsURL != "" {
		sumPath := filepath.Join(tmpDir, "checksums.txt")
		if err := download(ctx, checksumsURL, sumPath, maxChecksumFileBytes, nil); err != nil {
			return fmt.Errorf("descarga checksums.txt: %w", err)
		}
		if err := verifyChecksum(archivePath, sumPath); err != nil {
			return err
		}
	} else if envRequireChecksum() {
		return fmt.Errorf("checksums.txt requerido pero no hay URL de asset")
	}

	newBin := filepath.Join(tmpDir, "brinco")
	if runtime.GOOS == "windows" {
		newBin += ".exe"
	}
	if wantedExt == ".zip" {
		if err := extractZipBinary(archivePath, newBin); err != nil {
			return err
		}
	} else {
		if err := extractTarGzBinary(archivePath, newBin); err != nil {
			return err
		}
	}
	if err := os.Chmod(newBin, 0o755); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("chmod binario extraido: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("eval symlinks: %w", err)
	}

	if runtime.GOOS == "windows" {
		dst := exe + ".new"
		if err := copyFileWithMode(newBin, dst, 0o755); err != nil {
			return fmt.Errorf("copia a %s: %w", dst, err)
		}
		fmt.Printf("Actualizacion descargada en %s. Cierra Brinco y reemplaza el binario.\n", dst)
		return nil
	}
	if err := atomicReplaceBinary(newBin, exe); err != nil {
		return err
	}
	fmt.Printf("Actualizado a %s correctamente.\n", latest)
	return nil
}

func prereleaseFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("BRINCO_UPDATE_PRERELEASE"))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envRequireChecksum() bool {
	v := strings.TrimSpace(os.Getenv("BRINCO_UPDATE_REQUIRE_CHECKSUM"))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envShowProgress() bool {
	v := strings.TrimSpace(os.Getenv("BRINCO_UPDATE_PROGRESS"))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func fetchRelease(ctx context.Context) (*ghRelease, error) {
	if u := strings.TrimSpace(os.Getenv("BRINCO_UPDATE_RELEASE_API")); u != "" {
		return fetchSingleRelease(ctx, u)
	}
	if prereleaseFromEnv() {
		return fetchBestReleaseFromList(ctx, repoReleasesAPI)
	}
	return fetchSingleRelease(ctx, repoLatestAPI)
}

func fetchSingleRelease(ctx context.Context, url string) (*ghRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()

	resp, err := doGETWithRetries(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("github api %s: %w", url, err)
	}
	defer resp.Body.Close()

	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release json: %w", err)
	}
	return &rel, nil
}

func fetchBestReleaseFromList(ctx context.Context, url string) (*ghRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()

	resp, err := doGETWithRetries(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("github releases list: %w", err)
	}
	defer resp.Body.Close()

	var list []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode releases list: %w", err)
	}
	var best *ghRelease
	var bestCanon string
	for i := range list {
		r := &list[i]
		if r.Draft {
			continue
		}
		tag := strings.TrimSpace(r.TagName)
		if tag == "" {
			continue
		}
		v := "v" + strings.TrimPrefix(tag, "v")
		if !semver.IsValid(v) {
			continue
		}
		c := semver.Canonical(v)
		if best == nil || semver.Compare(c, bestCanon) > 0 {
			best = r
			bestCanon = c
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no hay releases con tag semver válido")
	}
	return best, nil
}

func shouldOfferUpdate(current, latest string) (bool, error) {
	lv := "v" + strings.TrimPrefix(strings.TrimSpace(latest), "v")
	cv := "v" + strings.TrimPrefix(strings.TrimSpace(current), "v")
	if semver.IsValid(lv) && semver.IsValid(cv) {
		return semver.Compare(semver.Canonical(lv), semver.Canonical(cv)) > 0, nil
	}
	// Versiones no semver (p. ej. builds locales): igualdad por texto.
	return strings.TrimSpace(latest) != strings.TrimSpace(current), nil
}

func localVersionAhead(current, latest string) bool {
	lv := "v" + strings.TrimPrefix(strings.TrimSpace(latest), "v")
	cv := "v" + strings.TrimPrefix(strings.TrimSpace(current), "v")
	if semver.IsValid(lv) && semver.IsValid(cv) {
		return semver.Compare(semver.Canonical(cv), semver.Canonical(lv)) > 0
	}
	return false
}

func doGETWithRetries(ctx context.Context, url string) (*http.Response, error) {
	backoff := initialBackoff
	var lastErr error
	for attempt := 0; attempt < maxHTTPRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := defaultHTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if sec, e := strconv.Atoi(ra); e == nil && sec > 0 {
					_ = resp.Body.Close()
					t := time.NewTimer(time.Duration(sec) * time.Second)
					select {
					case <-ctx.Done():
						t.Stop()
						return nil, ctx.Err()
					case <-t.C:
					}
					continue
				}
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("tras %d intentos: %w", maxHTTPRetries, lastErr)
	}
	return nil, fmt.Errorf("tras %d intentos", maxHTTPRetries)
}

type progressFn func(written, total int64)

func progressWriter() progressFn {
	if !envShowProgress() {
		return nil
	}
	return func(written, total int64) {
		if total > 0 {
			pct := written * 100 / total
			fmt.Printf("\rDescargando... %d%% (%d / %d bytes)", pct, written, total)
			if written >= total {
				fmt.Println()
			}
			return
		}
		if written > 0 && written%(2<<20) == 0 {
			fmt.Printf("\rDescargando... %d MB", written>>20)
		}
	}
}

func download(ctx context.Context, url, dst string, maxBytes int64, onProgress progressFn) error {
	ctx, cancel := context.WithTimeout(ctx, downloadAssetTimeout)
	defer cancel()

	resp, err := doGETWithRetries(ctx, url)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if cl := resp.ContentLength; cl > 0 && cl > maxBytes {
		return fmt.Errorf("tamaño declarado %d supera el máximo %d", cl, maxBytes)
	}

	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("crear %s: %w", dst, err)
	}
	defer f.Close()

	var body io.Reader = resp.Body
	if onProgress != nil {
		body = &progressReader{r: resp.Body, total: resp.ContentLength, on: onProgress}
	}

	n, err := io.Copy(f, io.LimitReader(body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("escribir descarga: %w", err)
	}
	if n > maxBytes {
		return fmt.Errorf("descarga supera el máximo permitido (%d bytes)", maxBytes)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync archivo: %w", err)
	}
	if onProgress != nil && resp.ContentLength <= 0 && n > 0 {
		fmt.Println()
	}
	return nil
}

type progressReader struct {
	r       io.Reader
	total   int64
	n       int64
	on      progressFn
	lastPct int64
	lastMB  int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	nr, err := p.r.Read(b)
	if nr > 0 && p.on != nil {
		p.n += int64(nr)
		if p.total > 0 {
			pct := p.n * 100 / p.total
			if pct != p.lastPct || p.n >= p.total {
				p.lastPct = pct
				p.on(p.n, p.total)
			}
		} else {
			mb := p.n >> 20
			if mb != p.lastMB && p.n > 0 {
				p.lastMB = mb
				p.on(p.n, p.total)
			}
		}
	}
	return nr, err
}

func verifyChecksum(archivePath, checksumsPath string) error {
	raw, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("leer checksums: %w", err)
	}
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("leer archivo para hash: %w", err)
	}
	sum := sha256.Sum256(archiveBytes)
	got := hex.EncodeToString(sum[:])
	fileName := filepath.Base(archivePath)

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		wantHash := strings.ToLower(fields[0])
		nameField := fields[len(fields)-1]
		nameField = strings.TrimPrefix(nameField, "*")
		if filepath.Base(nameField) != fileName {
			continue
		}
		if wantHash == got {
			return nil
		}
		return fmt.Errorf("checksum inválido para %s (esperado en manifest distinto del calculado)", fileName)
	}
	return fmt.Errorf("checksums.txt no contiene entrada para %s", fileName)
}

func extractZipBinary(archivePath, dst string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("abrir zip: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base != "brinco" && base != "brinco.exe" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("abrir entrada zip: %w", err)
		}
		err = writeExtracted(rc, dst)
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return fmt.Errorf("cerrar zip entry: %w", closeErr)
		}
		return nil
	}
	return fmt.Errorf("binario no encontrado en zip")
}

func extractTarGzBinary(archivePath, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("abrir tar.gz: %w", err)
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if base != "brinco" && base != "brinco.exe" {
			continue
		}
		if err := writeExtracted(tr, dst); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("binario no encontrado en tar.gz")
}

func writeExtracted(r io.Reader, dst string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("crear binario destino: %w", err)
	}
	_, copyErr := io.Copy(out, r)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("extraer binario: %w", copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("sync binario: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("cerrar binario: %w", closeErr)
	}
	return nil
}

func copyFileWithMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("abrir origen: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("crear destino: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copiar: %w", copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("sync destino: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("cerrar destino: %w", closeErr)
	}
	return nil
}

func atomicReplaceBinary(newBin, exe string) error {
	exeDir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(exeDir, ".brinco-new-*")
	if err != nil {
		return fmt.Errorf("temp en directorio del ejecutable: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("cerrar temp: %w", err)
	}
	if err := copyFileWithMode(newBin, tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copia a temporal: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temporal: %w", err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("reemplazo atómico del binario: %w", err)
	}
	return nil
}
