package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	repoLatestAPI = "https://api.github.com/repos/yerlinson10/Brinco-CLI/releases/latest"
)

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func Check(currentVersion string) (string, bool, error) {
	rel, err := fetchRelease()
	if err != nil {
		return "", false, err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	current := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")
	return latest, latest != "" && latest != current, nil
}

func Apply(currentVersion string) error {
	rel, err := fetchRelease()
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	if latest == strings.TrimPrefix(currentVersion, "v") {
		fmt.Println("Ya estas en la ultima version.")
		return nil
	}
	archiveName := fmt.Sprintf("brinco_%s_%s_%s", latest, runtime.GOOS, runtime.GOARCH)
	wantedExt := ".tar.gz"
	if runtime.GOOS == "windows" {
		wantedExt = ".zip"
	}
	assetURL := ""
	checksumsURL := ""
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
	tmpDir, err := os.MkdirTemp("", "brinco-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	archivePath := filepath.Join(tmpDir, "release"+wantedExt)
	if err := download(assetURL, archivePath); err != nil {
		return err
	}
	if checksumsURL != "" {
		sumPath := filepath.Join(tmpDir, "checksums.txt")
		if err := download(checksumsURL, sumPath); err == nil {
			if err := verifyChecksum(archivePath, sumPath); err != nil {
				return err
			}
		}
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
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		dst := exe + ".new"
		if err := copyFile(newBin, dst); err != nil {
			return err
		}
		fmt.Printf("Actualizacion descargada en %s. Cierra Brinco y reemplaza el binario.\n", dst)
		return nil
	}
	backup := exe + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return err
	}
	if err := copyFile(newBin, exe); err != nil {
		_ = os.Rename(backup, exe)
		return err
	}
	_ = os.Chmod(exe, 0o755)
	_ = os.Remove(backup)
	fmt.Printf("Actualizado a %s correctamente.\n", latest)
	return nil
}

func fetchRelease() (*ghRelease, error) {
	resp, err := http.Get(repoLatestAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github api status: %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func download(url, dst string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("descarga fallo: %s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func verifyChecksum(archivePath, checksumsPath string) error {
	raw, err := os.ReadFile(checksumsPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(mustRead(archivePath))
	got := hex.EncodeToString(sum[:])
	fileName := filepath.Base(archivePath)
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, fileName) {
			fields := strings.Fields(line)
			if len(fields) > 0 && strings.EqualFold(fields[0], got) {
				return nil
			}
			return fmt.Errorf("checksum invalido para %s", fileName)
		}
	}
	return nil
}

func extractZipBinary(archivePath, dst string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base == "brinco" || base == "brinco.exe" {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			out, err := os.Create(dst)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, rc)
			return err
		}
	}
	return fmt.Errorf("binario no encontrado en zip")
}

func extractTarGzBinary(archivePath, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		base := filepath.Base(hdr.Name)
		if base == "brinco" || base == "brinco.exe" {
			out, err := os.Create(dst)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, tr)
			return err
		}
	}
	return fmt.Errorf("binario no encontrado en tar.gz")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func mustRead(path string) []byte {
	b, _ := os.ReadFile(path)
	return b
}
